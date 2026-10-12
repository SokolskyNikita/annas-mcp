package match

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// Captured pages are read from package parse's testdata.
const capturedPages = "../parse/testdata/pages/"

func readPage(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(capturedPages + name)
	if err == nil && strings.HasSuffix(name, ".gz") {
		var reader *gzip.Reader
		if reader, err = gzip.NewReader(bytes.NewReader(data)); err == nil {
			data, err = io.ReadAll(reader)
		}
	}
	if err != nil {
		t.Fatalf("read captured page %q: %v", name, err)
	}
	return data
}

// parsedPages holds the captured pages once parsed, so tests that read the
// same record share one parse. The documents must not be changed.
var parsedPages sync.Map

func pageDocument(t testing.TB, name string) *goquery.Document {
	t.Helper()
	if doc, ok := parsedPages.Load(name); ok {
		return doc.(*goquery.Document)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(readPage(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := parsedPages.LoadOrStore(name, doc)
	return stored.(*goquery.Document)
}

// capturedCSLPage is a page in the capture manifest; DOI is set for
// DOI.org's answers.
type capturedCSLPage struct{ Name, DOI string }

// capturedCSL returns the manifest entry of DOI.org's answer called name.
func capturedCSL(t testing.TB, name string) capturedCSLPage {
	t.Helper()
	var manifest struct {
		Pages []capturedCSLPage `json:"pages"`
	}
	if err := json.Unmarshal(readPage(t, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	for _, page := range manifest.Pages {
		if page.Name == name {
			return page
		}
	}
	t.Fatalf("no captured page %q", name)
	return capturedCSLPage{}
}

// capturedCases are testdata/cases/captured_records.json.
type capturedCases struct {
	Comment string `json:"comment"`
	Cases   []struct {
		CSL    string `json:"csl"`
		Record string `json:"record"`
		Same   bool   `json:"same"`
		Why    string `json:"why"`
	} `json:"cases"`
	AnotherDOI struct {
		Record   string `json:"record"`
		OwnDOI   string `json:"own_doi"`
		OtherDOI string `json:"other_doi"`
	} `json:"another_doi"`
}

// TestDOIIdentityCases checks DOI verification on real records against
// DOI.org's citation data: the paper's own records match, and look-alikes
// and other versions under another DOI do not.
func TestDOIIdentityCases(t *testing.T) {
	var cases capturedCases
	loadCases(t, "captured_records", &cases)
	for _, tc := range cases.Cases {
		t.Run(tc.CSL+"/"+tc.Record, func(t *testing.T) {
			csl := capturedCSL(t, tc.CSL)
			metadata, err := parse.ParseCitation(bytes.NewReader(readPage(t, csl.Name+".json")))
			if err != nil {
				t.Fatal(err)
			}
			record := parse.RecordIdentity(pageDocument(t, tc.Record+".html.gz"))
			if got := SameWork(parse.NormalizeDOI(csl.DOI), metadata, record); got != tc.Same {
				t.Fatalf("SameWork = %v, want %v (%s)\nCSL: %+v\nrecord: %+v", got, tc.Same, tc.Why, metadata, record)
			}
		})
	}
}

// TestDOIIdentityRejectsAnotherDOI checks that a record carrying another real
// DOI is rejected even when its title and authors agree with DOI.org's.
func TestDOIIdentityRejectsAnotherDOI(t *testing.T) {
	var cases capturedCases
	loadCases(t, "captured_records", &cases)
	another := cases.AnotherDOI
	record := parse.RecordIdentity(pageDocument(t, another.Record+".html.gz"))
	if record.DOI != another.OwnDOI {
		t.Fatalf("record DOI = %q, want %q from its identifiers panel", record.DOI, another.OwnDOI)
	}
	metadata := parse.Citation{Title: record.Title, Authors: record.Authors, Year: record.Year}
	if SameWork(another.OtherDOI, metadata, record) {
		t.Fatal("a record with another DOI matched on title and authors alone")
	}
	if !SameWork(another.OwnDOI, metadata, record) {
		t.Fatal("the record did not match its own DOI")
	}
}
