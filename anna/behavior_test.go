package anna

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTitleFromDOIReadsCitationJSON(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "doi.org" || req.Header.Get("Accept") == "" {
				return nil, errors.New("unexpected DOI metadata request: " + req.URL.String())
			}
			return fixtureResponse(t, req, http.StatusOK, attentionCSL, "application/json", nil), nil
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

	if _, err := normalizeHash("not-an-md5"); CodeOf(err) != InvalidArgument {
		t.Fatalf("normalizeHash error = %v", err)
	}
	if got, err := normalizeFormat(".PDF"); err != nil || got != "pdf" {
		t.Fatalf("normalizeFormat(.PDF) = %q, %v", got, err)
	}
	if _, err := normalizeFormat("../pdf"); CodeOf(err) != InvalidArgument {
		t.Fatalf("normalizeFormat traversal error = %v", err)
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

	var cookies struct {
		Comment string
		Cases   []struct{ Input, Header string }
	}
	loadCases(t, "cookie_headers", &cookies)
	for _, tc := range cookies.Cases {
		if got := normalizeCookieHeader(tc.Input); got != tc.Header {
			t.Errorf("normalizeCookieHeader(%q) = %q, want %q", tc.Input, got, tc.Header)
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

	client := mustClient(Config{DownloadPath: t.TempDir()})
	for name, options := range map[string]ArticleDownloadOptions{
		"missing identifier": {},
		"both identifiers":   {DOI: "10.1000/example", Hash: "0123456789abcdef0123456789abcdef"},
	} {
		if _, err := client.DownloadArticle(context.Background(), options, 0, nil); CodeOf(err) != InvalidArgument {
			t.Errorf("%s returned %v", name, err)
		}
	}
}

func TestDownloadFileRejectsNonSuccessResponse(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fixtureResponse(t, req, http.StatusForbidden, "blocked.txt", "text/plain", nil), nil
	})}
	get := func(_ context.Context, _ *http.Client, rawURL string, _ http.Header) (*http.Response, error) {
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

func TestPublicMethodsRequireCredentialsBeforeNetwork(t *testing.T) {
	t.Parallel()

	newClient := func() (*Client, *int) {
		calls := 0
		client := mustClient(Config{
			BaseURL:      "annas.test",
			DownloadPath: t.TempDir(),
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("network should not be reached")
			})},
		})
		return client, &calls
	}
	// literal: one call per public method; the arguments do not matter.
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
		if err := call(client); CodeOf(err) != CodeConfig {
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
			_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{Hash: "0123456789abcdef0123456789abcdef"}, 0, nil)
			return err
		},
	} {
		calls := 0
		client := mustClient(Config{
			BaseURL:       "annas.test",
			AccountCookie: "aa_account_id2=test",
			DownloadPath:  t.TempDir(),
			HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("network should not be reached")
			})},
		})
		if err := call(client); CodeOf(err) != CodeConfig {
			t.Errorf("%s returned %v, want config error", name, err)
		}
		if calls != 0 {
			t.Errorf("%s contacted the network %d times", name, calls)
		}
	}
}
