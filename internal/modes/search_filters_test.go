package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func callTool[T any](t *testing.T, archive *fakeArchive, name string, arguments map[string]any) T {
	t.Helper()
	result, err := connectTestServer(t, archive).CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("%s failed: %s", name, toolText(t, result))
	}
	var decoded T
	if err := json.Unmarshal([]byte(toolText(t, result)), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestBookSearchFiltersReachTheArchiveNormalized(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	callTool[searchResult[*anna.Book]](t, archive, "book_search", map[string]any{
		"author": "  Michele   Boldrin ", "year": "2008", "format": "EPUB, .mobi, -pdf", "language": "en,ZH-hant,-de",
		"source": "LGLI,-scihub", "content": "book_nonfiction", "sort": "Newest", "downloadable_only": true, "search_descriptions": true,
	})
	want := anna.SearchOptions{
		Page: 1, Sort: "newest", Descriptions: true,
		Content: []string{"book_nonfiction"}, Language: []string{"en", "zh-Hant", "-de"},
		Format: []string{"epub", "mobi", "-pdf"}, Source: []string{"lgli", "-scihub"}, Access: []string{"aa_download"},
		Fields: []anna.SearchField{{Name: "author", Value: "Michele Boldrin"}, {Name: "year", Value: "2008"}},
	}
	if archive.query != "" || !reflect.DeepEqual(archive.options, want) {
		t.Fatalf("query %q options\n got %+v\nwant %+v", archive.query, archive.options, want)
	}
}

func TestArticleAndMetadataSearchesUseTheirTabs(t *testing.T) {
	t.Parallel()
	articles := &fakeArchive{}
	callTool[searchResult[*anna.Paper]](t, articles, "article_search", map[string]any{"query": "attention", "downloadable_only": true, "sort": "relevance"})
	if articles.options.Index != "journals" || !reflect.DeepEqual(articles.options.Access, []string{"aa_download", "aa_scidb"}) || articles.options.Sort != "" {
		t.Fatalf("article options = %+v", articles.options)
	}
	catalog := &fakeArchive{records: []*anna.MetadataRecord{{ID: "isbndb:9780521879286", Title: "Against Intellectual Monopoly"}}}
	page := callTool[searchResult[*anna.MetadataRecord]](t, catalog, "metadata_search", map[string]any{"title": "Against Intellectual Monopoly", "source": "isbndb,ol"})
	if catalog.options.Index != "meta" || !reflect.DeepEqual(catalog.options.Source, []string{"isbndb", "ol"}) || catalog.options.Fields[0].Name != "title" {
		t.Fatalf("metadata options = %+v", catalog.options)
	}
	if len(page.Results) != 1 || page.Results[0].ID != "isbndb:9780521879286" {
		t.Fatalf("metadata page = %+v", page)
	}
}

func TestPartialMatchesOnlyFillRoomLeftByExactResults(t *testing.T) {
	t.Parallel()
	books := func(prefix string, n int) []*anna.Book {
		out := make([]*anna.Book, n)
		for i := range out {
			out[i] = &anna.Book{Title: fmt.Sprint(prefix, i)}
		}
		return out
	}
	archive := &fakeArchive{books: books("exact", 3), partial: books("partial", 20), total: 3}
	page := callTool[searchResult[*anna.Book]](t, archive, "book_search", map[string]any{"query": "example"})
	if page.Matched != 3 || page.Limit != 3 || page.Total != 3 || page.MorePages || len(page.PartialMatches) != 7 {
		t.Fatalf("page = %+v (partial %d)", page, len(page.PartialMatches))
	}
	full := &fakeArchive{books: books("exact", 12), partial: books("partial", 5), total: 120}
	page = callTool[searchResult[*anna.Book]](t, full, "book_search", map[string]any{"query": "example"})
	if len(page.Results) != 10 || page.PartialMatches != nil || page.Total != 120 {
		t.Fatalf("a full page needs no partial matches: %+v", page)
	}
}

func TestCLINewCommandsAndFlags(t *testing.T) {
	t.Parallel()
	run := func(archive *fakeArchive, args ...string) string {
		t.Helper()
		var output bytes.Buffer
		cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
		cmd.SetOut(&output)
		cmd.SetErr(&output)
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return output.String()
	}

	archive := &fakeArchive{books: []*anna.Book{{Title: "Exact", FastDownload: true}}, partial: []*anna.Book{{Title: "Loose"}}, total: 51}
	out := run(archive, "book-search", "--author", "Boldrin", "--format", "epub", "--source", "-scihub", "--sort", "oldest", "--downloadable-only")
	if archive.query != "" || archive.options.Fields[0].Value != "Boldrin" || archive.options.Format[0] != "epub" || archive.options.Sort != "oldest" || archive.options.Access[0] != "aa_download" {
		t.Fatalf("options = %+v", archive.options)
	}
	for _, want := range []string{"Book 1:\nTitle: Exact", "Fast download: yes", "Partial matches", "Title: Loose", "showing 1 of 51 results"} {
		if !strings.Contains(out, want) {
			t.Errorf("book-search output lacks %q:\n%s", want, out)
		}
	}

	catalog := &fakeArchive{records: []*anna.MetadataRecord{{ID: "ol:OL1M", Title: "Catalog Title"}}}
	if out := run(catalog, "metadata-search", "--year", "2008", "--title", "Monopoly"); !strings.Contains(out, "ID: ol:OL1M") || catalog.options.Index != "meta" {
		t.Errorf("metadata-search output:\n%s", out)
	}
	details := &fakeArchive{}
	if out := run(details, "book-details", "isbndb:9780521879286"); !strings.Contains(out, "Title: Example Details") || details.detailID != "isbndb:9780521879286" {
		t.Errorf("book-details output:\n%s", out)
	}
	if out := run(&fakeArchive{}, "account-status"); !strings.Contains(out, "Fast downloads left: 7") || !strings.Contains(out, "25 per 18 hours") {
		t.Errorf("account-status output:\n%s", out)
	}
	if out := run(&fakeArchive{}, "account-status", "--json"); !strings.Contains(out, `"downloads_left":7`) {
		t.Errorf("account-status JSON:\n%s", out)
	}
}

func TestCLIDownloadReportsDownloadsLeft(t *testing.T) {
	t.Parallel()
	left := 41
	var output bytes.Buffer
	if err := printDownloadResult(&output, anna.DownloadResult{Path: "/tmp/book.epub", Bytes: 1, DownloadsLeft: &left}, "Book", false); err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != "Book downloaded successfully to: /tmp/book.epub (41 fast downloads left)\n" {
		t.Fatalf("output = %q", got)
	}
}
