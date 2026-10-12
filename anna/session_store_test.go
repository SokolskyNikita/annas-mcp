package anna

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// fakeArchive signs in with key, minting mint, and serves search only to
// cookies it considers valid. One archive outlives several clients, the way
// Anna's Archive outlives MCP server restarts.
type fakeArchive struct {
	t       *testing.T
	key     string
	mint    string
	valid   map[string]bool
	mu      sync.Mutex
	signIns int
	cookies []string
}

func newFakeArchive(t *testing.T, valid ...string) *fakeArchive {
	archive := &fakeArchive{t: t, key: "test-secret", mint: "aa_account_id2=minted", valid: map[string]bool{}}
	for _, cookie := range valid {
		archive.valid[cookie] = true
	}
	return archive
}

// run starts a client like a fresh server process, searches once and
// returns the cookies that search sent.
func (a *fakeArchive) run(config Config) []string {
	a.t.Helper()
	config.BaseURL = "annas.test"
	config.HTTPClient = &http.Client{Transport: roundTripFunc(a.roundTrip)}
	a.mu.Lock()
	seen := len(a.cookies)
	a.mu.Unlock()
	if _, err := mustClient(config).FindBook(context.Background(), "example", SearchOptions{}, callTimeout); err != nil {
		a.t.Fatal(err)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.cookies[seen:]...)
}

func (a *fakeArchive) roundTrip(req *http.Request) (*http.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	switch req.URL.Path {
	case "/account/":
		a.signIns++
		a.valid[a.mint] = true
		return fakeSignIn(a.t, req, a.key, a.mint), nil
	case "/search":
		cookie := req.Header.Get("Cookie")
		a.cookies = append(a.cookies, cookie)
		if !a.valid[cookie] {
			return forbidden(req), nil
		}
		return fixtureResponse(a.t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
	}
	return nil, errors.New("unexpected request: " + req.URL.String())
}

func TestSavedSessionIsReusedByTheNextRun(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "annas-mcp", "session.json")
	archive := newFakeArchive(t)
	config := Config{SecretKey: "test-secret", SessionFile: file}

	archive.run(config)
	if cookies := archive.run(config); strings.Join(cookies, ",") != "aa_account_id2=minted" {
		t.Fatalf("second run sent %v, want only the saved session", cookies)
	}
	if archive.signIns != 1 {
		t.Fatalf("expected the second run to reuse the saved session, got %d sign-ins", archive.signIns)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "test-secret") {
		t.Fatalf("session file contains the secret key: %s", data)
	}
	if info, err := os.Stat(file); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("session file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestRejectedConfiguredCookieIsSkippedUntilItChanges(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "session.json")
	archive := newFakeArchive(t, "aa_account_id2=pasted")
	expired := Config{SecretKey: "test-secret", SessionFile: file, AccountCookie: "expired"}

	if cookies := archive.run(expired); strings.Join(cookies, ",") != "aa_account_id2=expired,aa_account_id2=minted" {
		t.Fatalf("first run sent %v, want the expired cookie then the renewed one", cookies)
	}
	if cookies := archive.run(expired); strings.Join(cookies, ",") != "aa_account_id2=minted" {
		t.Fatalf("second run sent %v, want the expired cookie skipped", cookies)
	}
	pasted := expired
	pasted.AccountCookie = "pasted"
	if cookies := archive.run(pasted); strings.Join(cookies, ",") != "aa_account_id2=pasted" {
		t.Fatalf("run after a new cookie was set sent %v, want the new cookie", cookies)
	}
	if archive.signIns != 1 {
		t.Fatalf("expected one sign-in across all runs, got %d", archive.signIns)
	}
}

func TestSavedSessionBelongsToOneSecretKey(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "session.json")
	archive := newFakeArchive(t)
	archive.run(Config{SecretKey: "test-secret", SessionFile: file})

	archive.key, archive.mint = "other-secret", "aa_account_id2=other"
	if cookies := archive.run(Config{SecretKey: "other-secret", SessionFile: file}); strings.Join(cookies, ",") != "aa_account_id2=other" {
		t.Fatalf("a different key reused the saved session: %v", cookies)
	}
	if archive.signIns != 2 {
		t.Fatalf("expected the new key to sign in, got %d sign-ins", archive.signIns)
	}
}

func TestBrokenSessionFileOnlyCostsASignIn(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	corrupt := filepath.Join(dir, "session.json")
	if err := os.WriteFile(corrupt, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	for name, file := range map[string]string{"corrupt": corrupt, "unwritable": filepath.Join(blocker, "session.json")} {
		archive := newFakeArchive(t)
		if cookies := archive.run(Config{SecretKey: "test-secret", SessionFile: file}); strings.Join(cookies, ",") != "aa_account_id2=minted" {
			t.Fatalf("%s: sent %v, want a fresh sign-in", name, cookies)
		}
	}
	var saved savedSession
	if data, err := os.ReadFile(corrupt); err != nil || json.Unmarshal(data, &saved) != nil || saved.Cookie != "aa_account_id2=minted" {
		t.Fatalf("corrupt session file was not replaced: %v %+v", err, saved)
	}
}
