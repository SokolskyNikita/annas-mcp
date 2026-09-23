package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
	cases := fixtureJSON[[]struct {
		Name      string
		Tool      string
		Arguments map[string]any
	}](t, "invalid-arguments.json")
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			archive := &fakeArchive{}
			session := connectTestServer(t, archive)
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.Tool, Arguments: tc.Arguments})
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
