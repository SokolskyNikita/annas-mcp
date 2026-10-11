package anna

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// mirrorUp answers the uptime check on the mirrors up reports as working.
// The others hang until the check gives up, so they never win a round and
// leave no failure behind once another mirror answers.
func mirrorUp(t *testing.T, req *http.Request, up func(host string) bool) (*http.Response, error) {
	if up(req.URL.Host) {
		return fixtureResponse(t, req, http.StatusOK, "mirror_up.json", "application/json", nil), nil
	}
	return hang(req)
}

func TestMirrorSelectionIsLazyAndKeepsAWorkingMirror(t *testing.T) {
	t.Parallel()
	var checks, searches atomic.Int32
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !slices.Contains(mirror.OfficialHosts(), req.URL.Host) {
			t.Errorf("request to %s", req.URL.Host)
		}
		if req.Header.Get("Cookie") != "aa_account_id2=test" {
			t.Error("missing cookie on archive request")
		}
		if req.URL.Path == "/dyn/up/" {
			// Checks of the losing mirrors may still be starting when the
			// call returns, so only the winner's are counted.
			if req.URL.Host == "annas-archive.gl" {
				checks.Add(1)
			}
			return mirrorUp(t, req, func(host string) bool { return host == "annas-archive.gl" })
		}
		searches.Add(1)
		return fixtureResponse(t, req, http.StatusOK, noResultsPage, "text/html", nil), nil
	})}})
	if checks.Load()+searches.Load() != 0 {
		t.Fatal("constructor contacted upstream")
	}
	if page, err := client.FindBook(context.Background(), "example", SearchOptions{}, callTimeout); err != nil || len(page.Results) != 0 {
		t.Fatalf("search: %v %v", page.Results, err)
	}
	if checks.Load() != 1 || searches.Load() != 1 {
		t.Fatalf("expected one check and a search; got %d checks, %d searches", checks.Load(), searches.Load())
	}
	for range 3 {
		if _, err := client.FindBook(context.Background(), "again", SearchOptions{}, callTimeout); err != nil {
			t.Fatal(err)
		}
	}
	if checks.Load() != 1 || searches.Load() != 4 {
		t.Fatalf("a working mirror was checked again: %d checks", checks.Load())
	}
}

func TestEveryMirrorDownFailsFastWithASummary(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host == "annas-archive.pk" {
			return fixtureResponse(t, req, http.StatusForbidden, "challenge.html", "text/html", nil), nil
		}
		return nil, errors.New("connection refused")
	})}})
	_, err := client.FindBook(context.Background(), "example", SearchOptions{}, callTimeout)
	if apperr.CodeOf(err) != apperr.Upstream {
		t.Fatalf("wrong code for an outage: %v", err)
	}
	for _, want := range []string{
		"no Anna's Archive mirror is working",
		"annas-archive.gl could not be reached just now",
		"annas-archive.pk answered its check with HTTP 403 just now",
		"annas-archive.gd could not be reached just now",
		"Try again in",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
	// Until a hold ends or 30 seconds pass, calls fail without contacting
	// any mirror.
	for range 3 {
		if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, callTimeout); err == nil {
			t.Fatal("search succeeded while every mirror was down")
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("calls while every mirror was held contacted them: %d requests", calls.Load())
	}
}

func TestDownloadConfigurationFailsBeforeNetwork(t *testing.T) {
	t.Parallel()
	for _, config := range []Config{{DownloadPath: "relative", SecretKey: "key"}, {DownloadPath: t.TempDir()}} {
		config.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Error("invalid configuration contacted upstream")
			return nil, errors.New("unexpected request")
		})}
		_, err := NewClient(config).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, callTimeout, nil)
		if apperr.CodeOf(err) != apperr.Config {
			t.Fatalf("wrong error: %v", err)
		}
	}
}

func TestDOIDownloadHonorsFilenameAndValidatesSciDBChecksum(t *testing.T) {
	t.Parallel()
	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	corrupt := false
	client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: dir, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/dyn/api/fast_download.json":
			return fixtureResponse(t, req, http.StatusBadRequest, "fast_download_error.json", "application/json", nil), nil
		case "/scidb/10.1000/example/":
			return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
		case "/md5/" + hash:
			return fixtureResponse(t, req, http.StatusOK, sciDBRecordPage, "text/html", nil), nil
		case "/scidb/paper.pdf":
			if req.Header.Get("Cookie") != "" {
				t.Error("sent archive cookie to SciDB PDF CDN")
			}
			name := "paper.pdf"
			if corrupt {
				name = "corrupt.pdf"
			}
			return fixtureResponse(t, req, http.StatusOK, name, "application/pdf", nil), nil
		default:
			return nil, errors.New("unexpected URL")
		}
	})}})
	result, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "https://doi.org/10.1000/example", Title: "My paper", Format: "pdf"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.Path) != "My paper.pdf" {
		t.Fatalf("ignored filename options: %s", result.Path)
	}
	if result.Bytes != int64(len(body)) {
		t.Fatalf("bytes: %d", result.Bytes)
	}
	if err := os.Remove(result.Path); err != nil {
		t.Fatal(err)
	}
	corrupt = true
	if _, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, callTimeout, nil); err == nil {
		t.Fatal("SciDB checksum mismatch was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed download left files behind: %v", entries)
	}
}

func TestDownloadRetriesShareOneDeadline(t *testing.T) {
	t.Parallel()
	var deadline time.Time
	calls := 0
	client := NewClient(Config{BaseURL: "annas.test", SecretKey: "secret", AccountCookie: "aa_account_id2=test", DownloadPath: t.TempDir(), HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		current, ok := req.Context().Deadline()
		if !ok {
			t.Error("missing operation deadline")
		}
		if deadline.IsZero() {
			deadline = current
		} else if !deadline.Equal(current) {
			t.Error("retry reset the timeout")
		}
		return nil, errors.New("temporary transport failure")
	})}})
	if _, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, callTimeout, nil); err == nil {
		t.Fatal("expected failure")
	}
	if calls != len(domainIndexes) {
		t.Fatalf("expected one attempt per download server: %d", calls)
	}
}
