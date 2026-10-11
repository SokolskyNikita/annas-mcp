package anna

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

const (
	outageDOI   = "10.48550/arXiv.1706.03762"
	outageTitle = "Attention Is All You Need"
	otherHash   = "abcdef0123456789abcdef0123456789"
)

// timeoutError is a transport timeout, as net/http reports one.
type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// doiArchiveClient answers DOI.org with valid CSL metadata, unless doiStatus
// says otherwise, and sends every archive request to archive.
func doiArchiveClient(t *testing.T, doiStatus int, archive func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	return NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "doi.org" {
			if doiStatus != http.StatusOK {
				return fixtureResponse(t, req, doiStatus, "blocked.txt", "text/plain", nil), nil
			}
			return fixtureResponse(t, req, http.StatusOK, "doi_metadata.json", "application/json", nil), nil
		}
		return archive(req)
	})}})
}

// failingEditions finds two editions of the paper, but every other archive
// request, record pages included, answers 502. Several editions share the
// title, so no single record error used to be reported.
func failingEditions(t *testing.T) func(*http.Request) (*http.Response, error) {
	return func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/search" {
			replacements := map[string]string{"0123456789abcdef0123456789abcdef": otherHash}
			if req.URL.Query().Get("q") == outageTitle {
				replacements = nil
			}
			return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", replacements), nil
		}
		return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
	}
}

func TestLookupDOIReportsArchiveOutage(t *testing.T) {
	t.Parallel()
	status := func(code int) func(*http.Request) (*http.Response, error) {
		return func(req *http.Request) (*http.Response, error) {
			return fixtureResponse(t, req, code, "blocked.txt", "text/plain", nil), nil
		}
	}
	for _, test := range []struct {
		name      string
		doiStatus int
		archive   func(*http.Request) (*http.Response, error)
		want      string
	}{
		{"every archive request answers 502", http.StatusOK, status(http.StatusBadGateway), apperr.Upstream},
		{"DOI.org also fails", http.StatusServiceUnavailable, status(http.StatusBadGateway), apperr.Upstream},
		{"every archive request is blocked", http.StatusOK, status(http.StatusForbidden), apperr.UpstreamBlocked},
		{"every archive request times out", http.StatusOK, func(*http.Request) (*http.Response, error) {
			return nil, timeoutError{}
		}, apperr.RequestTimeout},
		{"SciDB answers 502 and searches find nothing", http.StatusOK, func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/search" {
				return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
			}
			return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
		}, apperr.Upstream},
		{"search finds editions whose records answer 502", http.StatusOK, failingEditions(t), apperr.Upstream},
		{"a search answers 502 and the other finds a different paper", http.StatusOK, func(req *http.Request) (*http.Response, error) {
			switch {
			case req.URL.Path == "/scidb/"+outageDOI:
				return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
			case req.URL.Path == "/search" && req.URL.Query().Get("index") == "journals":
				return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
			case req.URL.Path == "/search":
				return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
			}
			return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", map[string]string{"Ashish Vaswani": "Unrelated Author"}), nil
		}, apperr.Upstream},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := doiArchiveClient(t, test.doiStatus, test.archive)
			paper, err := client.LookupDOI(context.Background(), outageDOI, time.Second)
			if paper != nil || err == nil {
				t.Fatalf("lookup succeeded during an outage: paper=%+v err=%v", paper, err)
			}
			if code := apperr.CodeOf(err); code != test.want {
				t.Fatalf("error code = %s, want %s: %v", code, test.want, err)
			}
			if !strings.Contains(err.Error(), "could not check Anna's Archive for DOI "+outageDOI) {
				t.Fatalf("error does not say the archive could not be checked: %v", err)
			}
			if test.doiStatus != http.StatusOK && !strings.Contains(err.Error(), "DOI metadata: doi.org returned status 503") {
				t.Fatalf("error dropped the DOI.org failure: %v", err)
			}
		})
	}
}

