package anna

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// testdata contains representative upstream responses. Keeping the payloads
// in files makes them easier to inspect and lets tests exercise the parser
// with realistic documents without hiding fixtures in Go source.
//
//go:embed testdata/*
var testdataFS embed.FS

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// readFixture returns a fixture's contents, unzipping .gz files.
func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	if strings.HasSuffix(name, ".gz") {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err == nil {
			data, err = io.ReadAll(reader)
		}
		if err != nil {
			t.Fatalf("unzip fixture %q: %v", name, err)
		}
	}
	return data
}

// capturedPage is one page in testdata/pages/manifest.json, captured from
// the live archive by TestCapturePages.
type capturedPage struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
	DOI  string `json:"doi,omitempty"`
	Page int    `json:"page,omitempty"`
	Note string `json:"note,omitempty"`
}

// file is the captured page's name under testdata/pages.
func (p capturedPage) file() string {
	if p.Kind == "csl" {
		return p.Name + ".json"
	}
	return p.Name + ".html.gz"
}

func readManifest(t testing.TB) []capturedPage {
	t.Helper()
	var manifest struct {
		Pages []capturedPage `json:"pages"`
	}
	if err := json.Unmarshal(readFixture(t, "pages/manifest.json"), &manifest); err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return manifest.Pages
}

// capturedPageNamed returns the manifest entry called name.
func capturedPageNamed(t testing.TB, name string) capturedPage {
	t.Helper()
	for _, page := range readManifest(t) {
		if page.Name == name {
			return page
		}
	}
	t.Fatalf("no captured page %q", name)
	return capturedPage{}
}

func fixtureText(t testing.TB, name string) string {
	t.Helper()
	return string(readFixture(t, name))
}

func fixtureHash(t testing.TB, name string) string {
	t.Helper()
	sum := md5.Sum(readFixture(t, name))
	return hex.EncodeToString(sum[:])
}

func fixtureDocument(t testing.TB, name string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(readFixture(t, name)))
	if err != nil {
		t.Fatalf("parse fixture %q: %v", name, err)
	}
	return doc
}

func fixtureResponse(t testing.TB, req *http.Request, status int, name, contentType string, replacements map[string]string) *http.Response {
	t.Helper()
	body := readFixture(t, name)
	for old, newValue := range replacements {
		body = bytes.ReplaceAll(body, []byte(old), []byte(newValue))
	}
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {contentType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
