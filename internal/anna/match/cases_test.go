package match

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
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

func pageDocument(t testing.TB, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(readPage(t, name)))
	if err != nil {
		t.Fatal(err)
	}
	return doc
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
			csl := capturedCSL(t, tc.csl)
			metadata, err := parse.ParseCitation(bytes.NewReader(readPage(t, csl.Name+".json")))
			if err != nil {
				t.Fatal(err)
			}
			record := parse.RecordIdentity(pageDocument(t, tc.record+".html.gz"))
			if got := SameWork(parse.NormalizeDOI(csl.DOI), metadata, record); got != tc.want {
				t.Fatalf("verifyDOIIdentity = %v, want %v\nCSL: %+v\nrecord: %+v", got, tc.want, metadata, record)
			}
		})
	}
}

// TestDOIIdentityRejectsAnotherDOI checks that a record carrying another real
// DOI is rejected even when its title and authors agree with DOI.org's.
func TestDOIIdentityRejectsAnotherDOI(t *testing.T) {
	record := parse.RecordIdentity(pageDocument(t, "record_liang_2016.html.gz"))
	if record.DOI != "10.1002/chin.201627137" {
		t.Fatalf("record DOI = %q, want the one in its identifiers panel", record.DOI)
	}
	metadata := parse.Citation{Title: record.Title, Authors: record.Authors, Year: record.Year}
	if SameWork("10.1000/another", metadata, record) {
		t.Fatal("a record with another DOI matched on title and authors alone")
	}
	if !SameWork("10.1002/chin.201627137", metadata, record) {
		t.Fatal("the record did not match its own DOI")
	}
}
