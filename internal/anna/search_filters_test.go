package anna

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
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
	client := searchFixtureClient(t, "pages/search_filtered_partial.html.gz", &query)
	page, err := client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Source: []string{"lgli"}}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got := query["src"]; !reflect.DeepEqual(got, []string{"lgli"}) {
		t.Fatalf("src = %v", got)
	}
	// Nine EPUBs match the filters; Anna adds 41 looser matches to the short page.
	if len(page.Results) != 9 || len(page.Partial) != 41 {
		t.Fatalf("results=%d partial=%d", len(page.Results), len(page.Partial))
	}
	if page.Total != 9 || page.TotalIsLowerBound || page.MorePages {
		t.Fatalf("totals = %d %v %v", page.Total, page.TotalIsLowerBound, page.MorePages)
	}
	first := page.Results[0]
	want := &Book{
		Title: "Against Intellectual Monopoly", Authors: "Michele Boldrin; David K. Levine",
		Publisher: "chenjin5.com 万千书友聚集地", Edition: "2008",
		Language: "English", Format: "EPUB", Size: "3.8MB", Year: "2008", Content: "book_nonfiction",
		Sources: []string{"lgli", "zlib"}, FastDownload: true,
		Hash: "80ea470d302a6c9aadaf0e769e71c14f",
		URL:  "https://annas.test/md5/80ea470d302a6c9aadaf0e769e71c14f",
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("first result\n got %+v\nwant %+v", first, want)
	}
	for _, book := range page.Results {
		if book.Format != "EPUB" {
			t.Fatalf("a result ignores the format filter: %+v", book)
		}
	}
	if partial := page.Partial[0]; partial.Hash != "45af58ccb96c6b5c83979b36bdfe5870" || partial.Format != "MOBI" {
		t.Fatalf("partial match = %+v", partial)
	}
}

func TestFindBookFlagsFilesWithPossibleIssues(t *testing.T) {
	t.Parallel()

	page, err := searchFixtureClient(t, "pages/search_books.html.gz", nil).FindBook(context.Background(), "sicp", SearchOptions{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var flagged []string
	for _, book := range page.Results {
		if book.PossibleIssues {
			flagged = append(flagged, book.Hash)
		}
	}
	// Anna greys out seven of the 50 cards.
	if len(flagged) != 7 || flagged[0] != "d6ff202b668d37545b9c187ce35f5420" {
		t.Fatalf("flagged = %v", flagged)
	}
}

func TestFindArticleCarriesCardDetails(t *testing.T) {
	t.Parallel()

	page, err := searchFixtureClient(t, searchJournalsPage, nil).FindArticle(context.Background(), "attention is all you need", SearchOptions{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	paper := page.Results[0]
	if paper.Title != "Equity-premium prediction: attention is all you need." || paper.Year != "2022" || !paper.FastDownload ||
		paper.Journal != "Wiley, Journal of Applied Econometrics, 38, 2022" || len(paper.Sources) == 0 || len(page.Partial) != 39 {
		t.Fatalf("paper = %+v", paper)
	}
}

func TestSearchRejectsFilterValuesThePageDoesNotOffer(t *testing.T) {
	t.Parallel()

	client := searchFixtureClient(t, "pages/search_filtered_partial.html.gz", nil)
	// Anna answers an unknown source with partial matches only, which would
	// look like the book being absent.
	_, err := client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Source: []string{"lgli", "-libgen"}}, time.Second)
	if apperr.CodeOf(err) != apperr.InvalidArgument || !strings.Contains(err.Error(), `"libgen"`) || !strings.Contains(err.Error(), "lgli, lgrs, magzdb") {
		t.Fatalf("err = %v", err)
	}
	_, err = client.FindBook(context.Background(), "against intellectual monopoly", SearchOptions{Content: []string{"podcast"}}, time.Second)
	if apperr.CodeOf(err) != apperr.InvalidArgument || !strings.Contains(err.Error(), "content") {
		t.Fatalf("err = %v", err)
	}
	// Pages without a facet are not checked.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(searchPageWith(t, capturedCard(t, searchAttentionPage, attentionHash))))
	if err != nil {
		t.Fatal(err)
	}
	doc.Find("input[name='src']").Remove()
	withoutFacet, err := doc.Html()
	if err != nil {
		t.Fatal(err)
	}
	unchecked := NewClient(Config{BaseURL: "https://annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return htmlResponse(req, http.StatusOK, withoutFacet), nil
	})}})
	if _, err := unchecked.FindBook(context.Background(), "example", SearchOptions{Source: []string{"anything"}}, time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestFindMetadataReturnsCatalogRecords(t *testing.T) {
	t.Parallel()

	var query url.Values
	page, err := searchFixtureClient(t, "pages/search_meta.html.gz", &query).FindMetadata(context.Background(), "against intellectual monopoly", SearchOptions{Page: 1}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if query.Get("index") != "meta" {
		t.Fatalf("index = %q", query.Get("index"))
	}
	if len(page.Results) != 50 || page.Total != 158 || page.TotalIsLowerBound || !page.MorePages {
		t.Fatalf("results = %d, totals = %d %v %v", len(page.Results), page.Total, page.TotalIsLowerBound, page.MorePages)
	}
	want := &MetadataRecord{
		ID: "isbndb:9780511415449", Title: "Against Intellectual Monopoly", Authors: "Boldrin, Michele, Levine, David",
		Publisher: "Cambridge University Press", Language: "English", URL: "https://annas.test/isbndb/9780511415449",
	}
	if !reflect.DeepEqual(page.Results[1], want) {
		t.Fatalf("record\n got %+v\nwant %+v", page.Results[1], want)
	}
	for _, record := range page.Results {
		if strings.HasPrefix(record.ID, "md5:") {
			t.Fatalf("a file appeared among catalog records: %+v", record)
		}
	}
}

func TestPageTotalsWithoutResultsLine(t *testing.T) {
	t.Parallel()

	page := SearchPage[int]{Results: make([]int, parse.ResultsPerPage)}
	pageTotals(&page, fixtureDocument(t, "pages/search_past_end.html.gz"), 2)
	if page.Total != 2*parse.ResultsPerPage || !page.TotalIsLowerBound || !page.MorePages {
		t.Fatalf("a full page without a count should suggest more: %+v", page)
	}
	last := SearchPage[int]{Results: make([]int, parse.ResultsPerPage)}
	pageTotals(&last, fixtureDocument(t, "pages/search_past_end.html.gz"), parse.MaxResultPages)
	if last.MorePages {
		t.Fatal("Anna serves at most 100 pages")
	}
}

func TestSearchNeedsAQueryOrAField(t *testing.T) {
	t.Parallel()

	var query url.Values
	client := searchFixtureClient(t, "pages/search_fields.html.gz", &query)
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
	drifted := strings.ReplaceAll(fixtureText(t, "pages/search_filtered_partial.html.gz"), "custom-a block", "result-link")
	for name, test := range map[string]struct {
		page    string
		pageNum int
		wantErr bool
	}{
		"results that do not parse":          {drifted, 1, true},
		"no results":                         {fixtureText(t, noResultsPage), 1, false},
		"a page past the end of the results": {fixtureText(t, "pages/search_past_end.html.gz"), 3, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return htmlResponse(req, http.StatusOK, test.page), nil
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
