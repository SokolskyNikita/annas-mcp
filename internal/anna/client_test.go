package anna

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

type clientRoundTrip func(*http.Request) (*http.Response, error)

func (f clientRoundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func fakeResponse(req *http.Request, status int, body, contentType string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func TestAutomaticMirrorDiscoveryIsLazyAndUsesInjectedTransport(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	client := NewClient(Config{BaseURL: "fallback.example", AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: clientRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		if req.URL.Host == "open-slum.org" {
			if req.Header.Get("Cookie") != "" {
				t.Error("sent account cookie to discovery service")
			}
			return fakeResponse(req, 200, `<div class="site-card"><div class="site-card-title">annas</div><div class="domain-item-dense"><a href="https://annas-archive.test/">mirror</a><span class="status-badge compact up">UP</span></div></div>`, "text/html"), nil
		}
		if req.URL.Host != "annas-archive.test" {
			t.Errorf("request used %s", req.URL.Host)
		}
		if req.Header.Get("Cookie") != "aa_account_id2=test" {
			t.Error("missing cookie on archive request")
		}
		return fakeResponse(req, 200, "<title>Search - Anna's Archive</title>", "text/html"), nil
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
	client := NewClient(Config{AutoBaseURL: true, Resolver: func(context.Context) (string, error) {
		resolves++
		if resolves == 1 {
			return "annas-archive.one", nil
		}
		return "annas-archive.two", nil
	}, HTTPClient: &http.Client{Transport: clientRoundTrip(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "annas-archive.one" {
			return fakeResponse(req, 403, "blocked", "text/html"), nil
		}
		return fakeResponse(req, 200, "<title>Anna's Archive</title>", "text/html"), nil
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
		config.HTTPClient = &http.Client{Transport: clientRoundTrip(func(req *http.Request) (*http.Response, error) {
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
	body := "%PDF-1.5\nTest paper\n"
	sum := md5.Sum([]byte(body))
	hash := hex.EncodeToString(sum[:])
	dir := t.TempDir()
	client := NewClient(Config{BaseURL: "annas.test", DownloadPath: dir, HTTPClient: &http.Client{Transport: clientRoundTrip(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/scidb/10.1000/example":
			return fakeResponse(req, 200, `<a href="/md5/`+hash+`">file</a>`, "text/html"), nil
		case "/md5/" + hash:
			return fakeResponse(req, 200, "<title>Original title - Anna's Archive</title>", "text/html"), nil
		case "/scidb":
			return fakeResponse(req, 200, body, "application/pdf"), nil
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
	body = "%PDF-1.5\nCorrupt replacement\n"
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
	client := NewClient(Config{BaseURL: "annas.test", SecretKey: "secret", DownloadPath: t.TempDir(), HTTPClient: &http.Client{Transport: clientRoundTrip(func(req *http.Request) (*http.Response, error) {
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
