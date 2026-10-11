package match

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

func TestSameWorkRequiresFullTitleAndAuthorsWithoutIdentifier(t *testing.T) {
	t.Parallel()

	doi := "10.1000/example"
	metadata := parse.Citation{
		DOI:     doi,
		Title:   "Attention Is All You Need",
		Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar", "Jakob Uszkoreit", "Llion Jones", "Aidan N. Gomez", "Łukasz Kaiser", "Illia Polosukhin"},
		Year:    "2017",
	}
	record := parse.Identity{
		Title:   metadata.Title,
		Authors: []string{"Ashish Vaswani", "Noam Shazeer", "Niki Parmar", "Jakob Uszkoreit", "Llion Jones", "Aidan N. Gomez", "Lukasz Kaiser", "Illia Polosukhin"},
		Year:    "2017",
	}
	if !SameWork(doi, metadata, record) {
		t.Fatal("matching CSL title, full authors, and year should verify")
	}

	wrongAuthors := record
	wrongAuthors.Authors = append([]string(nil), record.Authors...)
	wrongAuthors.Authors[len(wrongAuthors.Authors)-1] = "A Different Author"
	if SameWork(doi, metadata, wrongAuthors) {
		t.Fatal("same title with a different author list must be rejected")
	}
	missingAuthors := record
	missingAuthors.Authors = nil
	if SameWork(doi, metadata, missingAuthors) {
		t.Fatal("title-only fallback must be rejected")
	}
	wrongYear := record
	wrongYear.Year = "2018"
	if SameWork(doi, metadata, wrongYear) {
		t.Fatal("mismatched publication years must be rejected")
	}
	wrongDOI := record
	wrongDOI.DOI = "10.1000/other"
	if SameWork(doi, metadata, wrongDOI) {
		t.Fatal("a conflicting record DOI must be rejected even when title and authors match")
	}
}

func TestSameWorkAcceptsExactRecordDOIWithoutOptionalAuthors(t *testing.T) {
	t.Parallel()

	doi := "10.1000/example"
	metadata := parse.Citation{DOI: doi, Title: "A Paper", Authors: []string{"An Author"}}
	if !SameWork(doi, metadata, parse.Identity{DOI: doi, Title: "A Paper"}) {
		t.Fatal("an exact structured record DOI should not require optional author fields")
	}
	if SameWork(doi, metadata, parse.Identity{DOI: doi, DOIConflict: true, Title: "A Paper"}) {
		t.Fatal("conflicting structured DOIs must be rejected")
	}
}

func TestSameWorkAcceptsThePublishedVersionOfAnArxivPreprint(t *testing.T) {
	t.Parallel()

	// DOI.org's metadata for arXiv:1412.6980 and the archive's ICLR record.
	doi := "10.48550/arXiv.1412.6980"
	metadata := parse.Citation{DOI: doi, Title: "Adam: A Method for Stochastic Optimization", Authors: []string{"Diederik P. Kingma", "Jimmy Ba"}, Year: "2014"}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>
		<div class="js-aarecord-list-fallback-cover"><div data-content="Adam: A method for stochastic optimization"></div><div data-content="Kingma, Diederik P. (author); Ba, Jimmy Lei (author)"></div></div>
		<div class="js-md5-codes-container">
			<a href="/search?q=doi:10.0000/2b5806fea0e2c0ff892275cc38f9477c">doi</a>
			<a href="/search?q=year:2015">year</a>
		</div></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	record := parse.RecordIdentity(doc)
	if record.DOI != "" || len(record.Authors) != 2 || record.Authors[0] != "Diederik P. Kingma" {
		t.Fatalf("parsed record: %+v", record)
	}
	if !SameWork(doi, metadata, record) {
		t.Fatal("the published version of the preprint was rejected")
	}

	for name, change := range map[string]func(*parse.Identity){
		"a year earlier than the preprint": func(r *parse.Identity) { r.Year = "2013" },
		"two years later":                  func(r *parse.Identity) { r.Year = "2016" },
		"another family name":              func(r *parse.Identity) { r.Authors = []string{"Diederik P. Kingma", "Jimmy Lei Bo"} },
		"another first initial":            func(r *parse.Identity) { r.Authors = []string{"Diederik P. Kingma", "Lei Ba"} },
		"its own real DOI":                 func(r *parse.Identity) { r.DOI = "10.1109/example.2015.1" },
	} {
		changed := record
		change(&changed)
		if SameWork(doi, metadata, changed) {
			t.Errorf("%s was accepted", name)
		}
	}
	// The year after is only allowed for preprints.
	journal := record
	if SameWork("10.1000/journal", parse.Citation{Title: metadata.Title, Authors: metadata.Authors, Year: "2014"}, journal) {
		t.Error("a journal DOI accepted a record from the following year")
	}
}

func TestSameWorkKeepsDOISuffixPunctuation(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"(02)", "(02).", ").", ".", ",", ";", ":", "]", "}", ">", "'", "\"", "?", "#"} {
		if SameWork("10.1000/example", parse.Citation{}, parse.Identity{DOI: "10.1000/example" + suffix}) {
			t.Errorf("distinct DOI suffix %q matched the unpunctuated DOI", suffix)
		}
	}
}

func TestSameWorkWithTheRecordsOwnIdentifiers(t *testing.T) {
	t.Parallel()

	const doi = "10.48550/arXiv.1706.03762"
	citation := parse.Citation{Title: "Attention Is All You Need", Authors: []string{"Ashish Vaswani"}, Year: "2017"}
	own := parse.Identity{ArxivID: "1706.03762", Title: "Attention is all you need", Authors: []string{"A. Vaswani"}, Year: "2017"}
	if !SameWork(doi, citation, own) {
		t.Fatal("a record with the arXiv id was rejected")
	}
	for name, change := range map[string]func(*parse.Identity){
		"another arXiv id": func(r *parse.Identity) { r.ArxivID = "1706.99999" },
		"another title":    func(r *parse.Identity) { r.Title = "Attention Is Not All You Need" },
		"other authors":    func(r *parse.Identity) { r.Authors = []string{"Someone Else"} },
		"another year":     func(r *parse.Identity) { r.Year = "2020" },
	} {
		record := own
		change(&record)
		if SameWork(doi, citation, record) {
			t.Errorf("%s was accepted", name)
		}
	}
	// Without DOI.org's data, the record's own identifier decides.
	if !SameWork(doi, parse.Citation{}, parse.Identity{ArxivID: "1706.03762"}) {
		t.Error("the arXiv id alone was rejected")
	}
	// Without an identifier, the record must match on title and authors.
	if SameWork(doi, parse.Citation{Title: "Attention Is All You Need"}, parse.Identity{Title: "Attention Is All You Need"}) {
		t.Error("a title alone was accepted")
	}
}

func TestSameYear(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		doi, want, got string
		same           bool
	}{
		{"10.1000/x", "2017", "2017", true},
		{"10.1000/x", "2017", "2018", false},
		{"10.48550/arXiv.1706.03762", "2017", "2018", true},
		{"10.48550/arXiv.1706.03762", "2017", "2019", false},
		{"10.48550/arXiv.1706.03762", "c. 2017", "2018", false},
	} {
		if got := sameYear(tc.doi, tc.want, tc.got); got != tc.same {
			t.Errorf("sameYear(%q, %q, %q) = %v", tc.doi, tc.want, tc.got, got)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	t.Parallel()

	if got := NormalizeTitle("  Adam:  A Method—for Stochastic Optimization! "); got != "adam a method for stochastic optimization" {
		t.Fatalf("NormalizeTitle = %q", got)
	}
}
