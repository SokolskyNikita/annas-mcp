package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

const currentSLUMFixture = `
<html><body>
<div class="site-card">
  <a href="annas.html" class="site-card-title">annas</a>
  <li class="domain-item-dense">
    <a href="https://annas-archive.pro">annas-archive.pro</a>
    <a class="status-badge compact protected">PROTECTED</a>
  </li>
  <li class="domain-item-dense">
    <a href="https://annas-archive.good">annas-archive.good</a>
    <a class="status-badge compact up">UP</a>
  </li>
  <li class="domain-item-dense">
    <a href="https://software.annas-archive.gl">software.annas-archive.gl</a>
    <a class="status-badge compact up">UP</a>
  </li>
</div>
<div class="site-card">
  <a href="libgen.html" class="site-card-title">libgen</a>
</div>
</body></html>`

func TestParseStatusPageExtractsCurrentSLUMAnnaDirectory(t *testing.T) {
	t.Parallel()

	candidates, err := ParseStatusPageHTML(currentSLUMFixture)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("expected 2 direct Anna candidates, got %d: %+v", len(candidates), candidates)
	}
	if candidates[0] != (Candidate{BaseURL: "annas-archive.pro", Status: "protected"}) {
		t.Fatalf("unexpected protected candidate: %+v", candidates[0])
	}
	if candidates[1] != (Candidate{BaseURL: "annas-archive.good", Status: "up"}) {
		t.Fatalf("unexpected up candidate: %+v", candidates[1])
	}
}

func TestParseStatusPageRejectsUntrustedCandidateOrigins(t *testing.T) {
	t.Parallel()

	html := `<div class="site-card">
  <a class="site-card-title">annas</a>
  <li class="domain-item-dense"><a href="https://annas-archive.good@evil.example/">bad userinfo</a><a class="status-badge compact up">UP</a></li>
  <li class="domain-item-dense"><a href="https://annas-archive.good.evil.example/">bad suffix</a><a class="status-badge compact up">UP</a></li>
  <li class="domain-item-dense"><a href="http://annas-archive.http/">bad scheme</a><a class="status-badge compact up">UP</a></li>
  <li class="domain-item-dense"><a href="https://annas-archive.path/path">bad path</a><a class="status-badge compact up">UP</a></li>
  <li class="domain-item-dense"><a href="https://annas-archive.good/">good</a><a class="status-badge compact up">UP</a></li>
</div>`

	candidates, err := ParseStatusPageHTML(html)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].BaseURL != "annas-archive.good" {
		t.Fatalf("expected only the exact HTTPS origin, got %+v", candidates)
	}
}

func TestParseStatusPageCapsCandidates(t *testing.T) {
	t.Parallel()

	var rows strings.Builder
	for i := 0; i < maxMirrorCandidates+4; i++ {
		fmt.Fprintf(&rows, `<li class="domain-item-dense"><a href="https://annas-archive.%c">mirror</a><a class="status-badge compact up">UP</a></li>`, 'a'+rune(i))
	}
	html := `<div class="site-card"><a class="site-card-title">annas</a>` + rows.String() + `</div>`
	candidates, err := ParseStatusPageHTML(html)
	if err != nil {
		t.Fatalf("ParseStatusPageHTML returned error: %v", err)
	}
	if len(candidates) != maxMirrorCandidates {
		t.Fatalf("expected %d candidates, got %d", maxMirrorCandidates, len(candidates))
	}
}

func TestResolvePrefersUpMirrorAndSkipsDownMirror(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://status.example/" {
			return nil, errors.New("unexpected request: " + req.URL.String())
		}
		return stringResponse(req, http.StatusOK, `<div class="site-card"><a class="site-card-title">annas</a>
<li class="domain-item-dense"><a href="https://annas-archive.pro">protected</a><a class="status-badge compact protected">PROTECTED</a></li>
<li class="domain-item-dense"><a href="https://annas-archive.good">up</a><a class="status-badge compact up">UP</a></li>
<li class="domain-item-dense"><a href="https://annas-archive.down">down</a><a class="status-badge compact down">DOWN</a></li></div>`), nil
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
	if baseURL != "annas-archive.good" {
		t.Fatalf("expected up mirror, got %q", baseURL)
	}
	if len(probed) != 1 || probed[0] != "annas-archive.good" {
		t.Fatalf("expected only the up mirror to be probed, got %v", probed)
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
		return stringResponse(req, http.StatusOK, currentSLUMFixture), nil
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

func TestDefaultProbeRequiresAnnaMarkerAndSendsCookie(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req
		return stringResponse(req, http.StatusOK, `<html><title>Anna's Archive search</title></html>`), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	resolver.SetAccountCookie("aa_account_id2=token")
	if err := resolver.defaultProbe(context.Background(), "annas-archive.good"); err != nil {
		t.Fatalf("defaultProbe returned error: %v", err)
	}
	if seen == nil || seen.Header.Get("Cookie") != "aa_account_id2=token" {
		t.Fatalf("expected account cookie on probe request, got %#v", seen)
	}

	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return stringResponse(req, http.StatusOK, `<html><title>challenge</title></html>`), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.good"); err == nil {
		t.Fatal("expected a response without the Anna marker to fail")
	}

	client.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body := `<html><title>Anna's Archive search</title></html>` + strings.Repeat("result ", maxProbeBodyBytes)
		return stringResponse(req, http.StatusOK, body), nil
	})
	if err := resolver.defaultProbe(context.Background(), "annas-archive.good"); err != nil {
		t.Fatalf("expected a large response with an early Anna marker to pass: %v", err)
	}
}

func TestDefaultProbeRejectsHTTPSDowngrade(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return redirectResponse(req, http.StatusFound, "http://annas-archive.good/search"), nil
	})}
	resolver := NewResolver(client, "https://status.example/", nil)
	if err := resolver.defaultProbe(context.Background(), "annas-archive.good"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected HTTPS downgrade to fail, got %v", err)
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
