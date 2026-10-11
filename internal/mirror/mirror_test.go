package mirror

import (
	"context"
	"embed"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

//go:embed testdata/*.html
var testPages embed.FS

// upResponse is what a mirror's uptime endpoint returns to a signed-in probe.
const upResponse = `{"aa_logged_in":1}`

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

func TestCandidatesPutTheConfiguredHostFirst(t *testing.T) {
	t.Parallel()
	for configured, want := range map[string][]string{
		"":                 {"annas-archive.gl", "annas-archive.pk", "annas-archive.gd"},
		"annas-archive.pk": {"annas-archive.pk", "annas-archive.gl", "annas-archive.gd"},
		"proxy.example":    {"proxy.example", "annas-archive.gl", "annas-archive.pk", "annas-archive.gd"},
	} {
		if got := Candidates(configured); !slices.Equal(got, want) {
			t.Errorf("Candidates(%q) = %v, want %v", configured, got, want)
		}
	}
}

func TestProberChecksUptimeEndpointAndSendsCookie(t *testing.T) {
	t.Parallel()

	var seen *http.Request
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		seen = req
		return serve(req, http.StatusOK, upResponse), nil
	})}
	prober := NewProber(client, func() string { return "aa_account_id2=token" })
	if err := prober.Check(context.Background(), "annas-archive.gl"); err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if seen == nil || seen.URL.String() != "https://annas-archive.gl/dyn/up/" {
		t.Fatalf("probe requested %v, want the uptime endpoint", seen)
	}
	if seen.Header.Get("Cookie") != "aa_account_id2=token" {
		t.Fatalf("expected account cookie on probe request, got %q", seen.Header.Get("Cookie"))
	}

	rejected := map[string]string{
		"challenge page":          page(t, "probe_challenge.html"),
		"archive HTML page":       "<html><head><title>Anna's Archive</title></head></html>",
		"JSON without the status": `{"status":"ok"}`,
		"oversized response":      `{"aa_logged_in":1,"padding":"` + strings.Repeat("x", maxProbeBodyBytes) + `"}`,
	}
	for name, body := range rejected {
		client.Transport = transport(func(req *http.Request) (*http.Response, error) {
			return serve(req, http.StatusOK, body), nil
		})
		if err := prober.Check(context.Background(), "annas-archive.gl"); !errors.Is(err, errNotUptime) {
			t.Errorf("%s: probe returned %v, want errNotUptime", name, err)
		}
	}
}

func TestProberRejectsHTTPSDowngrade(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return redirect(req, http.StatusFound, "http://annas-archive.gl/search"), nil
	})}
	if err := NewProber(client, nil).Check(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("expected HTTPS downgrade to fail, got %v", err)
	}
}

func TestProberRejectsCrossOriginRedirectAndHTTPError(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		return redirect(req, http.StatusFound, "https://evil.example/search"), nil
	})}
	prober := NewProber(client, nil)
	if err := prober.Check(context.Background(), "annas-archive.gl"); err == nil || !strings.Contains(err.Error(), "cross-origin") {
		t.Fatalf("expected cross-origin redirect to fail, got %v", err)
	}

	client.Transport = transport(func(req *http.Request) (*http.Response, error) {
		return serve(req, http.StatusForbidden, page(t, "probe_challenge.html")), nil
	})
	err := prober.Check(context.Background(), "annas-archive.gl")
	if failure := checkFailure(err); failure.Reason != "answered its check with HTTP 403" || !failure.Blocked {
		t.Fatalf("expected a blocked check, got %+v from %v", failure, err)
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
		{name: "port", raw: "https://fallback.example:8443", want: "fallback.example:8443"},
		{name: "explicit fraudulent-looking host is a deliberate override", raw: "https://annas-archive.su/", want: "annas-archive.su"},
		{name: "http", raw: "http://fallback.example"},
		{name: "path", raw: "https://fallback.example/path"},
		{name: "userinfo", raw: "https://user:fallback@example.com"},
		{name: "a language version of an official mirror", raw: "https://es.annas-archive.gl"},
		{name: "a three-letter language version", raw: "kmr.annas-archive.pk"},
		{name: "www is not a language", raw: "www.annas-archive.gl", want: "www.annas-archive.gl"},
		{name: "a subdomain of another host", raw: "es.example.com", want: "es.example.com"},
	}
	for _, test := range tests {
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

func TestParseBaseURLNamesTheEnglishMirror(t *testing.T) {
	t.Parallel()

	if _, err := ParseBaseURL("de.annas-archive.gd"); err == nil || !strings.Contains(err.Error(), "use annas-archive.gd") {
		t.Fatalf("err = %v", err)
	}
}
