package parse

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// testdata holds pages captured from the live archive and DOI.org; see
// TestCapturePages in package anna.
//
//go:embed testdata
var testdataFS embed.FS

// readPage returns a captured page, unzipped.
func readPage(t testing.TB, name string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/pages/" + name)
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

func htmlDocument(t testing.TB, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}