func TestLookupDOIKeepsNotFoundForGenuineMiss(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		detail int
	}{
		{"searches find nothing", 0},
		{"the only edition's record is gone", http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := doiArchiveClient(t, http.StatusOK, func(req *http.Request) (*http.Response, error) {
				switch {
				case req.URL.Path == "/scidb/"+outageDOI:
					return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
				case req.URL.Path == "/search" && test.detail != 0 && req.URL.Query().Get("q") == outageTitle:
					return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
				case req.URL.Path == "/search":
					return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
				case strings.HasPrefix(req.URL.Path, "/md5/") && test.detail != 0:
					return fixtureResponse(t, req, test.detail, "blocked.txt", "text/plain", nil), nil
				}
				t.Errorf("unexpected archive request: %s", req.URL)
				return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
			})
			paper, err := client.LookupDOI(context.Background(), outageDOI, time.Second)
			if paper != nil || apperr.CodeOf(err) != apperr.NotFound {
				t.Fatalf("genuine miss was not NOT_FOUND: paper=%+v err=%v", paper, err)
			}
		})
	}
}

func TestDownloadArticleByDOIReportsArchiveOutage(t *testing.T) {
	t.Parallel()
	archive := failingEditions(t)
	client := NewClient(Config{
		BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "doi.org" {
				return fixtureResponse(t, req, http.StatusOK, "doi_metadata.json", "application/json", nil), nil
			}
			if req.URL.Path == "/dyn/api/fast_download.json" {
				t.Error("tried a fast download without a verified record")
			}
			return archive(req)
		})},
	})
	_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: outageDOI}, time.Second, nil)
	if code := apperr.CodeOf(err); err == nil || code != apperr.Upstream {
		t.Fatalf("article download during an outage: code=%s err=%v", code, err)
	}
}

// editions is a search page with n editions of the outage paper, each
// with its own hash.
func editions(t *testing.T, n int) string {
	card := fixtureText(t, "search_result.html")
	var page strings.Builder
	for i := range n {
		page.WriteString(strings.ReplaceAll(card, "0123456789abcdef0123456789abcdef", fmt.Sprintf("%032x", i+1)))
	}
	return page.String()
}

func TestLookupDOIStopsAtTheFirstQueryThatFindsThePaper(t *testing.T) {
	t.Parallel()
	var searches atomic.Int32
	client := doiArchiveClient(t, http.StatusOK, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/scidb/"):
			return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
		case req.URL.Path == "/search":
			searches.Add(1)
			if q := req.URL.Query().Get("q"); q != outageTitle {
				t.Errorf("searched %q after the title search found the paper", q)
			}
			return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
		}
		return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", map[string]string{"<head>": `<head><meta name="citation_doi" content="` + outageDOI + `">`}), nil
	})
	if paper, err := client.LookupDOI(context.Background(), outageDOI, time.Minute); err != nil || paper == nil {
		t.Fatalf("lookup: %+v %v", paper, err)
	}
	if searches.Load() != 2 {
		t.Fatalf("expected the title searched on both indexes, got %d searches", searches.Load())
	}
}

func TestLookupDOIReadsRecordPagesTogetherAndKeepsTheirOrder(t *testing.T) {
	t.Parallel()
	var started sync.WaitGroup
	started.Add(doiRecordChecks)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	var records atomic.Int32
	client := doiArchiveClient(t, http.StatusOK, func(req *http.Request) (*http.Response, error) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/scidb/"):
			return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
		case req.URL.Path == "/search":
			response := fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil)
			page := editions(t, 2*doiRecordChecks)
			response.Body, response.ContentLength = io.NopCloser(strings.NewReader(page)), int64(len(page))
			return response, nil
		}
		// Each of the first pages answers only once all of them were
		// requested, which reading one page at a time would never reach.
		if records.Add(1) <= doiRecordChecks {
			started.Done()
			select {
			case <-allStarted:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}
		// Only the second and third editions are the paper; the others name
		// another author.
		detail := map[string]string{"Ashish Vaswani": "Unrelated Author"}
		if hash := path.Base(req.URL.Path); hash == fmt.Sprintf("%032x", 2) || hash == fmt.Sprintf("%032x", 3) {
			detail = nil
		}
		return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", detail), nil
	})
	paper, err := client.LookupDOI(context.Background(), outageDOI, 10*time.Second)
	if err != nil || paper == nil || paper.Hash != fmt.Sprintf("%032x", 2) {
		t.Fatalf("lookup: %+v %v", paper, err)
	}
	if records.Load() != doiRecordChecks {
		t.Fatalf("read %d record pages, want one batch of %d", records.Load(), doiRecordChecks)
	}
}
