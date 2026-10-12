package parse

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"io"
	"io/fs"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// testdata holds pages captured from the live archive and DOI.org; see
// TestCapturePages in package anna.
//
//go:embed testdata
var testdataFS embed.FS

// unzippedPages holds the captured pages once read and unzipped.
var unzippedPages sync.Map

// readPage returns a captured page, unzipped once per test run. Callers must
// not change it.
func readPage(t testing.TB, name string) []byte {
	t.Helper()
	if data, ok := unzippedPages.Load(name); ok {
		return data.([]byte)
	}
	data, err := readTestdata("pages/" + name)
	if err != nil {
		t.Fatalf("read captured page %q: %v", name, err)
	}
	stored, _ := unzippedPages.LoadOrStore(name, data)
	return stored.([]byte)
}

// readTestdata returns a file under testdata, unzipped when it ends in .gz.
func readTestdata(path string) ([]byte, error) {
	data, err := testdataFS.ReadFile("testdata/" + path)
	if err == nil && strings.HasSuffix(path, ".gz") {
		var reader *gzip.Reader
		if reader, err = gzip.NewReader(bytes.NewReader(data)); err == nil {
			data, err = io.ReadAll(reader)
		}
	}
	return data, err
}

// corpusFileLines returns the non-empty lines of a corpus file, or nil when
// there is no such file.
func corpusFileLines(name string) ([]string, error) {
	data, err := readTestdata("corpus/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		if line := scanner.Text(); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}

// parsedPages holds the captured pages once parsed. Parsing them is most of
// the tests' time under the race detector, so each is parsed once per run.
var parsedPages sync.Map

// pageDocument returns a captured page, parsed once and shared between
// tests, which must not change it; a test that does uses freshPageDocument.
func pageDocument(t testing.TB, name string) *goquery.Document {
	t.Helper()
	if doc, ok := parsedPages.Load(name); ok {
		return doc.(*goquery.Document)
	}
	doc, _ := parsedPages.LoadOrStore(name, freshPageDocument(t, name))
	return doc.(*goquery.Document)
}

// freshPageDocument parses a captured page anew, for a test that changes it.
func freshPageDocument(t testing.TB, name string) *goquery.Document {
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
