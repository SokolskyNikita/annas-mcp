package mirror

import (
	"context"
	"embed"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

//go:embed testdata/*.html
var testPages embed.FS

func page(t *testing.T, name string) string {
	t.Helper()
	data, err := testPages.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("missing test page %s: %v", name, err)
	}
	return string(data)
}

// transport answers requests without a network; it satisfies http.RoundTripper.
type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func serve(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Request:    req,
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func redirect(req *http.Request, status int, location string) *http.Response {
	resp := serve(req, status, "redirect")
	resp.Header.Set("Location", location)
	return resp
}

// statusPage returns a client whose every request receives the named page.
func statusPage(t *testing.T, name string) *http.Client {
	return &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusOK, page(t, name)), nil
	})}
}

func offline() *http.Client {
	return &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
}

func TestParseStatusPageHTML(t *testing.T) {
	t.Parallel()

	cases := map[string][]Candidate{
		// The live SLUM layout lists each official domain with a status badge.
		"current_slum.html": {
			{BaseURL: "annas-archive.gl", Status: "protected"},
			{BaseURL: "annas-archive.pk", Status: "up"},
			{BaseURL: "annas-archive.gd", Status: "degraded"},
		},
		// Look-alike and non-HTTPS domains are dropped.
		"status_untrusted.html": {{BaseURL: "annas-archive.gl", Status: "up"}},
		"status_text_badges.html": {
			{BaseURL: "annas-archive.gl", Status: "degraded"},
			{BaseURL: "annas-archive.pk"},
			{BaseURL: "annas-archive.gd"},
		},
		"status_missing_anna.html": nil,
	}
	for name, want := range cases {
		got, err := ParseStatusPageHTML(page(t, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s: got %+v, want %+v", name, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s[%d] = %+v, want %+v", name, i, got[i], want[i])
			}
		}
	}

	// A large directory still yields only the three official domains.
	many, err := ParseStatusPageHTML(page(t, "status_many.html"))
	if err != nil || len(many) != 3 {
		t.Fatalf("status_many.html: got %d candidates (%v), want 3", len(many), err)
	}
}

func TestNewResolverValidatesStatusPageAndProvidesDefaultClient(t *testing.T) {
	t.Parallel()

	resolver := NewResolver(nil, "http://status.example/path", nil)
	if resolver.statusPageURL != DefaultStatusPageURL {
		t.Fatalf("invalid status page URL selected %q", resolver.statusPageURL)
	}
	if resolver.client == nil || resolver.client.Timeout != defaultProbeTimeout {
		t.Fatalf("unexpected default client: %#v", resolver.client)
	}

	resolver = NewResolver(nil, "https://status.example/status", nil)
	if resolver.statusPageURL != "https://status.example/status" {
		t.Fatalf("valid status page URL changed to %q", resolver.statusPageURL)
	}
}

func TestParseCandidateURLRequiresTrustedHTTPSOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "canonical official host", raw: "https://ANNAS-ARCHIVE.GL/", want: "annas-archive.gl"},
		{name: "official pk host", raw: "https://annas-archive.pk", want: "annas-archive.pk"},
		{name: "official gd host", raw: "annas-archive.gd", want: "annas-archive.gd"},
		{name: "arbitrary suffix", raw: "https://annas-archive.good"},
		{name: "fraudulent su suffix", raw: "https://annas-archive.su"},
		{name: "fraudulent io suffix", raw: "https://annas-archive.io"},
		{name: "fraudulent is suffix", raw: "https://annas-archive.is"},
		{name: "fraudulent cc suffix", raw: "https://annas-archive.cc"},
		{name: "userinfo", raw: "https://annas-archive.gl@evil.example"},
		{name: "suffix", raw: "https://annas-archive.gl.evil.example"},
		{name: "port", raw: "https://annas-archive.gl:443"},
		{name: "path", raw: "https://annas-archive.gl/search"},
		{name: "http", raw: "http://annas-archive.gl"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseCandidateURL(test.raw)
			if test.want == "" {
				if err == nil {
					t.Fatalf("ParseCandidateURL(%q) unexpectedly returned %q", test.raw, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ParseCandidateURL(%q) = %q, %v; want %q", test.raw, got, err, test.want)
			}
		})
	}
}

func TestResolveSelection(t *testing.T) {
	t.Parallel()

	accept := func(context.Context, string) error { return nil }
	cases := []struct {
		name     string
		client   *http.Client
		probe    ProbeFunc
		fallback string
		want     string
		wantErr  bool
	}{
		{name: "prefers the up mirror", client: statusPage(t, "status_resolve.html"), probe: accept, fallback: "fallback.example", want: "annas-archive.pk"},
		{name: "status page unreachable uses fallback", client: offline(), probe: accept, fallback: "https://fallback.example/", want: "fallback.example"},
		{name: "insecure fallback is ignored", client: offline(), fallback: "http://fallback.example", wantErr: true},
		{name: "no fallback reports the failure", client: offline(), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var probed []string
			probe := tc.probe
			if probe != nil {
				probe = func(ctx context.Context, baseURL string) error {
					probed = append(probed, baseURL)
					return tc.probe(ctx, baseURL)
				}
			}
			got, err := NewResolver(tc.client, "https://status.example/", probe).Resolve(context.Background(), ResolveOptions{FallbackBaseURL: tc.fallback})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Resolve = %q, want an error", got)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("Resolve = %q, %v; want %q", got, err, tc.want)
			}
			if tc.want == "annas-archive.pk" && (len(probed) != 1 || probed[0] != tc.want) {
				t.Fatalf("probed %v; the first healthy candidate should end the search", probed)
			}
		})
	}
}

