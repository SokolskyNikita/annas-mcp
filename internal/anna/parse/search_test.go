package parse

import (
	"reflect"
	"strings"
	"testing"
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
	noLink := htmlDocument(t, `<div class="js-aarecord-list-outer"><script>var x = 1;</script><div><a class="js-vim-focus" href="https://other.example/md5/x">Title</a></div><div><a class="js-vim-focus" href="/md5/x"></a></div></div>`)
	if cards := SearchCards(noLink); len(cards) != 0 {
		t.Fatalf("a card linking off the archive was read: %+v", cards)
	}
}

func TestRecordPath(t *testing.T) {
	t.Parallel()

	for href, want := range map[string][2]string{
		"/md5/0123":              {"md5", "0123"},
		"/isbndb/9780521879286/": {"isbndb", "9780521879286"},
		"/doi/10.1000/x":         {"doi", "10.1000/x"},
		"/ol/OL1M?x=1":           {"ol", "OL1M"},
	} {
		prefix, id, ok := RecordPath(href)
		if !ok || prefix != want[0] || id != want[1] {
			t.Errorf("RecordPath(%q) = %q, %q, %v", href, prefix, id, ok)
		}
	}
	for _, href := range []string{"https://annas.test/md5/x", "//annas.test/md5/x", "md5/x", "/md5/", "/md5", "/MD5/x", "/1x/y", "%zz"} {
		if _, _, ok := RecordPath(href); ok {
			t.Errorf("RecordPath(%q) accepted", href)
		}
	}
}

func TestPublisherAndEdition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ link, publisher, edition string }{
		{`<a href="/search?q=MIT Press">MIT Press, 2nd, 2019</a>`, "MIT Press", "2nd, 2019"},
		{`<a href="/search?q=">2019</a>`, "", "2019"},
		{`<a href="/search?q=Simon &amp; Schuster">Simon &amp; Schuster</a>`, "Simon & Schuster", ""},
		{`<a href="/search?q=Cambridge%20University%20Press">Cambridge University Press, 2008</a>`, "Cambridge University Press", "2008"},
		{`<a href="/search?q=Other">Cambridge University Press, 2008</a>`, "Cambridge University Press, 2008", ""},
		{`<a href="/elsewhere">Press, 2008</a>`, "Press, 2008", ""},
		{`<a href="/search?q=Press"></a>`, "", ""},
	} {
		publisher, edition := publisherAndEdition(htmlDocument(t, tc.link).Find("a"))
		if publisher != tc.publisher || edition != tc.edition {
			t.Errorf("%s: %q, %q; want %q, %q", tc.link, publisher, edition, tc.publisher, tc.edition)
		}
	}
}

// countPage is a page with count above its results list, as Anna's are.
func countPage(count string) string {
	return `<div>` + count + `</div><div><div class="js-aarecord-list-outer"></div></div>`
}

func TestResultsTotal(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		html              string
		total             int
		lowerBound, found bool
	}{
		{countPage(`Results 1-50 (500+ total)`), 500, true, true},
		{countPage(`Results 1-9 (9 total)`), 9, false, true},
		{countPage(`Results 4,951–5,000 (5,000+ total)`), 5000, true, true},
		// Pages are read in English: a translated count is not one.
		{countPage(`Résultats 51-100 (76 au total)`), 0, false, false},
		{countPage(`Results 1-50 (99999999999999999999 total)`), 0, false, false},
		{countPage(`No files found`), 0, false, false},
	} {
		total, lowerBound, found := ResultsTotal(htmlDocument(t, tc.html))
		if total != tc.total || lowerBound != tc.lowerBound || found != tc.found {
			t.Errorf("%s: %d %v %v", tc.html, total, lowerBound, found)
		}
	}
	if _, _, found := ResultsTotal(nil); found {
		t.Fatal("no page has a total")
	}
}

func TestShowsResults(t *testing.T) {
	t.Parallel()

	count := countPage("Results 1-50 (76 total)")
	if !ShowsResults(htmlDocument(t, count), 1) || !ShowsResults(htmlDocument(t, count), 2) {
		t.Fatal("a count covering the page was missed")
	}
	if ShowsResults(htmlDocument(t, count), 3) {
		t.Fatal("a count that ends before the page was read as results")
	}
	if !ShowsResults(htmlDocument(t, `<a href="/md5/x">file</a>`), 1) {
		t.Fatal("links to files were missed")
	}
	if ShowsResults(pageDocument(t, "search_empty.html.gz"), 1) || ShowsResults(nil, 1) {
		t.Fatal("an empty page shows results")
	}
}

func TestFacetValues(t *testing.T) {
	t.Parallel()

	doc := htmlDocument(t, `<input name="src" value="lgli"><input name="src" value="anti__lgli"><input name="src" value="lgli"><input name="src" value=""><input name="src" value="zlib"><input name="lang" value="en">`)
	if got := FacetValues(doc, "src"); !reflect.DeepEqual(got, []string{"lgli", "zlib"}) {
		t.Fatalf("FacetValues = %v", got)
	}
	if got := FacetValues(pageDocument(t, "search_books.html.gz"), "content"); len(got) != 8 {
		t.Fatalf("content facet = %v", got)
	}
}
