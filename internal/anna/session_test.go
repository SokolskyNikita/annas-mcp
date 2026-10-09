package anna

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// fakeSignIn answers Anna's account form: the right key gets a redirect that
// sets cookie, anything else gets the form back with the error message.
func fakeSignIn(t *testing.T, req *http.Request, key, cookie string) *http.Response {
	t.Helper()
	if req.Method != http.MethodPost || req.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Errorf("sign-in used %s with content type %q", req.Method, req.Header.Get("Content-Type"))
	}
	if req.Header.Get("Cookie") != "" {
		t.Errorf("sign-in sent a cookie: %q", req.Header.Get("Cookie"))
	}
	if err := req.ParseForm(); err != nil {
		t.Error(err)
	}
	if req.PostForm.Get("key") != key {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<p>Invalid secret key. Verify your key and try again.</p>")), Request: req}
	}
	header := http.Header{"Location": {"/account/"}, "Set-Cookie": {cookie + "; Domain=" + req.URL.Host + "; HttpOnly; Path=/"}}
	return &http.Response{StatusCode: http.StatusFound, Header: header, Body: io.NopCloser(strings.NewReader("")), Request: req}
}

func forbidden(req *http.Request) *http.Response {
	return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("DDoS-Guard")), Request: req}
}

func TestSignsInWithSecretKeyWhenNoCookieIsConfigured(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	signIns := 0
	client := NewClient(Config{
		BaseURL:   "annas.test",
		SecretKey: "test-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/account/":
				mu.Lock()
				signIns++
				mu.Unlock()
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
			case "/search":
				if req.Header.Get("Cookie") != "aa_account_id2=minted" {
					return forbidden(req), nil
				}
				return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
			}
			return nil, errors.New("unexpected request: " + req.URL.String())
		})},
	})

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			books, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
			if err == nil && len(books) != 1 {
				err = errors.New("expected one book")
			}
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if signIns != 1 {
		t.Fatalf("expected concurrent searches to share one sign-in, got %d", signIns)
	}
}

func TestExpiredCookieIsRenewedAndTheRequestRetried(t *testing.T) {
	t.Parallel()

	var cookies []string
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=expired",
		SecretKey:     "test-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/account/":
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=fresh"), nil
			case "/search":
				cookies = append(cookies, req.Header.Get("Cookie"))
				if req.Header.Get("Cookie") != "aa_account_id2=fresh" {
					return forbidden(req), nil
				}
				return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
			}
			return nil, errors.New("unexpected request: " + req.URL.String())
		})},
	})

	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cookies, ",") != "aa_account_id2=expired,aa_account_id2=fresh" {
		t.Fatalf("expected one retry with the renewed cookie, got %v", cookies)
	}
}

func TestRejectedSecretKeyIsAConfigErrorAndNotRetriedAtOnce(t *testing.T) {
	t.Parallel()

	signIns, searches := 0, 0
	client := NewClient(Config{
		BaseURL:   "annas.test",
		SecretKey: "wrong-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/account/":
				signIns++
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
			case "/search":
				searches++
				return forbidden(req), nil
			}
			return nil, errors.New("unexpected request: " + req.URL.String())
		})},
	})

	for range 2 {
		_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
		if apperr.CodeOf(err) != apperr.Config || !strings.Contains(err.Error(), "rejected ANNAS_SECRET_KEY") {
			t.Fatalf("expected a rejected-key config error, got %v", err)
		}
		if strings.Contains(err.Error(), "wrong-secret") {
			t.Fatalf("error leaked the secret key: %v", err)
		}
	}
	if signIns != 1 || searches != 0 {
		t.Fatalf("expected one sign-in and no searches, got %d sign-ins and %d searches", signIns, searches)
	}
}

func TestForbiddenFreshSessionPointsAtMembership(t *testing.T) {
	t.Parallel()

	signIns := 0
	client := NewClient(Config{
		BaseURL:   "annas.test",
		SecretKey: "test-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/account/":
				signIns++
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
			case "/search":
				return forbidden(req), nil
			}
			return nil, errors.New("unexpected request: " + req.URL.String())
		})},
	})

	_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
	if apperr.CodeOf(err) != apperr.UpstreamBlocked || !strings.Contains(err.Error(), "Lucky Librarian") {
		t.Fatalf("expected a membership hint, got %v", err)
	}
	if !strings.Contains(err.Error(), "copy the aa_account_id2 value into ANNAS_ACCOUNT_COOKIE") {
		t.Fatalf("expected the steps for setting the cookie by hand, got %v", err)
	}
	if signIns != 1 {
		t.Fatalf("a session minted moments ago was renewed again: %d sign-ins", signIns)
	}
}

