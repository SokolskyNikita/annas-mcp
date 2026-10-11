package anna

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestBuildSearchURLSendsFiltersExclusionsSortAndFields(t *testing.T) {
	t.Parallel()

	got := buildSearchURL("annas-archive.gl", "", SearchOptions{
		Format:       []string{"epub", "-pdf"},
		Language:     []string{"en", "zh-Hant"},
		Source:       []string{"-scihub"},
		Access:       []string{"aa_download"},
		Sort:         "newest",
		Descriptions: true,
		Fields:       []SearchField{{Name: "author", Value: "Michele Boldrin"}, {Name: "year", Value: "2008"}},
		Page:         3,
	})
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	want := url.Values{
		"q":    {""},
		"ext":  {"epub", "anti__pdf"},
		"lang": {"en", "zh-Hant"},
		"src":  {"anti__scihub"},
		"acc":  {"aa_download"},
		"sort": {"newest"},
		"desc": {"1"},
		"page": {"3"},
		// Anna numbers field terms from 1.
		"termtype_1": {"author"}, "termval_1": {"Michele Boldrin"},
		"termtype_2": {"year"}, "termval_2": {"2008"},
	}
	if !reflect.DeepEqual(parsed.Query(), want) {
		t.Fatalf("query = %v\nwant    %v", parsed.Query(), want)
	}
}

func TestResolveSearchSendsJournalArticlesToTheJournalsTab(t *testing.T) {
	t.Parallel()

	books := ResolveSearch(SearchOptions{Content: []string{"journal_article"}}, "")
	if books.Index != "journals" || len(books.Content) != 0 {
		t.Fatalf("journal_article should select the journals tab: %+v", books)
	}
	catalog := ResolveSearch(SearchOptions{Index: "meta", Content: []string{"journal_article", "book_any"}}, "")
	if catalog.Index != "meta" || !reflect.DeepEqual(catalog.Content, []string{"journal_article"}) {
		t.Fatalf("the metadata tab filters journal_article itself: %+v", catalog)
	}
	excluded := ResolveSearch(SearchOptions{Content: []string{"-book_fiction"}}, "journals")
	if excluded.Index != "" || !reflect.DeepEqual(excluded.Content, []string{"-book_fiction"}) {
		t.Fatalf("an exclusion is an explicit content filter: %+v", excluded)
	}
}

func TestParseCardMetaReadsEveryPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want cardMeta
	}{
		{
			line: "English [en] · EPUB · 3.8MB · 2008 · 📘 Book (non-fiction) · 🚀/lgli/zlib    · ",
			want: cardMeta{language: "English", format: "EPUB", size: "3.8MB", year: "2008", content: "book_nonfiction", sources: []string{"lgli", "zlib"}, fastDownload: true},
		},
		{
			line: "English [en] · PDF · 0.2MB · 2006 · 📄 Journal article · 🧬/🚀/lgli/scihub",
			want: cardMeta{language: "English", format: "PDF", size: "0.2MB", year: "2006", content: "journal_article", sources: []string{"lgli", "scihub"}, fastDownload: true},
		},
		{
			// Without a language the extension comes first.
			line: "PDF · 1.5MB · 2008 · 📕 Book (fiction) · 🚀/lgli",
			want: cardMeta{format: "PDF", size: "1.5MB", year: "2008", content: "book_fiction", sources: []string{"lgli"}, fastDownload: true},
		},
		{
			// No 🚀: the file has no fast download.
			line: "English [en] · German [de] · 7Z · 1.2GB · 💬 Comic book · lgli/lgrs",
			want: cardMeta{language: "English, German", format: "7Z", size: "1.2GB", content: "book_comic", sources: []string{"lgli", "lgrs"}},
		},
		{
			// A catalog record: no file, and a record id that looks like an extension.
			line: "Metadata · English [en] · 📗 Book (unknown) · ol · OL1M",
			want: cardMeta{language: "English", content: "book_unknown", sources: []string{"ol"}},
		},
		{
			// A lowercase label before the content type is not a source.
			line: "metadatos · Español [es] · 1999 · 📗\u00a0Libro (desconocido) · isbndb",
			want: cardMeta{language: "Español", year: "1999", content: "book_unknown", sources: []string{"isbndb"}},
		},
		{
			line: "Metadata · English [en] · 2008 · 📗 Book (unknown) · isbndb · ISBNdb 9780521879286",
			want: cardMeta{language: "English", year: "2008", content: "book_unknown", sources: []string{"isbndb"}},
		},
	}
	for _, tc := range tests {
		if got := parseCardMeta(tc.line); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseCardMeta(%q)\n got %+v\nwant %+v", tc.line, got, tc.want)
		}
	}
}

