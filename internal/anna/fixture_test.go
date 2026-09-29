package anna

import (
	"bytes"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"io"
	"net/http"
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

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return data
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
