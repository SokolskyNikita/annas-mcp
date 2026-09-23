package anna

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestIsDOIQueryRecognizesDOIForms(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"10.1000/example":                          true,
		" DOI:10.1000/example ":                    true,
		"https://doi.org/10.1000/example":          true,
		"https://dx.doi.org/10.1000/example":       true,
		"attention is all you need":                false,
		"doi.org/10.1000/example without protocol": false,
	}
	for query, want := range tests {
		if got := IsDOIQuery(query); got != want {
			t.Errorf("IsDOIQuery(%q) = %v, want %v", query, got, want)
		}
	}
}

func TestModelStringMethodsIncludeCoreFields(t *testing.T) {
	t.Parallel()

	book := (&Book{Title: "Example", Authors: "Author", Publisher: "Press", URL: "https://annas.test/md5/hash", Hash: "hash"}).String()
	for _, part := range []string{"Title: Example", "Authors: Author", "Publisher: Press", "URL: https://annas.test/md5/hash"} {
		if !strings.Contains(book, part) {
			t.Errorf("book string %q does not contain %q", book, part)
		}
	}
	paper := (&Paper{DOI: "10.1000/example", Title: "Example", Authors: "Author", Journal: "Journal", PageURL: "https://annas.test/scidb"}).String()
	for _, part := range []string{"DOI: 10.1000/example", "Title: Example", "Journal: Journal", "Page: https://annas.test/scidb"} {
		if !strings.Contains(paper, part) {
			t.Errorf("paper string %q does not contain %q", paper, part)
		}
	}
}

func TestTitleFromDOIReadsCitationJSON(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "doi.org" || req.Header.Get("Accept") == "" {
				return nil, errors.New("unexpected DOI metadata request: " + req.URL.String())
			}
			return fixtureResponse(t, req, http.StatusOK, "doi_metadata.json", "application/json", nil), nil
		})},
	})

	title, err := client.titleFromDOI(context.Background(), "10.1000/example")
	if err != nil {
		t.Fatal(err)
	}
	if title != "Attention Is All You Need" {
		t.Fatalf("unexpected DOI title %q", title)
	}
}

func TestDownloadInputNormalizersRejectUnsafeValues(t *testing.T) {
	t.Parallel()

	if _, err := normalizeHash("not-an-md5"); apperr.CodeOf(err) != apperr.InvalidArgument {
		t.Fatalf("normalizeHash error = %v", err)
	}
	if got, err := normalizeFormat(".PDF"); err != nil || got != "pdf" {
		t.Fatalf("normalizeFormat(.PDF) = %q, %v", got, err)
	}
	if _, err := normalizeFormat("../pdf"); apperr.CodeOf(err) != apperr.InvalidArgument {
		t.Fatalf("normalizeFormat traversal error = %v", err)
	}
}

func TestFormatFromResponseUsesFilenameAndMediaType(t *testing.T) {
	t.Parallel()

	withFilename := &http.Response{Header: http.Header{"Content-Disposition": {`attachment; filename="paper.epub"`}, "Content-Type": {"application/octet-stream"}}}
	if got := formatFromResponse(withFilename); got != "epub" {
		t.Fatalf("filename format = %q", got)
	}
	withMediaType := &http.Response{Header: http.Header{"Content-Type": {"application/pdf; charset=binary"}}}
	if got := formatFromResponse(withMediaType); got != "pdf" {
		t.Fatalf("media type format = %q", got)
	}
}

func TestParserHelpersNormalizeMetadataAndSnippets(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("long description ", 40)
	if got := snippet(text); len(got) > 480 || strings.Contains(got, "  ") {
		t.Fatalf("snippet was not normalized: length=%d value=%q", len(got), got)
	}
	if language, format, size := extractMetaInformation("✅ English [en] · EPUB · 1.2MB · 2020"); language != "English" || format != "EPUB" || size != "1.2MB" {
		t.Fatalf("metadata = %q, %q, %q", language, format, size)
	}
	if language, format, size := extractMetaInformation("unstructured metadata"); language != "" || format != "" || size != "" {
		t.Fatalf("malformed metadata = %q, %q, %q", language, format, size)
	}
	if !looksLikeFilename("book.pdf") || looksLikeFilename("A title") {
		t.Fatal("filename heuristic returned an unexpected result")
	}
	candidates := bookDOICandidates(&Book{DOI: "10.1000/example", Description: "See 10.1000/example and 10.1000/other"})
	if len(candidates) != 2 {
		t.Fatalf("DOI candidates were not deduplicated: %v", candidates)
	}
}