func TestSignInFallsBackToAnotherOfficialMirror(t *testing.T) {
	t.Parallel()

	var hosts []string
	client := NewClient(Config{
		BaseURL:     "annas-archive.gl",
		AutoBaseURL: true,
		SecretKey:   "test-secret",
		Resolver:    func(context.Context) (string, error) { return "https://annas-archive.pk", nil },
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/account/":
				hosts = append(hosts, req.URL.Host)
				if req.URL.Host == "annas-archive.gl" {
					return nil, errors.New("connection refused")
				}
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=minted"), nil
			case "/search":
				if req.URL.Host != "annas-archive.pk" || req.Header.Get("Cookie") != "aa_account_id2=minted" {
					return forbidden(req), nil
				}
				return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
			}
			return nil, errors.New("unexpected request: " + req.URL.String())
		})},
	})

	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Join(hosts, ",") != "annas-archive.gl,annas-archive.pk" {
		t.Fatalf("unexpected sign-in hosts: %v", hosts)
	}
}

func TestFailedSignInExplainsHowToSetTheCookieByHand(t *testing.T) {
	t.Parallel()

	blocked := func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusForbidden, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<title>DDoS-Guard</title>")), Request: req}
	}
	noSession := func(req *http.Request) *http.Response {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("<form action=\"/account/\">")), Request: req}
	}
	tests := map[string]struct {
		config  Config
		answers map[string]func(*http.Request) *http.Response
		code    string
		reasons []string
	}{
		"every mirror refuses": {
			config: Config{BaseURL: "annas-archive.gl", AutoBaseURL: true, Resolver: func(context.Context) (string, error) { return "https://annas-archive.gl", nil }},
			answers: map[string]func(*http.Request) *http.Response{
				"annas-archive.gl": blocked,
				"annas-archive.gd": noSession,
			},
			code: apperr.UpstreamBlocked,
			reasons: []string{
				"annas-archive.gl put the sign-in form behind a DDoS-Guard browser check",
				"request to annas-archive.pk failed",
				"annas-archive.gd answered the sign-in form with HTTP status 200 but no session",
			},
		},
		"the only mirror is unreachable": {
			config:  Config{BaseURL: "annas.test"},
			code:    apperr.Upstream,
			reasons: []string{"request to annas.test failed"},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			config := tt.config
			config.SecretKey = "test-secret"
			config.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/account/" {
					t.Errorf("searched after sign-in failed: %s", req.URL)
				}
				if answer := tt.answers[req.URL.Host]; answer != nil {
					return answer(req), nil
				}
				return nil, errors.New("connection refused")
			})}
			client := NewClient(config)

			_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
			if apperr.CodeOf(err) != tt.code {
				t.Fatalf("expected %s, got %s: %v", tt.code, apperr.CodeOf(err), err)
			}
			message := err.Error()
			for _, want := range append(tt.reasons, "automatic sign-in with ANNAS_SECRET_KEY failed", "copy the aa_account_id2 value into ANNAS_ACCOUNT_COOKIE") {
				if !strings.Contains(message, want) {
					t.Errorf("error lacks %q: %s", want, message)
				}
			}
			if strings.Contains(message, "test-secret") {
				t.Fatalf("error leaked the secret key: %s", message)
			}
		})
	}
}

func TestExpiredCookieWithoutSecretKeyExplainsBothFixes(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=expired",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/search" {
				t.Errorf("unexpected request without a secret key: %s", req.URL)
			}
			return forbidden(req), nil
		})},
	})

	_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Second)
	if apperr.CodeOf(err) != apperr.UpstreamBlocked {
		t.Fatalf("expected an upstream-blocked error, got %v", err)
	}
	for _, want := range []string{"Set ANNAS_SECRET_KEY", "copy the aa_account_id2 value into ANNAS_ACCOUNT_COOKIE"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}
