package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type fakeArchive struct {
	calls   int
	query   string
	options anna.SearchOptions
	article anna.ArticleDownloadOptions
	books   []*anna.Book
	failure error
}

func (f *fakeArchive) FindBook(_ context.Context, q string, o anna.SearchOptions, _ time.Duration) ([]*anna.Book, error) {
	f.calls++
	f.query = q
	f.options = o
	return f.books, f.failure
}
func (f *fakeArchive) FindArticle(_ context.Context, q string, o anna.SearchOptions, _ time.Duration) ([]*anna.Paper, error) {
	f.calls++
	f.query = q
	f.options = o
	return []*anna.Paper{}, f.failure
}
func (f *fakeArchive) LookupDOI(_ context.Context, q string, _ time.Duration) (*anna.Paper, error) {
	f.calls++
	f.query = q
	return &anna.Paper{DOI: "10.1000/example", Title: "Example"}, f.failure
}
func (f *fakeArchive) DownloadBook(_ context.Context, _ *anna.Book, _ time.Duration, _ anna.ProgressFunc) (anna.DownloadResult, error) {
	f.calls++
	return anna.DownloadResult{Path: "/tmp/example.pdf", Bytes: 123}, f.failure
}
func (f *fakeArchive) DownloadArticle(_ context.Context, o anna.ArticleDownloadOptions, _ time.Duration, _ anna.ProgressFunc) (anna.DownloadResult, error) {
	f.calls++
	f.article = o
	return anna.DownloadResult{Path: "/tmp/example.pdf", Bytes: 123}, f.failure
}

func connectTestServer(t *testing.T, archive *fakeArchive) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(&service{archive: archive}, anna.DefaultSearchTimeout, anna.DefaultDownloadTimeout)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "annas-mcp-test", Version: "0.0.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(); serverSession.Close() })
	return session
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	value, ok := result.Content[0].(*mcp.TextContent)
	if !ok || value == nil {
		t.Fatalf("content type %T", result.Content[0])
	}
	return value.Text
}

func TestMCPInitializationAndSchemasNeedNoUpstream(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	session := connectTestServer(t, archive)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 4 {
		t.Fatalf("got %d tools", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		readOnly := strings.HasSuffix(tool.Name, "search")
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != readOnly {
			t.Fatalf("wrong annotations: %+v", tool)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("tool marked destructive: %s", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("missing input schema: %s", tool.Name)
		}
		if tool.Name != "article_search" && tool.OutputSchema == nil {
			t.Fatalf("missing output schema: %s", tool.Name)
		}
	}
	if archive.calls != 0 {
		t.Fatal("initialization contacted upstream")
	}
}

func TestMCPRejectsInvalidArgumentsBeforeCallingUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"book_search", map[string]any{"query": "example", "page": -1}},
		{"book_search", map[string]any{"query": "example", "limit": -1}},
		{"book_search", map[string]any{"query": "example", "language": "English"}},
		{"article_search", map[string]any{"query": "https://doi.org/10.1000/example", "page": -1}},
		{"book_search", map[string]any{"query": " "}},
		{"book_search", map[string]any{"query": "example", "content": "pdf"}},
		{"book_search", map[string]any{"query": "example", "timeout_seconds": 9223372037}},
		{"article_download", map[string]any{}},
		{"article_download", map[string]any{"doi": "10.1000/example", "hash": "abc"}},
	} {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) {
			archive := &fakeArchive{}
			session := connectTestServer(t, archive)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.name, Arguments: tc.args})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.HasPrefix(toolText(t, result), "[INVALID_ARGUMENT]") {
				t.Fatalf("unexpected result: %+v", result)
			}
			if archive.calls != 0 {
				t.Fatal("invalid input reached upstream")
			}
		})
	}
}

func TestDOIURLsRouteToLookupThroughBothAdapters(t *testing.T) {
	t.Parallel()
	for _, query := range []string{"10.1000/example", "https://doi.org/10.1000/example", "doi:10.1000/example"} {
		archive := &fakeArchive{}
		session := connectTestServer(t, archive)
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "article_search", Arguments: map[string]any{"query": query}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError || !strings.Contains(toolText(t, result), `"doi":"10.1000/example"`) {
			t.Fatalf("unexpected result: %+v", result)
		}
		if archive.query != query {
			t.Fatalf("lookup query=%q", archive.query)
		}
		var output bytes.Buffer
		cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs([]string{"article-search", "--json", query})
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err)
		}
		var paper anna.Paper
		if err := json.Unmarshal(output.Bytes(), &paper); err != nil {
			t.Fatal(err)
		}
		if paper.DOI != "10.1000/example" {
			t.Fatalf("wrong CLI result: %+v", paper)
		}
	}
}

func TestSearchPaginationAndStructuredResults(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	for i := 0; i < 12; i++ {
		archive.books = append(archive.books, &anna.Book{Title: fmt.Sprint(i)})
	}
	session := connectTestServer(t, archive)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "book_search", Arguments: map[string]any{"query": "example", "page": 2, "language": "EN"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.StructuredContent == nil {
		t.Fatalf("missing structured result: %+v", result)
	}
	var page searchResult[*anna.Book]
	if err := json.Unmarshal([]byte(toolText(t, result)), &page); err != nil {
		t.Fatal(err)
	}
	if page.Matched != 12 || page.Limit != 10 || len(page.Results) != 10 || page.Page != 2 || page.Language != "en" {
		t.Fatalf("wrong page: %+v", page)
	}
	var output bytes.Buffer
	cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"book-search", "--json", "example"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 12 {
		t.Fatal("CLI should default to the complete page")
	}
}

func TestTypedErrorsDoNotClassifyDOIDigitsAsHTTPStatus(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{failure: apperr.New(apperr.NotFound, "no paper found for DOI: 10.1403/example")}
	session := connectTestServer(t, archive)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "article_search", Arguments: map[string]any{"query": "10.1403/example"}})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || !strings.HasPrefix(toolText(t, result), "[NOT_FOUND]") {
		t.Fatalf("wrong error: %+v", result)
	}
	if got := codedError(fmt.Errorf("request failed: %w", context.DeadlineExceeded)); !strings.HasPrefix(got, "[REQUEST_TIMEOUT]") {
		t.Fatal(got)
	}
	if got := codedError(errors.New("upstream text contains 403")); !strings.HasPrefix(got, "[UPSTREAM]") {
		t.Fatal(got)
	}
}

func TestCLIValidationAndDownloadOptions(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	for _, args := range [][]string{{"book-search", "--page=-1", "example"}, {"book-search", "--limit=-1", "example"}, {"book-search", "--timeout=25h", "example"}, {"article-download"}} {
		cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); apperr.CodeOf(err) != apperr.InvalidArgument {
			t.Fatalf("args %v: %v", args, err)
		}
	}
	if archive.calls != 0 {
		t.Fatal("invalid CLI input reached upstream")
	}
	var output bytes.Buffer
	cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"article-download", "--hash", "0123456789abcdef0123456789abcdef", "--title", "Example", "--format", "pdf", "--json"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if archive.article.Title != "Example" || archive.article.Format != "pdf" || archive.article.Hash == "" {
		t.Fatalf("wrong options: %+v", archive.article)
	}
	var result anna.DownloadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Bytes != 123 {
		t.Fatalf("invalid download output: %s, %v", output.String(), err)
	}
}