func TestParseBooksSkipsMalformedCards(t *testing.T) {
	t.Parallel()

	if books := parseBooks(fixtureDocument(t, "malformed_cards.html"), "https://annas.test/search"); len(books) != 0 {
		t.Fatalf("malformed cards produced books: %+v", books)
	}
}

func TestCookieAndHTMLResponseHelpers(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		"aa_account_id2=abc=":         "aa_account_id2=abc=",
		"abc":                         "aa_account_id2=abc",
		"other=x; aa_account_id2=abc": "aa_account_id2=abc",
		"other=x; other2=y":           "",
	} {
		if got := normalizeCookieHeader(raw); got != want {
			t.Errorf("normalizeCookieHeader(%q) = %q, want %q", raw, got, want)
		}
	}
	resp := &http.Response{
		Header: http.Header{"Content-Type": {"application/pdf"}},
		Body:   io.NopCloser(bytes.NewReader(readFixture(t, "paper.pdf"))),
	}
	isHTML, err := isHTMLResponse(resp)
	if err != nil || isHTML {
		t.Fatalf("PDF response classified as HTML: %v, %v", isHTML, err)
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadArticleRequiresExactlyOneIdentifier(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{DownloadPath: t.TempDir()})
	for name, options := range map[string]ArticleDownloadOptions{
		"missing identifier": {},
		"both identifiers":   {DOI: "10.1000/example", Hash: "0123456789abcdef0123456789abcdef"},
	} {
		if _, err := client.DownloadArticle(context.Background(), options, 0, nil); apperr.CodeOf(err) != apperr.InvalidArgument {
			t.Errorf("%s returned %v", name, err)
		}
	}
}

func TestDownloadFileRejectsNonSuccessResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fixtureResponse(t, req, http.StatusForbidden, "blocked.txt", "text/plain", nil), nil
	})}
	get := func(_ context.Context, _ *http.Client, rawURL string) (*http.Response, error) {
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		return client.Transport.RoundTrip(req)
	}
	if _, err := downloadFileWithGetter(context.Background(), client, "https://downloads.example/file", t.TempDir(), "paper", "pdf", "", nil, get); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected a non-success download error, got %v", err)
	}
}

func TestPublicMethodsRequireAccountCookieBeforeNetwork(t *testing.T) {
	t.Parallel()

	newClient := func() (*Client, *int) {
		calls := 0
		client := NewClient(Config{
			BaseURL:      "annas.test",
			SecretKey:    "test-secret",
			DownloadPath: t.TempDir(),
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("network should not be reached")
			})},
		})
		return client, &calls
	}
	tests := map[string]func(*Client) error{
		"FindBook": func(client *Client) error {
			_, err := client.FindBook(context.Background(), "example", SearchOptions{}, 0)
			return err
		},
		"FindArticle": func(client *Client) error {
			_, err := client.FindArticle(context.Background(), "example", SearchOptions{}, 0)
			return err
		},
		"LookupDOI": func(client *Client) error {
			_, err := client.LookupDOI(context.Background(), "10.1000/example", 0)
			return err
		},
		"DownloadBook": func(client *Client) error {
			_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, 0, nil)
			return err
		},
		"DownloadArticle": func(client *Client) error {
			_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, 0, nil)
			return err
		},
	}
	for name, call := range tests {
		client, calls := newClient()
		if err := call(client); apperr.CodeOf(err) != apperr.Config {
			t.Errorf("%s returned %v, want config error", name, err)
		}
		if *calls != 0 {
			t.Errorf("%s contacted the network %d times", name, *calls)
		}
	}
}

func TestDownloadMethodsRequireAPIKeyBeforeNetwork(t *testing.T) {
	t.Parallel()

	for name, call := range map[string]func(*Client) error{
		"DownloadBook": func(client *Client) error {
			_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, 0, nil)
			return err
		},
		"DownloadArticle": func(client *Client) error {
			_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, 0, nil)
			return err
		},
	} {
		calls := 0
		client := NewClient(Config{
			BaseURL:       "annas.test",
			AccountCookie: "aa_account_id2=test",
			DownloadPath:  t.TempDir(),
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("network should not be reached")
			})},
		})
		if err := call(client); apperr.CodeOf(err) != apperr.Config {
			t.Errorf("%s returned %v, want config error", name, err)
		}
		if calls != 0 {
			t.Errorf("%s contacted the network %d times", name, calls)
		}
	}
}
