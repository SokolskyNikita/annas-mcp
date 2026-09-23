package mirror

import (
	"context"
	"embed"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

//go:embed testdata/*.html
var fixtureFiles embed.FS

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := fixtureFiles.ReadFile(path.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return string(data)
}

func TestParseStatusPageExtractsCurrentSLUMAnnaDirectory(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(fixture(t, "current_slum.html"))
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected 3 direct official Anna candidates, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0] != (Candidate{BaseURL: "annas-archive.gl", Status: "protected"}) {
		t.Fatalf("unexpected protected candidate: %+v", candidates[0])
	}
	if candidates[1] != (Candidate{BaseURL: "annas-archive.pk", Status: "up"}) {
		t.Fatalf("unexpected up candidate: %+v", candidates[1])
	}
	if candidates[2] != (Candidate{BaseURL: "annas-archive.gd", Status: "degraded"}) {
		t.Fatalf("unexpected degraded candidate: %+v", candidates[2])
	}
}

func TestParseStatusPageRejectsUntrustedCandidateOrigins(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(fixture(t, "status_untrusted.html"))
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].BaseURL != "annas-archive.gl" {
		t.Fatalf("expected only the official HTTPS origin, got %+v", candidates)
	}
}

func TestParseStatusPageOnlyReturnsOfficialCandidatesFromLargeDirectory(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(fixture(t, "status_many.html"))
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 3 {
		t.Fatalf("expected only the 3 official candidates, got %d: %+v", len(candidates), candidates)
	}
}

func TestParseStatusPageReturnsNoCandidatesWithoutAnnaCard(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(fixture(t, "status_missing_anna.html"))
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidates, got %+v", candidates)
	}
}

func TestParseStatusPageReadsTextBadgesAndMissingStatus(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(fixture(t, "status_text_badges.html"))
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	want := []Candidate{
		{BaseURL: "annas-archive.gl", Status: "degraded"},
		{BaseURL: "annas-archive.pk", Status: ""},
		{BaseURL: "annas-archive.gd", Status: ""},
	}
	if len(candidates) != len(want) {
		t.Fatalf("got %d candidates, want %d: %+v", len(candidates), len(want), candidates)
	}
	for i := range want {
		if candidates[i] != want[i] {
			t.Errorf("candidate %d = %+v, want %+v", i, candidates[i], want[i])
		}
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

func TestResolvePrefersUpMirrorAndSkipsDownMirror(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://status.example/" {
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
		return stringResponse(req, http.StatusOK, fixture(t, "status_resolve.html")), nil
	})}

	var probed []string
	resolver := NewResolver(client, "https://status.example/", func(_ context.Context, baseURL string) error {
		probed = append(probed, baseURL)
		return nil
	})
	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "fallback.example"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "annas-archive.pk" {
		t.Fatalf("expected up official mirror, got %q", baseURL)
	}
	if len(probed) != 1 || probed[0] != "annas-archive.pk" {
		t.Fatalf("expected only the up mirror to be probed, got %v", probed)
	}
}

func TestResolveNeverProbesFraudulentSLUMCandidates(t *testing.T) {
	t.Parallel()

	var probedHosts []string
	var cookieHosts []string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "status.example" {
			return stringResponse(req, http.StatusOK, fixture(t, "status_untrusted.html")), nil
		}
		probedHosts = append(probedHosts, req.URL.Host)
		if req.Header.Get("Cookie") != "" {
			cookieHosts = append(cookieHosts, req.URL.Host)
		}
		if req.URL.Host != "annas-archive.gl" {
			t.Errorf("unexpected mirror probe to %q", req.URL.Host)
		}
		return stringResponse(req, http.StatusOK, fixture(t, "probe_valid.html")), nil
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

func TestResolveFallsBackWhenStatusPageFails(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	resolver := NewResolver(client, "https://status.example/", func(context.Context, string) error { return nil })

	baseURL, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "https://fallback.example/"})
	if err != nil {
		t.Fatalf("Resolve returned error: %v", err)
	}
	if baseURL != "fallback.example" {
		t.Fatalf("expected fallback.example, got %q", baseURL)
	}
}

func TestResolveDoesNotUseUnsafeFallback(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("network down")
	})}
	resolver := NewResolver(client, "https://status.example/", nil)

	if _, err := resolver.Resolve(context.Background(), ResolveOptions{FallbackBaseURL: "http://fallback.example"}); err == nil {
		t.Fatal("expected unsafe fallback to be rejected")
	}
}

func TestResolveUsesBoundedContextBeforeFallback(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, fixture(t, "current_slum.html")), nil
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

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, fixture(t, "status_resolve.html")), nil
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
	resolver := NewResolver(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, req.Context().Err()
	})}, "https://status.example/", nil)

	if _, err := resolver.Resolve(ctx, ResolveOptions{FallbackBaseURL: "fallback.example"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Resolve error = %v, want context.Canceled", err)
	}
}

func TestDefaultProbeRequiresAnnaMarkerAndSendsCookie(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req
		return stringResponse(req, http.StatusOK, fixture(t, "probe_valid.html")), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	resolver.SetAccountCookie("aa_account_id2=token")
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err != nil {
		t.Fatalf("defaultProbe returned error: %v", err)
	}
	if seen == nil || seen.Header.Get("Cookie") != "aa_account_id2=token" {
		t.Fatalf("expected account cookie on probe request, got %#v", seen)
	}

	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, fixture(t, "probe_challenge.html")), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil {
		t.Fatal("expected a response without the Anna marker to fail")
	}

	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := fixture(t, "probe_valid.html") + strings.Repeat("result ", maxProbeBodyBytes)
		return stringResponse(req, http.StatusOK, body), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err != nil {
		t.Fatalf("expected a large response with an early Anna marker to pass: %v", err)
	}
}

func TestDefaultProbeRejectsHTTPSDowngrade(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return redirectResponse(req, http.StatusFound, "http://annas-archive.gl/search"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected HTTPS downgrade to fail, got %v", err)
	}
}

func TestDefaultProbeRejectsCrossOriginRedirectAndHTTPError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return redirectResponse(req, http.StatusFound, "https://evil.example/search"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected cross-origin redirect to fail, got %v", err)
	}

	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusForbidden, fixture(t, "probe_challenge.html")), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected HTTP error status to fail, got %v", err)
	}
}

func TestFetchRejectsOversizedStatusPage(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, strings.Repeat("x", maxStatusPageBytes+1)), nil
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

func stringResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}
}

func redirectResponse(req *http.Request, status int, location string) *http.Response {
	response := stringResponse(req, status, "redirect")
	response.Header.Set("Location", location)
	return response
}
