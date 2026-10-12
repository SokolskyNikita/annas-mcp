package parse

import (
	"reflect"
	"testing"
)

func TestRecordIdentityUsesRecordFieldsNotCitations(t *testing.T) {
	t.Parallel()

	doc := snippetDocument(t, "record_citation_mention.html")

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

// recordCases are the record page parts in testdata/cases/record.json.
type recordCases struct {
	Comment string
	DOIs    []struct {
		Name, HTML, DOI string
		Conflict        bool
	}
	Years []struct{ In, Want string }
}

func TestRecordIdentityReadsStructuredDOIAndDetectsConflict(t *testing.T) {
	t.Parallel()

	var cases recordCases
	loadCases(t, "record", &cases)
	for _, test := range cases.DOIs {
		t.Run(test.Name, func(t *testing.T) {
			identity := RecordIdentity(htmlDocument(t, test.HTML))
			if identity.DOI != test.DOI || identity.DOIConflict != test.Conflict {
				t.Fatalf("identity = %+v, want DOI %q conflict %v", identity, test.DOI, test.Conflict)
			}
		})
	}
}

func TestYearValue(t *testing.T) {
	t.Parallel()

	var cases recordCases
	loadCases(t, "record", &cases)
	for _, tc := range cases.Years {
		if got := yearValue(tc.In); got != tc.Want {
			t.Errorf("yearValue(%q) = %q, want %q", tc.In, got, tc.Want)
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
	ignored := RecordIdentity(snippetDocument(t, "record_citation_tags.html"))
	if ignored.Title != "Page Title" || ignored.Authors != nil || ignored.DOI != "" {
		t.Fatalf("read parts that are not the record's: %+v", ignored)
	}
	if untitled := RecordIdentity(snippetDocument(t, "record_site_name.html")); untitled.Title != "" {
		t.Fatalf("the site's name was read as the title: %+v", untitled)
	}
	codes := RecordIdentity(snippetDocument(t, "record_codes.html"))
	if codes.ArxivID != "1706.03762" || codes.Year != "2017" || codes.DOI != "" {
		t.Fatalf("codes: %+v", codes)
	}
}