func TestResolveNeverProbesFraudulentSLUMCandidates(t *testing.T) {
	t.Parallel()

	var probedHosts []string
	var cookieHosts []string
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "status.example" {
			return serve(req, http.StatusOK, page(t, "status_untrusted.html")), nil
		}
		probedHosts = append(probedHosts, req.URL.Host)
		if req.Header.Get("Cookie") != "" {
			cookieHosts = append(cookieHosts, req.URL.Host)
		}
		if req.URL.Host != "annas-archive.gl" {
			t.Errorf("unexpected mirror probe to %q", req.URL.Host)
		}
		return serve(req, http.StatusOK, page(t, "probe_valid.html")), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	resolver.SetAccountCookie("aa_account_id2=token")

	selected, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if selected != "annas-archive.gl" {
		t.Fatalf("selected %q, want official annas-archive.gl", selected)
	}
	if len(probedHosts) != 1 || probedHosts[0] != "annas-archive.gl" {
		t.Fatalf("unexpected probed hosts: %v", probedHosts)
	}
	if len(cookieHosts) != 1 || cookieHosts[0] != "annas-archive.gl" {
		t.Fatalf("account cookie reached unexpected hosts: %v", cookieHosts)
	}
}

func TestResolveUsesBoundedContextBeforeFallback(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusOK, page(t, "current_slum.html")), nil
	})}
	resolver := NewResolver(client, "https://status.example/", func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	start := time.Now()
	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{
		FallbackBaseURL: "fallback.example",
		Timeout:         20 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("expected fallback.example, got %q", baseURL)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("resolution exceeded bounded timeout: %s", elapsed)
	}
}

func TestResolveFallsBackAfterAllProbesFail(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusOK, page(t, "status_resolve.html")), nil
	})}
	var probed []string
	resolver := NewResolver(client, "https://status.example/", func(_ context.Context, baseURL string) error {
		probed = append(probed, baseURL)
		return errors.New("mirror unavailable")
	})

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{
		FallbackBaseURL: "fallback.example",
		MaxCandidates:   1,
	})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("expected fallback.example, got %q", baseURL)
	}
	if len(probed) != 1 || probed[0] != "annas-archive.pk" {
		t.Fatalf("expected only highest-ranked candidate to be probed, got %v", probed)
	}
}

func TestResolvePropagatesCallerCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	resolver := NewResolver(&http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}, "https://status.example/", nil)

	if _, err := resolver.Resolve(ctx, ResolveOptions{FallbackBaseURL: "fallback.example"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
}

func TestDefaultProbeRequiresAnnaMarkerAndSendsCookie(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		seen = req
		return serve(req, http.StatusOK, page(t, "probe_valid.html")), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	resolver.SetAccountCookie("aa_account_id2=token")
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err != nil {
		t.Fatalf("defaultProbe returned error: %v", err)
	}
	if seen == nil || seen.Header.Get("Cookie") != "aa_account_id2=token" {
		t.Fatalf("expected account cookie on probe request, got %#v", seen)
	}

	client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusOK, page(t, "probe_challenge.html")), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil {
		t.Fatal("expected a response without the Anna marker to fail")
	}

	client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		body := page(t, "probe_valid.html") + strings.Repeat("result ", maxProbeBodyBytes)
		return serve(req, http.StatusOK, body), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err != nil {
		t.Fatalf("expected a large response with an early Anna marker to pass: %v", err)
	}
}

func TestDefaultProbeRejectsHTTPSDowngrade(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return redirect(req, http.StatusFound, "http://annas-archive.gl/search"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected HTTPS downgrade to fail, got %v", err)
	}
}

func TestDefaultProbeRejectsCrossOriginRedirectAndHTTPError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return redirect(req, http.StatusFound, "https://evil.example/search"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected cross-origin redirect to fail, got %v", err)
	}

	client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusForbidden, page(t, "probe_challenge.html")), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected HTTP error status to fail, got %v", err)
	}
}

func TestFetchRejectsOversizedStatusPage(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusOK, strings.Repeat("x", maxStatusPageBytes+1)), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if _, err := resolver.fetch(context.Background(), "https://status.example/"); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected oversized status page to fail, got %v", err)
	}
}

func TestParseBaseURLRejectsDowngradeAndURLParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "bare host", raw: "fallback.example", want: "fallback.example"},
		{name: "https origin", raw: "https://Fallback.Example/", want: "fallback.example"},
		{name: "explicit fraudulent-looking host is a deliberate override", raw: "https://annas-archive.su/", want: "annas-archive.su"},
		{name: "http", raw: "http://fallback.example"},
		{name: "path", raw: "https://fallback.example/path"},
		{name: "userinfo", raw: "https://user:fallback@example.com"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			got, err := ParseBaseURL(test.raw)
			if test.want == "" {
				if err == nil {
					t.Fatalf("expected %q to fail, got %q", test.raw, got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("ParseBaseURL(%q) = %q, %v; want %q", test.raw, got, err, test.want)
			}
		})
	}
}
