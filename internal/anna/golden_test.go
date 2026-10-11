package anna

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

var updateGolden = flag.Bool("update", false, "rewrite the expected outputs in testdata from the current parsers")

// goldenBase is the mirror captured pages are read as coming from.
const goldenBase = "https://annas-archive.gl"

// TestGoldenPages parses every captured page the way the client does and
// compares the result with the reviewed output in <name>.golden.json. After
// a capture or a parser change, run with -update and review the diff.
func TestGoldenPages(t *testing.T) {
	for _, page := range readManifest(t) {
		t.Run(page.Name, func(t *testing.T) {
			got, err := json.MarshalIndent(goldenOutput(t, page), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			name := filepath.Join("pages", page.Name+".golden.json")
			if *updateGolden {
				if err := os.WriteFile(filepath.Join("parse", "testdata", name), got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if want := readFixture(t, name); !bytes.Equal(got, want) {
				t.Errorf("%s changed; if the change is right, rerun with -update and review the diff\n%s", name, got)
			}
		})
	}
}

// goldenOutput is what the client makes of a captured page.
func goldenOutput(t *testing.T, page capturedPage) any {
	t.Helper()
	body := readFixture(t, "pages/"+page.file())
	if page.Kind == "csl" {
		metadata, err := parseCitationMetadata(bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		return metadata
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	pageURL := goldenBase + page.Path
	switch page.Kind {
	case "books", "articles", "metadata":
		return goldenSearch(doc, page, pageURL)
	case "record":
		paper := &Paper{}
		addRecordDetails(paper, doc)
		return struct {
			Identity parse.Identity `json:"identity"`
			// SciDB reads its paper's details from the same page.
			SciDBDetails *Paper `json:"scidb_details"`
		}{parse.RecordIdentity(doc), paper}
	case "scidb":
		downloadURL, hash, err := sciDBFile(doc, pageURL)
		return map[string]string{"download_url": downloadURL, "hash": hash, "error": errorText(err)}
	}
	t.Fatalf("unknown page kind %q", page.Kind)
	return nil
}

func goldenSearch(doc *goquery.Document, page capturedPage, pageURL string) any {
	results := searchResults{cards: parse.SearchCards(doc), doc: doc, pageURL: pageURL, page: max(page.Page, 1)}
	output := map[string]any{
		"facets": map[string][]string{"src": parse.FacetValues(doc, "src"), "content": parse.FacetValues(doc, "content")},
	}
	if len(results.cards) == 0 && parse.ShowsResults(doc, results.page) {
		output["error"] = "results this version cannot read"
		return output
	}
	switch page.Kind {
	case "books":
		output["page"] = filePage(results, func(book *Book) *Book { return book })
	case "articles":
		output["page"] = filePage(results, func(book *Book) *Paper { return paperFromBook(book, book.DOI) })
	case "metadata":
		output["page"] = metadataPage(results)
	}
	return output
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// TestDOIIdentityCases checks DOI verification on real records against
// DOI.org's citation data: the paper's own records match, and look-alikes
// and other versions under another DOI do not.
func TestDOIIdentityCases(t *testing.T) {
	cases := []struct {
		csl, record string
		want        bool
	}{
		{"csl_attention", "record_attention", true},
		{"csl_adam", "record_adam", true},
		// The archive holds ResNet's CVPR version, under the conference's DOI.
		{"csl_resnet", "record_resnet_cvpr", false},
		{"csl_attention", "record_equity_premium", false},
		{"csl_attention", "record_liang_2016", false},
		{"csl_attention", "record_book", false},
	}
	for _, tc := range cases {
		t.Run(tc.csl+"/"+tc.record, func(t *testing.T) {
			csl := capturedPageNamed(t, tc.csl)
			metadata, err := parseCitationMetadata(bytes.NewReader(readFixture(t, "pages/"+csl.file())))
			if err != nil {
				t.Fatal(err)
			}
			record := parse.RecordIdentity(fixtureDocument(t, "pages/"+capturedPageNamed(t, tc.record).file()))
			if got := verifyDOIIdentity(parse.NormalizeDOI(csl.DOI), metadata, record); got != tc.want {
				t.Fatalf("verifyDOIIdentity = %v, want %v\nCSL: %+v\nrecord: %+v", got, tc.want, metadata, record)
			}
		})
	}
}

// TestDOIIdentityRejectsAnotherDOI checks that a record carrying another real
// DOI is rejected even when its title and authors agree with DOI.org's.
func TestDOIIdentityRejectsAnotherDOI(t *testing.T) {
	record := parse.RecordIdentity(fixtureDocument(t, "pages/record_liang_2016.html.gz"))
	if record.DOI != "10.1002/chin.201627137" {
		t.Fatalf("record DOI = %q, want the one in its identifiers panel", record.DOI)
	}
	metadata := doiMetadata{Title: record.Title, Authors: record.Authors, Year: record.Year}
	if verifyDOIIdentity("10.1000/another", metadata, record) {
		t.Fatal("a record with another DOI matched on title and authors alone")
	}
	if !verifyDOIIdentity("10.1002/chin.201627137", metadata, record) {
		t.Fatal("the record did not match its own DOI")
	}
}
