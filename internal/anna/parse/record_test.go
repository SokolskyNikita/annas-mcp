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
