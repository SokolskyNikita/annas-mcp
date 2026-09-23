package anna

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestBuildSearchURLIncludesFiltersAndPage(t *testing.T) {
	t.Parallel()

	got := buildSearchURL("annas-archive.gl", "machine learning", SearchOptions{
		Content:  "book_nonfiction",
		Language: "en",
		Page:     2,
	})
	for _, part := range []string{
		"https://annas-archive.gl/search?",
		"q=machine+learning",
		"content=book_nonfiction",
		"lang=en",
		"page=2",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("search URL %q does not contain %q", got, part)
		}
	}
}

func TestResolveSearchDropsLegacyFilters(t *testing.T) {
	t.Parallel()

	books := ResolveSearch(SearchOptions{Content: "book_any", Page: 2}, "book_any")
	if books.Content != "" || books.Index != "" || books.Page != 2 {
		t.Fatalf("book_any should not be sent: %+v", books)
	}
	articles := ResolveSearch(SearchOptions{}, "journal")
	if articles.Index != "journals" || articles.Content != "" {
		t.Fatalf("articles should use the journals index: %+v", articles)
	}
	got := buildSearchURL("annas-archive.gl", "transformers", articles)
	if strings.Contains(got, "content=") || !strings.Contains(got, "index=journals") {
		t.Fatalf("article URL = %s", got)
	}
}

func TestParseBooksReadsSearchCard(t *testing.T) {
	t.Parallel()

	books := parseBooks(fixtureDocument(t, "search_card.html"), "https://annas-archive.gl/search?q=example")
	if len(books) != 1 {
		t.Fatalf("expected 1 book, got %d", len(books))
	}
	book := books[0]
	if book.Hash != "abc123def456" || book.Title != "Example Title" || book.Format != "EPUB" {
		t.Fatalf("unexpected book: %+v", book)
	}
	if book.Authors != "Ada Lovelace" || book.Publisher != "Example Press" || book.Language != "English" {
		t.Fatalf("unexpected metadata: %+v", book)
	}
	if book.URL != "https://annas-archive.gl/md5/abc123def456" {
		t.Fatalf("unexpected URL %q", book.URL)
	}
	if book.Description != "A short description. DOI 10.1000/example.123." || book.DOI != "" {
		t.Fatalf("unexpected description: %+v", book)
	}
}

func TestCopyWithHashRejectsMismatchedChecksum(t *testing.T) {
	t.Parallel()

	paper := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	_, err := copyWithHash(context.Background(), io.Discard, bytes.NewReader(paper), -1, hash, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = copyWithHash(context.Background(), io.Discard, bytes.NewReader(readFixture(t, "corrupt.pdf")), -1, hash, nil)
	if err == nil {
		t.Fatal("expected a checksum mismatch")
	}
}

func TestCopyWithHashRejectsEmptyAndMismatchedLength(t *testing.T) {
	t.Parallel()

	if _, err := copyWithHash(context.Background(), io.Discard, bytes.NewReader(readFixture(t, "empty.txt")), 0, "", nil); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected an empty-file error, got %v", err)
	}
	if _, err := copyWithHash(context.Background(), io.Discard, bytes.NewReader(readFixture(t, "short.txt")), 4, "", nil); err == nil || !strings.Contains(err.Error(), "size mismatch") {
		t.Fatalf("expected a content-length error, got %v", err)
	}
}

