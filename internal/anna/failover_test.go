package anna

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// requestLog records archive requests other than uptime checks.
type requestLog struct {
	mu   sync.Mutex
	seen []string
}

func (l *requestLog) add(req *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, req.URL.Host+req.URL.Path)
}

func (l *requestLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen)
}

// glFirst answers uptime checks on annas-archive.gl, and on the other
// mirrors only once glFailed is set, so gl is always the first selection.
func glFirst(t *testing.T, req *http.Request, glFailed *atomic.Bool) (*http.Response, error) {
	return mirrorUp(t, req, func(host string) bool {
		return host == "annas-archive.gl" || host == "annas-archive.pk" && glFailed.Load()
	})
}

func TestFailedRequestIsRetriedOnAnotherMirrorInTheSameCall(t *testing.T) {
	t.Parallel()
	var glFailed atomic.Bool
	var log requestLog
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/dyn/up/" {
			return glFirst(t, req, &glFailed)
		}
		log.add(req)
		if req.URL.Host == "annas-archive.gl" {
			glFailed.Store(true)
			return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
		}
		return fixtureResponse(t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
	})}})
	page, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute)
	if err != nil || len(page.Results) != 9 {
		t.Fatalf("search: %+v %v", page, err)
	}
	if got := log.list(); !slices.Equal(got, []string{"annas-archive.gl/search", "annas-archive.pk/search"}) {
		t.Fatalf("requests: %v", got)
	}
	if state := client.health.State("annas-archive.gl"); client.health.Eligible("annas-archive.gl") || state.Reason != "answered HTTP 502" {
		t.Fatalf("failed mirror was not held: %+v", state)
	}
	if state := client.health.State("annas-archive.pk"); state.LastOK.IsZero() || client.mirrors.Current() != "annas-archive.pk" {
		t.Fatalf("working mirror not recorded: %+v, current %s", state, client.mirrors.Current())
	}
}

func TestACallTriesOneOtherMirrorAtMost(t *testing.T) {
	t.Parallel()
	var log requestLog
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/dyn/up/" {
			return fixtureResponse(t, req, http.StatusOK, "mirror_up.json", "application/json", nil), nil
		}
		log.add(req)
		return fixtureResponse(t, req, http.StatusServiceUnavailable, "blocked.txt", "text/plain", nil), nil
	})}})
	_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute)
	if apperr.CodeOf(err) != apperr.Upstream || strings.Count(err.Error(), "HTTP status 503") != 2 || !strings.Contains(err.Error(), "(before that, ") {
		t.Fatalf("error does not describe both mirrors: %v", err)
	}
	if got := log.list(); len(got) != 2 || got[0] == got[1] {
		t.Fatalf("expected one search on each of two mirrors: %v", got)
	}
}

func TestOneMirrorRefusingIsHeldButTwoMeanTheAccount(t *testing.T) {
	t.Parallel()
	for name, pkRefuses := range map[string]bool{"one mirror refuses": false, "two mirrors refuse": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var glFailed atomic.Bool
			var log requestLog
			client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/dyn/up/" {
					return glFirst(t, req, &glFailed)
				}
				log.add(req)
				if req.URL.Host == "annas-archive.gl" || pkRefuses {
					glFailed.Store(true)
					return forbidden(req), nil
				}
				return fixtureResponse(t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
			})}})
			_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute)
			if len(log.list()) != 2 {
				t.Fatalf("expected a search on each of two mirrors: %v", log.list())
			}
			if !pkRefuses {
				if err != nil {
					t.Fatal(err)
				}
				if state := client.health.State("annas-archive.gl"); !state.Blocked || client.health.Eligible("annas-archive.gl") {
					t.Fatalf("refusing mirror was not held: %+v", state)
				}
				return
			}
			if apperr.CodeOf(err) != apperr.UpstreamBlocked || !strings.Contains(err.Error(), "ANNAS_ACCOUNT_COOKIE has probably expired") {
				t.Fatalf("two refusals were not reported as the account's: %v", err)
			}
			for _, host := range mirror.OfficialHosts() {
				if state := client.health.State(host); state.Failures != 0 || !client.health.Eligible(host) {
					t.Fatalf("%s was held for the account's problem: %+v", host, state)
				}
			}
		})
	}
}

