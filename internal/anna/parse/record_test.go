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
		<div class="js-aarecord-list-fallback-cover"><div data-content="Attention Is All You Need"></div><div data-content="Ashish Vaswani, Noam Shazeer"></div></div>
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
	want := RecordDetails{
		Title: "Equity-premium prediction: attention is all you need.", Authors: "Luiz Renato Lima; Lucas Lúcio Godeiro",
		Publisher: "Wiley", Edition: "Journal of Applied Econometrics, 38, 2022",
		Meta: CardMeta{Language: "English", Format: "PDF", Size: "1.4MB", Year: "2022", Content: "journal_article", Sources: []string{"nexusstc", "upload"}, FastDownload: true},
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("details\n got %+v\nwant %+v", details, want)
	}
	if details := ReadRecordDetails(nil); !reflect.DeepEqual(details, RecordDetails{}) {
		t.Fatalf("no page: %+v", details)
	}
}

func TestRecordIdentityReadsOnlyTheRecordsOwnParts(t *testing.T) {
	t.Parallel()

	if identity := RecordIdentity(nil); !reflect.DeepEqual(identity, Identity{}) {
		t.Fatalf("no page: %+v", identity)
	}
	// Anna emits no citation meta tags, and its only <h1> is the site's
	// name; neither is read as the record's.
	ignored := RecordIdentity(htmlDocument(t, `<title>Page Title - Anna’s Archive</title><h1>Anna’s Archive</h1>
		<meta name="citation_title" content="Meta Title"><meta name="citation_author" content="A Author"><meta name="citation_doi" content="10.1000/meta">`))
	if ignored.Title != "Page Title" || ignored.Authors != nil || ignored.DOI != "" {
		t.Fatalf("read parts that are not the record's: %+v", ignored)
	}
	if untitled := RecordIdentity(htmlDocument(t, `<title>Anna’s Archive</title><h1>Anna’s Archive</h1>`)); untitled.Title != "" {
		t.Fatalf("the site's name was read as the title: %+v", untitled)
	}
	codes := RecordIdentity(htmlDocument(t, `<div class="js-md5-codes-container">
		<a href='/search?q="arxiv:1706.03762v2"'>arxiv</a><a href='/search?q="year:2017"'>year</a>
		<a href="/search?q=%zz">bad</a><a href="/search?q=nokey">bad</a><a href="/search?q=doi:">empty</a>
		<a href='/search?q="doi:10.0000/0123456789abcdef0123456789abcdef"'>placeholder</a></div>`))
	if codes.ArxivID != "1706.03762" || codes.Year != "2017" || codes.DOI != "" {
		t.Fatalf("codes: %+v", codes)
	}
}
