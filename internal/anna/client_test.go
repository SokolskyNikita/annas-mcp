package anna

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestAutomaticMirrorDiscoveryIsLazyAndUsesInjectedTransport(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := NewClient(Config{BaseURL: "fallback.example", AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host == "open-slum.org" {
			if req.Header.Get("Cookie") != "" {
				t.Error("sent account cookie to discovery service")
			}
			return fixtureResponse(t, req, http.StatusOK, "mirror_discovery.html", "text/html", nil), nil
		}
		if req.URL.Host != "annas-archive.test" {
			t.Errorf("request used %s", req.URL.Host)
		}
		if req.Header.Get("Cookie") != "aa_account_id2=test" {
			t.Error("missing cookie on archive request")
		}
		return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
	})}})
	if calls.Load() != 0 {
		t.Fatal("constructor contacted upstream")
	}
	books, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
	if err != nil || len(books) != 0 {
		t.Fatalf("search: %v %v", books, err)
	}
	if calls.Load() != 3 {
		t.Fatalf("expected discovery, probe, search; got %d requests", calls.Load())
	}
	if _, err := client.FindBook(context.Background(), "again", SearchOptions{}, time.Second); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 4 {
		t.Fatal("successful mirror was not cached")
	}
}

func TestMirrorWaiterCanCancelAndFailuresRetryAfterExpiry(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	client := NewClient(Config{BaseURL: "fallback.example", AutoBaseURL: true, Resolver: func(ctx context.Context) (string, error) {
		close(started)
		select {
		case <-release:
			return "annas-archive.test", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}})
	finished := make(chan error, 1)
	go func() { _, err := client.baseURLFor(context.Background()); finished <- err }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.baseURLFor(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}

	var resolves int
	client = NewClient(Config{BaseURL: "fallback.example", AutoBaseURL: true, Resolver: func(context.Context) (string, error) {
		resolves++
		if resolves == 1 {
			return "", errors.New("temporary discovery failure")
		}
		return "annas-archive.test", nil
	}})
	for i := 0; i < 2; i++ {
		base, err := client.baseURLFor(context.Background())
		if err != nil || base != "https://fallback.example" {
			t.Fatalf("fallback: %s %v", base, err)
		}
	}
	if resolves != 1 {
		t.Fatal("failure cache did not prevent repeated discovery")
	}
	client.baseMu.Lock()
	client.baseExpires = time.Now().Add(-time.Second)
	client.baseMu.Unlock()
	base, err := client.baseURLFor(context.Background())
	if err != nil || base != "https://annas-archive.test" || resolves != 2 {
		t.Fatalf("did not retry: %s %v %d", base, err, resolves)
	}
}

func TestFailedArchiveRequestRefreshesMirrorOnNextCall(t *testing.T) {
	t.Parallel()
	resolves := 0
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", Resolver: func(context.Context) (string, error) {
		resolves++
		if resolves == 1 {
			return "annas-archive.one", nil
		}
		return "annas-archive.two", nil
	}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "annas-archive.one" {
			return fixtureResponse(t, req, http.StatusForbidden, "blocked.txt", "text/plain", nil), nil
		}
		return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
	})}})
	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second); apperr.CodeOf(err) != apperr.UpstreamBlocked {
		t.Fatalf("wrong error: %v", err)
	}
	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second); err != nil {
		t.Fatal(err)
	}
	if resolves != 2 {
		t.Fatalf("mirror not refreshed: %d", resolves)
	}
}

func TestDownloadConfigurationFailsBeforeNetwork(t *testing.T) {
	t.Parallel()
	for _, config := range []Config{{DownloadPath: "relative", SecretKey: "key"}, {DownloadPath: t.TempDir()}} {
		config.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Error("invalid configuration contacted upstream")
			return nil, errors.New("unexpected request")
		})}
		_, err := NewClient(config).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, time.Second, nil)
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
		case "/scidb/10.1000/example":
			return fixtureResponse(t, req, http.StatusOK, "scidb_result.html", "text/html", map[string]string{"{{HASH}}": hash}), nil
		case "/md5/" + hash:
			return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", nil), nil
		case "/scidb":
			name := "paper.pdf"
			if corrupt {
				name = "corrupt.pdf"
			}
			return fixtureResponse(t, req, http.StatusOK, name, "application/pdf", nil), nil
		default:
			return nil, errors.New("unexpected URL")
		}
	})}})
	result, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "https://doi.org/10.1000/example", Title: "My paper", Format: "pdf"}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.Path) != "My paper.pdf" {
		t.Fatalf("ignored filename options: %s", result.Path)
	}
	if result.Bytes != int64(len(body)) {
		t.Fatalf("bytes: %d", result.Bytes)
	}
	corrupt = true
	if _, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, time.Second, nil); err == nil {
		t.Fatal("SciDB checksum mismatch was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
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
	if _, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef"}, time.Second, nil); err == nil {
		t.Fatal("expected failure")
	}
	if calls != 3 {
		t.Fatalf("expected three domain attempts: %d", calls)
	}
}

func TestMirrorFailureFallsBackToConfiguredOrigin(t *testing.T) {
	t.Parallel()
	resolves := 0
	client := NewClient(Config{BaseURL: "configured.example", AutoBaseURL: true, Resolver: func(context.Context) (string, error) {
		resolves++
		if resolves == 1 {
			return "annas-archive.test", nil
		}
		return "", errors.New("discovery unavailable")
	}})
	base, err := client.baseURLFor(context.Background())
	if err != nil || base != "https://annas-archive.test" {
		t.Fatalf("initial selection: %s %v", base, err)
	}
	client.invalidateMirror(base)
	base, err = client.baseURLFor(context.Background())
	if err != nil || base != "https://configured.example" {
		t.Fatalf("fallback: %s %v", base, err)
	}
}