func searchFixtureClient(t *testing.T, fixture string, seen *url.Values) *Client {
	t.Helper()
	return NewClient(Config{
		BaseURL:       "https://annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/search" {
				t.Errorf("unexpected request %s", req.URL)
			}
			if seen != nil {
				*seen = req.URL.Query()
			}
			return fixtureResponse(t, req, http.StatusOK, fixture, "text/html", nil), nil
		})},
	})
}

func TestFindBookSeparatesPartialMatchesAndReadsCards(t *testing.T) {
	t.Parallel()

	var query url.Values
	client := searchFixtureClient(t, "search_filtered.html", &query)
	page, err := client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Source: []string{"lgli"}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := query["src"]; !reflect.DeepEqual(got, []string{"lgli"}) {
		t.Fatalf("src = %v", got)
	}
	if len(page.Results) != 2 || len(page.Partial) != 1 {
		t.Fatalf("results=%d partial=%d; the nav link and partial match must not count as results", len(page.Results), len(page.Partial))
	}
	if page.Total != 2 || page.TotalIsLowerBound || page.MorePages {
		t.Fatalf("totals = %d %v %v", page.Total, page.TotalIsLowerBound, page.MorePages)
	}
	first := page.Results[0]
	want := &Book{
		Title: "Against Intellectual Monopoly", Authors: "Michele Boldrin, David K. Levine",
		Publisher: "Cambridge University Press (Virtual Publishing), 1. publ, Cambridge, 2008",
		Language:  "English", Format: "EPUB", Size: "3.9MB", Year: "2008", Content: "book_nonfiction",
		Sources: []string{"lgli", "lgrs", "nexusstc", "zlib"}, FastDownload: true,
		Hash:        "224c5b1195fd2fb4650c3b75c2908d65",
		URL:         "https://annas.test/md5/224c5b1195fd2fb4650c3b75c2908d65",
		Description: "'Intellectual property' - patents and copyrights - have become controversial.",
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first result\n got %+v\nwant %+v", first, want)
	}
	flagged := page.Results[1]
	if !flagged.PossibleIssues || flagged.FastDownload || flagged.Format != "PDF" || flagged.Year != "" {
		t.Fatalf("flagged result = %+v", flagged)
	}
	if partial := page.Partial[0]; partial.Hash != "45af58ccb96c6b5c83979b36bdfe5870" || partial.Format != "MOBI" {
		t.Fatalf("partial match = %+v", partial)
	}
}

