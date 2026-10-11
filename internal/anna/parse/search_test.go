package parse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestSearchCardsOnCapturedPages(t *testing.T) {
	t.Parallel()

	count := func(cards []Card, keep func(Card) bool) int {
		n := 0
		for _, card := range cards {
			if keep(card) {
				n++
			}
		}
		return n
	}
	filtered := SearchCards(pageDocument(t, "search_filtered_partial.html.gz"))
	if exact, partial := count(filtered, func(c Card) bool { return !c.Partial }), count(filtered, func(c Card) bool { return c.Partial }); exact != 9 || partial != 41 {
		t.Fatalf("filtered search: %d exact, %d partial", exact, partial)
	}
	if flagged := count(SearchCards(pageDocument(t, "search_books.html.gz")), func(c Card) bool { return c.PossibleIssues }); flagged != 7 {
		t.Fatalf("flagged cards = %d", flagged)
	}
	catalog := SearchCards(pageDocument(t, "search_meta.html.gz"))
	if files := count(catalog, func(c Card) bool { return c.Prefix == "md5" }); files != 0 || len(catalog) != 50 {
		t.Fatalf("catalog search: %d cards, %d files", len(catalog), files)
	}
	languages := SearchCards(pageDocument(t, "search_languages.html.gz"))
	if count(languages, func(c Card) bool { return strings.Contains(c.Meta.Language, ", ") }) == 0 {
		t.Fatal("no card listed several languages")
	}
	formats := map[string]bool{}
	for _, card := range SearchCards(pageDocument(t, "search_rare_formats.html.gz")) {
		formats[card.Meta.Format] = true
	}
	for _, format := range []string{"ZIP", "RAR", "DJVU", "CBR"} {
		if !formats[format] {
			t.Errorf("no %s card among %v", format, formats)
		}
	}
	for _, name := range []string{"search_empty.html.gz", "search_past_end.html.gz"} {
		if cards := SearchCards(pageDocument(t, name)); len(cards) != 0 {
			t.Errorf("%s has %d cards", name, len(cards))
		}
	}
}

func TestSearchCardsSkipsMalformedCards(t *testing.T) {
	t.Parallel()

	if cards := SearchCards(accountDocument(t, "malformed_cards.html")); len(cards) != 0 {
		t.Fatalf("malformed cards: %+v", cards)
	}
	if cards := SearchCards(nil); cards != nil {
		t.Fatalf("no page: %+v", cards)
	}
	noLink := snippetDocument(t, "cards_off_archive.html")
	if cards := SearchCards(noLink); len(cards) != 0 {
		t.Fatalf("a card linking off the archive was read: %+v", cards)
	}
}

// searchCases are the parts of search pages in testdata/cases/search.json.
type searchCases struct {
	Comment     string
	RecordPaths struct {
		Valid   []struct{ Href, Prefix, ID string }
		Invalid []string
	} `json:"record_paths"`
	PublisherLinks []struct{ Link, Publisher, Edition string } `json:"publisher_links"`
	ResultCounts   []struct {
		Count      string
		Total      int
		LowerBound bool `json:"lower_bound"`
		Found      bool
	} `json:"result_counts"`
}

func loadSearchCases(t *testing.T) searchCases {
	t.Helper()
	var cases searchCases
	loadCases(t, "search", &cases)
	return cases
}

func TestRecordPath(t *testing.T) {
	t.Parallel()

	paths := loadSearchCases(t).RecordPaths
	for _, tc := range paths.Valid {
		prefix, id, ok := RecordPath(tc.Href)
		if !ok || prefix != tc.Prefix || id != tc.ID {
			t.Errorf("RecordPath(%q) = %q, %q, %v", tc.Href, prefix, id, ok)
		}
	}
	for _, href := range paths.Invalid {
		if _, _, ok := RecordPath(href); ok {
			t.Errorf("RecordPath(%q) accepted", href)
		}
	}
}

func TestPublisherAndEdition(t *testing.T) {
	t.Parallel()

	for _, tc := range loadSearchCases(t).PublisherLinks {
		link := htmlDocument(t, tc.Link).Find("a")
		publisher, edition := publisherAndEdition(link.Text(), link.AttrOr("href", ""))
		if publisher != tc.Publisher || edition != tc.Edition {
			t.Errorf("%s: %q, %q; want %q, %q", tc.Link, publisher, edition, tc.Publisher, tc.Edition)
		}
	}
}

// countPage is a page with count above its results list, as Anna's are.
func countPage(t *testing.T, count string) *goquery.Document {
	t.Helper()
	return htmlDocument(t, strings.ReplaceAll(snippet(t, "count_page.html"), "{{COUNT}}", count))
}

func TestResultsTotal(t *testing.T) {
	t.Parallel()

	for _, tc := range loadSearchCases(t).ResultCounts {
		total, lowerBound, found := ResultsTotal(countPage(t, tc.Count))
		if total != tc.Total || lowerBound != tc.LowerBound || found != tc.Found {
			t.Errorf("%s: %d %v %v", tc.Count, total, lowerBound, found)
		}
	}
	if _, _, found := ResultsTotal(nil); found {
		t.Fatal("no page has a total")
	}
}

func TestShowsResults(t *testing.T) {
	t.Parallel()

	count := countPage(t, "Results 1-50 (76 total)")
	if !ShowsResults(count, 1) || !ShowsResults(count, 2) {
		t.Fatal("a count covering the page was missed")
	}
	if ShowsResults(count, 3) {
		t.Fatal("a count that ends before the page was read as results")
	}
	if !ShowsResults(snippetDocument(t, "file_link.html"), 1) {
		t.Fatal("links to files were missed")
	}
	if ShowsResults(pageDocument(t, "search_empty.html.gz"), 1) || ShowsResults(nil, 1) {
		t.Fatal("an empty page shows results")
	}
}

func TestFacetValues(t *testing.T) {
	t.Parallel()

	doc := snippetDocument(t, "facet_inputs.html")
	if got := FacetValues(doc, "src"); !reflect.DeepEqual(got, []string{"lgli", "zlib"}) {
		t.Fatalf("FacetValues = %v", got)
	}
	if got := FacetValues(pageDocument(t, "search_books.html.gz"), "content"); len(got) != 8 {
		t.Fatalf("content facet = %v", got)
	}
}
