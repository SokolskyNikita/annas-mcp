package modes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func fixtureJSON[T any](t *testing.T, name string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal(fixture(t, name), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

type fakeArchive struct {
	calls    int
	query    string
	options  anna.SearchOptions
	article  anna.ArticleDownloadOptions
	books    []*anna.Book
	partial  []*anna.Book
	papers   []*anna.Paper
	records  []*anna.MetadataRecord
	total    int
	details  *anna.Details
	status   *anna.AccountStatus
	detailID string
	book     *anna.Book
	timeout  time.Duration
	failure  error
}

func (f *fakeArchive) FindBook(_ context.Context, q string, o anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.Book], error) {
	f.calls++
	f.timeout = timeout
	f.query = q
	f.options = o
	return anna.SearchPage[*anna.Book]{Results: f.books, Partial: f.partial, Total: max(f.total, len(f.books))}, f.failure
}
func (f *fakeArchive) FindArticle(_ context.Context, q string, o anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.Paper], error) {
	f.calls++
	f.timeout = timeout
	f.query = q
	f.options = o
	return anna.SearchPage[*anna.Paper]{Results: f.papers, Total: len(f.papers)}, f.failure
}
func (f *fakeArchive) FindMetadata(_ context.Context, q string, o anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.MetadataRecord], error) {
	f.calls++
	f.timeout = timeout
	f.query = q
	f.options = o
	return anna.SearchPage[*anna.MetadataRecord]{Results: f.records, Total: len(f.records)}, f.failure
}
func (f *fakeArchive) Details(_ context.Context, id string, timeout time.Duration) (*anna.Details, error) {
	f.calls++
	f.timeout = timeout
	f.detailID = id
	if f.details == nil {
		return &anna.Details{ID: "md5:" + id, Title: "Example Details"}, f.failure
	}
	return f.details, f.failure
}
func (f *fakeArchive) AccountStatus(_ context.Context, timeout time.Duration) (*anna.AccountStatus, error) {
	f.calls++
	f.timeout = timeout
	if f.status == nil {
		return &anna.AccountStatus{Member: true, DownloadsLeft: 7, DownloadsLimit: 25, WindowHours: 18}, f.failure
	}
	return f.status, f.failure
}
func (f *fakeArchive) LookupDOI(_ context.Context, q string, timeout time.Duration) (*anna.Paper, error) {
	f.calls++
	f.timeout = timeout
	f.query = q
	return &anna.Paper{DOI: "10.1000/example", Title: "Example"}, f.failure
}
func (f *fakeArchive) DownloadBook(_ context.Context, book *anna.Book, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	f.calls++
	f.timeout = timeout
	f.book = book
	if progress != nil {
		progress(123, 123)
	}
	return anna.DownloadResult{Path: "/tmp/example.pdf", Bytes: 123}, f.failure
}
func (f *fakeArchive) DownloadArticle(_ context.Context, o anna.ArticleDownloadOptions, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	f.calls++
	f.timeout = timeout
	f.article = o
	if progress != nil {
		progress(123, 123)
	}
	return anna.DownloadResult{Path: "/tmp/example.pdf", Bytes: 123}, f.failure
}

func connectTestServer(t *testing.T, archive archiveClient) *mcp.ClientSession {
	t.Helper()
	return connectTestServerOptions(t, archive, nil)
}

func connectTestServerOptions(t *testing.T, archive archiveClient, options *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(&service{archive: archive}, anna.DefaultSearchTimeout, anna.DefaultDownloadTimeout)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "annas-mcp-test", Version: "0.0.0"}, options)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
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