func TestMissingRecordIsNotRetriedElsewhere(t *testing.T) {
	t.Parallel()
	var log requestLog
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/dyn/up/" {
			return mirrorUp(t, req, func(host string) bool { return host == "annas-archive.gl" })
		}
		log.add(req)
		return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
	})}})
	if _, err := client.Details(context.Background(), "isbndb:9780521879286", time.Minute); apperr.CodeOf(err) != apperr.NotFound {
		t.Fatalf("wrong error: %v", err)
	}
	if got := log.list(); len(got) != 1 || !client.health.Eligible("annas-archive.gl") {
		t.Fatalf("a missing record counted against the mirror: %v", got)
	}
}

func TestFastDownloadAPIFailureTriesTheSameServerOnAnotherMirror(t *testing.T) {
	t.Parallel()
	var glFailed atomic.Bool
	var log requestLog
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: t.TempDir(), HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/dyn/up/":
			return glFirst(t, req, &glFailed)
		case req.URL.Host == "downloads.example":
			return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
		}
		log.add(req)
		if req.URL.Query().Get("domain_index") != "0" {
			t.Errorf("a mirror failure used up a download server: %s", req.URL.Query().Get("domain_index"))
		}
		if req.URL.Host == "annas-archive.gl" {
			glFailed.Store(true)
			return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
		}
		return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
	})}})
	if _, err := client.DownloadBook(context.Background(), &Book{Hash: fixtureHash(t, "paper.pdf"), Format: "pdf"}, time.Minute, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"annas-archive.gl/dyn/api/fast_download.json", "annas-archive.pk/dyn/api/fast_download.json"}
	if got := log.list(); !slices.Equal(got, want) {
		t.Fatalf("API requests: %v", got)
	}
}

func TestSignInMovesOnFromAQuietMirror(t *testing.T) {
	t.Parallel()
	var log requestLog
	client := NewClient(Config{AutoBaseURL: true, SecretKey: "test-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/dyn/up/" {
			return mirrorUp(t, req, func(host string) bool { return host == "annas-archive.pk" })
		}
		log.add(req)
		switch {
		case req.URL.Path == "/account/" && req.URL.Host == "annas-archive.gl":
			return hang(req)
		case req.URL.Path == "/account/":
			return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
		case req.Header.Get("Cookie") != "aa_account_id2=minted":
			return forbidden(req), nil
		}
		return fixtureResponse(t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
	})}})
	client.signInTimeout = 50 * time.Millisecond

	start := time.Now()
	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s on a quiet mirror", elapsed)
	}
	want := []string{"annas-archive.gl/account/", "annas-archive.pk/account/", "annas-archive.pk/search"}
	if got := log.list(); !slices.Equal(got, want) {
		t.Fatalf("requests: %v", got)
	}
	if state := client.health.State("annas-archive.gl"); state.Reason != "did not answer" {
		t.Fatalf("quiet mirror was not held: %+v", state)
	}
}

func TestUnreachableSignInIsNotRemembered(t *testing.T) {
	t.Parallel()
	var down atomic.Bool
	down.Store(true)
	client := NewClient(Config{AutoBaseURL: true, SecretKey: "test-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case down.Load():
			return nil, errors.New("network is unreachable")
		case req.URL.Path == "/dyn/up/":
			return fixtureResponse(t, req, http.StatusOK, "mirror_up.json", "application/json", nil), nil
		case req.URL.Path == "/account/":
			return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
		}
		return fixtureResponse(t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
	})}})

	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute); apperr.CodeOf(err) != apperr.Upstream {
		t.Fatalf("offline: %v", err)
	}
	// The network comes back within the minute a refused sign-in would be
	// remembered for.
	down.Store(false)
	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute); err != nil {
		t.Fatalf("back online: %v", err)
	}
}

func TestMirrorHealthIsSharedWithTheNextRun(t *testing.T) {
	t.Parallel()
	sessionFile := filepath.Join(t.TempDir(), "session.json")
	var checks atomic.Int32
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/dyn/up/" {
			// Checks of the losing mirrors may still be starting when a
			// run returns, so only the winner's are counted.
			if req.URL.Host == "annas-archive.pk" {
				checks.Add(1)
			}
			return mirrorUp(t, req, func(host string) bool { return host == "annas-archive.pk" })
		}
		return fixtureResponse(t, req, http.StatusOK, noResultsPage, "text/html", nil), nil
	})
	run := func() {
		client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", SessionFile: sessionFile, HTTPClient: &http.Client{Transport: transport}})
		if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	run()
	if checks.Load() != 1 {
		t.Fatalf("first run checked the mirror %d times", checks.Load())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(sessionFile), "mirrors.json")); err != nil {
		t.Fatalf("mirror health was not saved: %v", err)
	}
	run()
	if checks.Load() != 1 {
		t.Fatal("the next run checked the mirror again")
	}
}
