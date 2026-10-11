package parse

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestRecordIdentityUsesRecordFieldsNotCitations(t *testing.T) {
	t.Parallel()

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<!doctype html>
		<title>Attention Is All You Need - Anna’s Archive</title>
		<meta name="description" content="A citation to 10.9999/unrelated">
		<a href="/search?q=doi%3A10.9999%2Funrelated">citation DOI</a>
		<div data-content="Attention Is All You Need" class="font-bold text-violet-900"></div>
		<div data-content="Ashish Vaswani, Noam Shazeer" class="font-bold text-amber-900"></div>
		<div class="js-md5-codes-container hidden">
			<div id="md5-codes-panel-year"><span>Year:</span><span class="bg-gray-200">2017</span>
				<a href="/search?q=year%3A2017">Search year</a></div>
		</div>`))
	if err != nil {
		t.Fatal(err)
	}

	identity := RecordIdentity(doc)
	if identity.Title != "Attention Is All You Need" {
		t.Fatalf("record title = %q", identity.Title)
	}
	if !reflect.DeepEqual(identity.Authors, []string{"Ashish Vaswani", "Noam Shazeer"}) {
		t.Fatalf("record authors = %v", identity.Authors)
	}
	if identity.DOI != "" {
		t.Fatalf("citation mention was treated as record DOI: %+v", identity)
	}
	if identity.Year != "2017" {
		t.Fatalf("record year = %q", identity.Year)
	}
}

func TestRecordIdentityReadsStructuredDOIAndDetectsConflict(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		codes        string
		wantDOI      string
		wantConflict bool
	}{
		{name: "one DOI", codes: `<a href="/search?q=doi%3A10.1000%2Frecord">DOI</a>`, wantDOI: "10.1000/record"},
		{name: "exact search link", codes: `<a href='/search?q="doi:10.1000/record"'>DOI</a>`, wantDOI: "10.1000/record"},
		{name: "conflicting DOI fields", codes: `<a href="/search?q=doi%3A10.1000%2Frecord">DOI</a><a href="/search?q=doi%3A10.1000%2Fother">DOI</a>`, wantDOI: "10.1000/record", wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div class="js-md5-codes-container">` + test.codes + `</div>`))
			if err != nil {
				t.Fatal(err)
			}
			identity := RecordIdentity(doc)
			if identity.DOI != test.wantDOI || identity.DOIConflict != test.wantConflict {
				t.Fatalf("identity = %+v, want DOI %q conflict %v", identity, test.wantDOI, test.wantConflict)
			}
		})
	}
}

func TestRecordIdentityFallbacks(t *testing.T) {
	t.Parallel()

	if identity := RecordIdentity(nil); !reflect.DeepEqual(identity, Identity{}) {
		t.Fatalf("no page: %+v", identity)
	}
	meta := RecordIdentity(htmlDocument(t, `<meta name="citation_title" content="Meta Title"><meta name="citation_author" content="A Author">
		<meta name="citation_doi" content="10.1000/meta"><meta name="citation_doi" content="10.0000/0123456789abcdef0123456789abcdef"><meta name="citation_publication_date" content="2001-02-03">`))
	if meta.Title != "Meta Title" || !reflect.DeepEqual(meta.Authors, []string{"A Author"}) || meta.DOI != "10.1000/meta" || meta.Year != "2001" || meta.DOIConflict {
		t.Fatalf("meta fallbacks: %+v", meta)
	}
	heading := RecordIdentity(htmlDocument(t, `<h1>Heading</h1><a href="/search?q=x"><span class="icon-[mdi--user-edit]"></span> Link Author</a><time datetime="1999-01-01"></time>`))
	if heading.Title != "Heading" || !reflect.DeepEqual(heading.Authors, []string{"Link Author"}) || heading.Year != "1999" {
		t.Fatalf("heading fallbacks: %+v", heading)
	}
	titled := RecordIdentity(htmlDocument(t, `<title>Page Title - Anna’s Archive</title>`))
	if titled.Title != "Page Title" {
		t.Fatalf("title fallback: %+v", titled)
	}
	codes := RecordIdentity(htmlDocument(t, `<div class="js-md5-codes-container">
		<a href='/search?q="arxiv:1706.03762v2"'>arxiv</a><a href='/search?q="year:2017"'>year</a>
		<a href="/search?q=%zz">bad</a><a href="/search?q=nokey">bad</a><a href="/search?q=doi:">empty</a>
		<a href='/search?q="doi:10.0000/0123456789abcdef0123456789abcdef"'>placeholder</a></div>`))
	if codes.ArxivID != "1706.03762" || codes.Year != "2017" || codes.DOI != "" {
		t.Fatalf("codes: %+v", codes)
	}
	panel := RecordIdentity(htmlDocument(t, `<div class="js-md5-codes-container"><div id="md5-codes-panel-3">Year: <span class="bg-gray-200">abc</span><span class="bg-gray-200">1987</span></div><div id="md5-codes-panel-4">ISBN</div></div>`))
	if panel.Year != "1987" {
		t.Fatalf("year panel: %+v", panel)
	}
}

func TestYearValue(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]string{"2017": "2017", " 2017-06-12 ": "2017", "17": "", "3000": "", "20a7": "", "": ""} {
		if got := yearValue(value); got != want {
			t.Errorf("yearValue(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestReadRecordDetails(t *testing.T) {
	t.Parallel()

	details := ReadRecordDetails(pageDocument(t, "record_equity_premium.html.gz"))
	if details.Title != "Equity-premium prediction: attention is all you need." || details.Authors != "Luiz Renato Lima; Lucas Lúcio Godeiro" || details.Journal != "Wiley" {
		t.Fatalf("details = %+v", details)
	}
	for description, journal := range map[string]string{"Only": "Only", "Author&#10;&#10;Publisher": "Publisher", "A&#10;&#10;B&#10;&#10;C": "C"} {
		got := ReadRecordDetails(htmlDocument(t, `<title>No suffix</title><meta name="description" content="`+description+`"><div class="text-gray-500">path</div><div class="text-gray-500">2.0MB</div>`))
		if got.Journal != journal || got.Title != "" || got.Size != "2.0MB" {
			t.Errorf("%q: %+v", description, got)
		}
	}
}
