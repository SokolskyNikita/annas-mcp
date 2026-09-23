package anna

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseArchiveIdentityUsesRecordFieldsNotCitations(t *testing.T) {
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

	identity := parseArchiveIdentity(doc)
	if identity.Title != "Attention Is All You Need" {
		t.Fatalf("record title = %q", identity.Title)
	}
	if !sameAuthorSet([]string{"Ashish Vaswani", "Noam Shazeer"}, identity.Authors) {
		t.Fatalf("record authors = %v", identity.Authors)
	}
	if identity.DOI != "" {
		t.Fatalf("citation mention was treated as record DOI: %+v", identity)
	}
	if identity.Year != "2017" {
		t.Fatalf("record year = %q", identity.Year)
	}
}

func TestParseArchiveIdentityReadsStructuredDOIAndDetectsConflict(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name         string
		codes        string
		wantDOI      string
		wantConflict bool
	}{
		{name: "one DOI", codes: `<a href="/search?q=doi%3A10.1000%2Frecord">DOI</a>`, wantDOI: "10.1000/record"},
		{name: "conflicting DOI fields", codes: `<a href="/search?q=doi%3A10.1000%2Frecord">DOI</a><a href="/search?q=doi%3A10.1000%2Fother">DOI</a>`, wantDOI: "10.1000/record", wantConflict: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<div class="js-md5-codes-container">` + test.codes + `</div>`))
			if err != nil {
				t.Fatal(err)
			}
			identity := parseArchiveIdentity(doc)
			if identity.DOI != test.wantDOI || identity.DOIConflict != test.wantConflict {
				t.Fatalf("identity = %+v, want DOI %q conflict %v", identity, test.wantDOI, test.wantConflict)
			}
		})
	}
}

func TestVerifyDOIIdentityRequiresFullTitleAndAuthorsWithoutIdentifier(t *testing.T) {
	t.Parallel()

	doi := "10.1000/example"
	metadata := doiMetadata{
		DOI:     doi,
		Title:   "Attention Is All You Need",
		Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar", "Jakob Uszkoreit", "Llion Jones", "Aidan N. Gomez", "Łukasz Kaiser", "Illia Polosukhin"},
		Year:    "2017",
	}
	record := archiveIdentity{
		Title:   metadata.Title,
		Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar", "Jakob Uszkoreit", "Llion Jones", "Aidan N. Gomez", "Lukasz Kaiser", "Illia Polosukhin"},
		Year:    "2017",
	}
	if !verifyDOIIdentity(doi, metadata, record) {
		t.Fatal("matching CSL title, full authors, and year should verify")
	}

	wrongAuthors := record
	wrongAuthors.Authors = append([]string(nil), record.Authors...)
	wrongAuthors.Authors[len(wrongAuthors.Authors)-1] = "A Different Author"
	if verifyDOIIdentity(doi, metadata, wrongAuthors) {
		t.Fatal("same title with a different author list must be rejected")
	}
	missingAuthors := record
	missingAuthors.Authors = nil
	if verifyDOIIdentity(doi, metadata, missingAuthors) {
		t.Fatal("title-only fallback must be rejected")
	}
	wrongYear := record
	wrongYear.Year = "2018"
	if verifyDOIIdentity(doi, metadata, wrongYear) {
		t.Fatal("mismatched publication years must be rejected")
	}
	wrongDOI := record
	wrongDOI.DOI = "10.1000/other"
	if verifyDOIIdentity(doi, metadata, wrongDOI) {
		t.Fatal("a conflicting record DOI must be rejected even when title and authors match")
	}
}

func TestVerifyDOIIdentityAcceptsExactRecordDOIWithoutOptionalAuthors(t *testing.T) {
	t.Parallel()

	doi := "10.1000/example"
	metadata := doiMetadata{DOI: doi, Title: "A Paper", Authors: []string{"An Author"}}
	if !verifyDOIIdentity(doi, metadata, archiveIdentity{DOI: doi, Title: "A Paper"}) {
		t.Fatal("an exact structured record DOI should not require optional author fields")
	}
	if verifyDOIIdentity(doi, metadata, archiveIdentity{DOI: doi, DOIConflict: true, Title: "A Paper"}) {
		t.Fatal("conflicting structured DOIs must be rejected")
	}
}

func TestBuildSciDBURLPreservesBalancedDOISuffix(t *testing.T) {
	t.Parallel()

	input := "https://doi.org/10.1000/example(suffix)"
	doi := NormalizeDOI(input)
	if doi != "10.1000/example(suffix)" {
		t.Fatalf("normalized DOI suffix = %q", doi)
	}
	page, err := url.Parse(buildSciDBURL("annas.test", doi))
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != "/scidb/10.1000/example(suffix)" || page.RawQuery != "" || page.Fragment != "" {
		t.Fatalf("SciDB URL corrupted DOI suffix: %s", page.String())
	}
}

func TestArxivIDRequiresCanonicalDOIPrefixAndPreservesSuffix(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		doi, want string
	}{
		{"10.48550/arXiv.1706.03762", "1706.03762"},
		{"https://doi.org/10.48550/arXiv.1706.03762v3", "1706.03762"},
		{"10.48550/arXiv.1706.03762.", "1706.03762."},
		{"10.1234/example.arxiv.1706.03762", ""},
		{"10.48550/other.1706.03762", ""},
		{"prefix-10.48550/arxiv.1706.03762", ""},
		{"10.48550/arxiv.", ""},
	} {
		if got := arxivID(test.doi); got != test.want {
			t.Errorf("arxivID(%q) = %q, want %q", test.doi, got, test.want)
		}
	}
}

func TestNormalizeArxivIDStripsOnlyExplicitNumericVersion(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"arxiv:1706.03762v2":       "1706.03762",
		"1706.03762.":              "1706.03762.",
		"notarxiv.1706.03762":      "notarxiv.1706.03762",
		"10.1234/arxiv.1706.03762": "10.1234/arxiv.1706.03762",
	} {
		if got := normalizeArxivID(input); got != want {
			t.Errorf("normalizeArxivID(%q) = %q, want %q", input, got, want)
		}
	}
}