func TestFindArticleCarriesCardDetails(t *testing.T) {
	t.Parallel()

	page, err := searchFixtureClient(t, "search_filtered.html", nil).FindArticle(context.Background(), "monopoly", SearchOptions{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	paper := page.Results[0]
	if paper.Year != "2008" || !paper.FastDownload || paper.Journal == "" || len(paper.Sources) != 4 || len(page.Partial) != 1 {
		t.Fatalf("paper = %+v", paper)
	}
}

func TestSearchRejectsFilterValuesThePageDoesNotOffer(t *testing.T) {
	t.Parallel()

	client := searchFixtureClient(t, "search_filtered.html", nil)
	// Anna answers an unknown source with partial matches only, which would
	// look like the book being absent.
	_, err := client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Source: []string{"lgli", "-libgen"}}, time.Second)
	if apperr.CodeOf(err) != apperr.InvalidArgument || !strings.Contains(err.Error(), `"libgen"`) || !strings.Contains(err.Error(), "lgli, lgrs, zlib") {
		t.Fatalf("err = %v", err)
	}
	_, err = client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Content: []string{"magazine"}}, time.Second)
	if apperr.CodeOf(err) != apperr.InvalidArgument || !strings.Contains(err.Error(), "content") {
		t.Fatalf("err = %v", err)
	}
	// Pages without a facet are not checked.
	if _, err := searchFixtureClient(t, "search_card.html", nil).FindBook(context.Background(), "example", SearchOptions{Source: []string{"anything"}}, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestFindMetadataReturnsCatalogRecords(t *testing.T) {
	t.Parallel()

	var query url.Values
	page, err := searchFixtureClient(t, "search_metadata.html", &query).FindMetadata(context.Background(), "9780521879286", SearchOptions{Page: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("index") != "meta" {
		t.Fatalf("index = %q", query.Get("index"))
	}
	if page.Total != 500 || !page.TotalIsLowerBound || !page.MorePages {
		t.Fatalf("totals = %d %v %v", page.Total, page.TotalIsLowerBound, page.MorePages)
	}
	want := []*MetadataRecord{
		{
			ID: "isbndb:9780521879286", Title: "Against Intellectual Monopoly", Authors: "Boldrin, Michele, Levine, David K.",
			Publisher: "Cambridge University Press", Edition: "1, 2008", Language: "English", Year: "2008",
			Description: "Patents and copyrights have become controversial.", URL: "https://annas.test/isbndb/9780521879286",
		},
		{
			ID: "ol:OL18017946M", Title: "Against intellectual monopoly", Authors: "Michele Boldrin, David K. Levine",
			Language: "English", URL: "https://annas.test/ol/OL18017946M",
		},
	}
	if !reflect.DeepEqual(page.Results, want) {
		t.Fatalf("records\n got %+v %+v\nwant %+v %+v", page.Results[0], page.Results[1], want[0], want[1])
	}
}

func TestPageTotalsWithoutResultsLine(t *testing.T) {
	t.Parallel()

	page := SearchPage[int]{Results: make([]int, resultsPerPage)}
	pageTotals(&page, fixtureDocument(t, "search_card.html"), 2)
	if page.Total != 2*resultsPerPage || !page.TotalIsLowerBound || !page.MorePages {
		t.Fatalf("a full page without a count should suggest more: %+v", page)
	}
	last := SearchPage[int]{Results: make([]int, resultsPerPage)}
	pageTotals(&last, fixtureDocument(t, "search_card.html"), maxResultPages)
	if last.MorePages {
		t.Fatal("Anna serves at most 100 pages")
	}
}

func TestSearchNeedsAQueryOrAField(t *testing.T) {
	t.Parallel()

	var query url.Values
	client := searchFixtureClient(t, "search_filtered.html", &query)
	if _, err := client.FindBook(context.Background(), " ", SearchOptions{}, time.Second); apperr.CodeOf(err) != apperr.InvalidArgument {
		t.Fatalf("err = %v", err)
	}
	if _, err := client.FindBook(context.Background(), "", SearchOptions{Fields: []SearchField{{Name: "author", Value: "Boldrin"}}}, time.Second); err != nil {
		t.Fatal(err)
	}
	if query.Get("termval_1") != "Boldrin" || query.Get("q") != "" {
		t.Fatalf("query = %v", query)
	}
}

func TestSearchPageWithResultsItCannotReadIsAnError(t *testing.T) {
	t.Parallel()
	// Anna renamed the card classes: the count and the file links remain,
	// but no card parses.
	drifted := strings.ReplaceAll(fixtureText(t, "search_filtered.html"), "custom-a block", "result-link")
	for name, test := range map[string]struct {
		page    string
		pageNum int
		wantErr bool
	}{
		"results that do not parse":          {drifted, 1, true},
		"no results":                         {fixtureText(t, "archive_page.html"), 1, false},
		"a page past the end of the results": {strings.ReplaceAll(drifted, "/md5/", "/nothing/"), 99, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				response := fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil)
				response.Body, response.ContentLength = io.NopCloser(strings.NewReader(test.page)), int64(len(test.page))
				return response, nil
			})}})
			page, err := client.FindBook(context.Background(), "example", SearchOptions{Page: test.pageNum}, time.Minute)
			if test.wantErr {
				if apperr.CodeOf(err) != apperr.Upstream || !strings.Contains(err.Error(), "cannot read") {
					t.Fatalf("unreadable results: %+v %v", page, err)
				}
				return
			}
			if err != nil || len(page.Results) != 0 {
				t.Fatalf("empty page: %+v %v", page, err)
			}
		})
	}
}