func TestSanitizeFilenameIsSafeAndByteBounded(t *testing.T) {
	t.Parallel()

	if got := sanitizeFilename("CON.txt"); got != "_CON.txt" {
		t.Fatalf("reserved Windows name was not escaped: %q", got)
	}
	if got := sanitizeFilename("report.pdf.  "); got != "report.pdf" {
		t.Fatalf("trailing Windows punctuation was not removed: %q", got)
	}
	got := sanitizeFilename(strings.Repeat("界", 100) + ".pdf")
	if len(got) > 180 || !utf8.ValidString(got) {
		t.Fatalf("filename is not a valid <=180-byte UTF-8 name: bytes=%d valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestNormalizeDOIPreservesSuffixPunctuation(t *testing.T) {
	t.Parallel()

	if got := NormalizeDOI("https://doi.org/10.1000%2Fexample?utm_source=test#fragment"); got != "10.1000/example" {
		t.Fatalf("DOI URL was not decoded/cleaned: %q", got)
	}
	for _, suffix := range []string{"(02)", "(02).", ").", ".", ",", ";", ":", "]", "}", ">", "'", "\"", "?", "#"} {
		want := "10.1000/example" + suffix
		for _, input := range []string{want + " ", "doi: " + want, "https://doi.org/" + url.PathEscape(want)} {
			got, valid := ParseDOI(input)
			if got != want || !valid {
				t.Errorf("DOI suffix changed: input=%q got=%q valid=%v want=%q", input, got, valid, want)
			}
		}
		if verifyDOIIdentity("10.1000/example", doiMetadata{}, archiveIdentity{DOI: want}) {
			t.Errorf("distinct DOI suffix %q matched the unpunctuated DOI", suffix)
		}
	}
	for _, input := range []string{"<https://doi.org/10.1000/example(02)>", "\"10.1000/example(02)\"", "doi: <10.1000/example(02)>", "(10.1000/example(02))", "doi: https://doi.org/10.1000/example(02)"} {
		got, valid := ParseDOI(input)
		if got != "10.1000/example(02)" || !valid || !IsDOIQuery(input) {
			t.Errorf("enclosing DOI wrapper was not recognized: input=%q got=%q valid=%v", input, got, valid)
		}
	}
}

func TestResolvePaperDownloadURLPreservesQuery(t *testing.T) {
	t.Parallel()

	got, err := resolvePaperDownloadURL("https://annas.test", "/scidb?doi=10.1000%2Fexample")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://annas.test/scidb?doi=10.1000%2Fexample" {
		t.Fatalf("download query was rewritten: %q", got)
	}
}

func TestRedactErrHidesSecret(t *testing.T) {
	t.Parallel()

	secret := "secret key/part"
	cause := apperr.Wrap(apperr.Config, "request failed for "+secret+"?q="+url.QueryEscape(secret)+" path="+url.PathEscape(secret), context.Canceled)
	got := redactErr(cause, secret)
	if strings.Contains(got.Error(), secret) || strings.Contains(got.Error(), url.QueryEscape(secret)) || strings.Contains(got.Error(), url.PathEscape(secret)) || !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("secret leaked: %v", got)
	}
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("redaction dropped context cause: %v", got)
	}
	var coded *apperr.Error
	if !errors.As(got, &coded) || coded.Code != apperr.Config {
		t.Fatalf("redaction dropped typed cause: %v", got)
	}
}

func TestFetchDocumentReportsForbidden(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{BaseURL: "https://annas.test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fixtureResponse(t, nil, http.StatusForbidden, "blocked.txt", "text/plain", nil), nil
	})}})
	_, _, err := client.fetchDocument(context.Background(), "https://annas.test/search?q=blocked")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}

func TestDownloadFileRejectsHTMLChallenge(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fixtureResponse(t, req, http.StatusOK, "challenge.html", "text/html; charset=utf-8", nil), nil
	})}
	_, err := downloadFileWithGetter(context.Background(), client, "https://download.invalid/file", t.TempDir(), "paper", "", "", nil, func(_ context.Context, _ *http.Client, _ string) (*http.Response, error) {
		return client.Transport.RoundTrip(&http.Request{})
	})
	if err == nil || !strings.Contains(err.Error(), "HTML") {
		t.Fatalf("expected HTML rejection, got %v", err)
	}
}

func TestClientSearchUsesInjectedBaseAndTransport(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL:       "https://annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/search" || req.URL.Query().Get("q") != "example" {
				return nil, errors.New("unexpected request URL: " + req.URL.String())
			}
			return fixtureResponse(t, req, http.StatusOK, "search_card.html", "text/html", nil), nil
		})},
	})
	books, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
	if err != nil || len(books) != 1 || books[0].Hash != "abc123def456" {
		t.Fatalf("injected client search failed: books=%+v err=%v", books, err)
	}
}

func TestClientRedirectDropsCookieOnSchemeChange(t *testing.T) {
	t.Parallel()

	var requests []string
	client := NewClient(Config{
		BaseURL:       "https://annas.test",
		AccountCookie: "aa_account_id2=secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests = append(requests, req.URL.String()+" cookie="+req.Header.Get("Cookie"))
			if len(requests) == 1 {
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"http://annas.test/insecure"}}, Body: io.NopCloser(http.NoBody), Request: req}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(http.NoBody), Request: req}, nil
		})},
	})
	_, err := client.doGet(context.Background(), client.requestClient(time.Second), "https://annas.test/start")
	if err == nil || !strings.Contains(err.Error(), "unsafe upstream redirect") {
		t.Fatalf("expected HTTPS-only redirect rejection, got %v", err)
	}
	if len(requests) != 1 || !strings.Contains(requests[0], "cookie=aa_account_id2=secret") {
		t.Fatalf("unexpected redirect cookies: %v", requests)
	}
}
