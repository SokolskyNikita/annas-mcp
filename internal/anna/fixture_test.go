package anna

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// testdata contains representative upstream responses. Keeping the payloads
// in files makes them easier to inspect and lets tests exercise the parser
// with realistic documents without hiding fixtures in Go source. Pages
// captured from the live archive live with the parsers, in parse/testdata.
//
//go:embed testdata/* parse/testdata/*
var testdataFS embed.FS

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// unzipped caches captured pages once unzipped: tests with short request
// deadlines serve them from fake mirrors, which must answer at once.
var unzipped sync.Map

// readFixture returns a fixture's contents, unzipping .gz files. name is
// relative to testdata or, for captured pages, parse/testdata. Callers must
// not modify the result.
func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	if data, ok := unzipped.Load(name); ok {
		return data.([]byte)
	}
	data, err := testdataFS.ReadFile("testdata/" + name)
	if errors.Is(err, fs.ErrNotExist) {
		data, err = testdataFS.ReadFile("parse/testdata/" + name)
	}
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
		unzipped.Store(name, data)
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

// file is the captured page's name under parse/testdata/pages.
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

// Captured pages and records the client tests serve.
const (
	searchAttentionPage = "pages/search_attention.html.gz"
	searchJournalsPage  = "pages/search_journals.html.gz"
	noResultsPage       = "pages/search_empty.html.gz"
	recordAttentionPage = "pages/record_attention.html.gz"
	recordLiangPage     = "pages/record_liang_2016.html.gz"
	sciDBPage           = "pages/scidb_equity_premium.html.gz"
	sciDBRecordPage     = "pages/record_equity_premium.html.gz"
	attentionCSL        = "pages/csl_attention.json"

	attentionDOI   = "10.48550/arXiv.1706.03762"
	attentionTitle = "Attention Is All You Need"
	// attentionHash is the record DOI lookup accepts for attentionDOI. Its
	// identifiers panel carries no DOI, so it matches on title and authors.
	attentionHash    = "18e1b007a1dab45b30cc861ba2dfda25"
	attentionAuthors = "Ashish Vaswani, Noam Shazeer, Niki Parmar, Jakob Uszkoreit, Llion Jones, Aidan N. Gomez, Lukasz Kaiser, Illia Polosukhin"
	// liangHash's record carries liangDOI in its identifiers panel, so it
	// matches that DOI without DOI.org.
	liangDOI  = "10.1002/chin.201627137"
	liangHash = "0029d5735c9b47b2f62be9f8f3d16cf5"
	// The captured SciDB page embeds sciDBFileURL, the file sciDBHash.
	sciDBHash    = "eff1d22d32721d0fcc05d6c7e332bf54"
	sciDBFileURL = "https://files.invalid/scidb/paper.pdf"
)

var capturedHTML sync.Map

// capturedCard returns the HTML of the search card for hash on a captured
// search page.
func capturedCard(t testing.TB, page, hash string) string {
	t.Helper()
	key := page + "#" + hash
	if card, ok := capturedHTML.Load(key); ok {
		return card.(string)
	}
	link := fixtureDocument(t, page).Find("a.custom-a.block[href='/md5/" + hash + "']")
	if link.Length() == 0 {
		t.Fatalf("%s has no card for %s", page, hash)
	}
	card, err := goquery.OuterHtml(link.Parent())
	if err != nil {
		t.Fatal(err)
	}
	capturedHTML.Store(key, card)
	return card
}

// searchPageWith is the captured title search with its results replaced by
// cards and its partial matches removed.
func searchPageWith(t testing.TB, cards ...string) string {
	t.Helper()
	key := "with#" + strings.Join(cards, "\x00")
	if page, ok := capturedHTML.Load(key); ok {
		return page.(string)
	}
	doc := fixtureDocument(t, searchAttentionPage)
	doc.Find(".js-aarecord-list-outer").First().SetHtml(strings.Join(cards, "\n"))
	doc.Find(".js-partial-matches-show, .js-partial-matches-remove").Remove()
	page, err := doc.Html()
	if err != nil {
		t.Fatal(err)
	}
	capturedHTML.Store(key, page)
	return page
}

// htmlResponse answers req with an HTML body.
func htmlResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {"text/html"}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}

// withCode adds an identifier to a captured record page's codes panel, as
// Anna links it: an exact search for key:value.
func withCode(key, value string) map[string]string {
	return map[string]string{
		`<div class="js-md5-codes-container hidden">`: `<div class="js-md5-codes-container hidden"><a href='/search?q="` + key + ":" + url.QueryEscape(value) + `"'>` + key + `</a>`,
	}
}
