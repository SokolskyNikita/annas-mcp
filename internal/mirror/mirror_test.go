package mirror

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

//go:embed testdata
var testData embed.FS

func page(t *testing.T, name string) string {
	t.Helper()
	data, err := testData.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("missing test page %s: %v", name, err)
	}
	return string(data)
}

// loadCases decodes testdata/cases/<name>.json into into. Unknown fields fail
// the test, so a typo in a case file cannot pass silently.
func loadCases(t testing.TB, name string, into any) {
	t.Helper()
	data, err := testData.ReadFile("testdata/cases/" + name + ".json")
	if err != nil {
		t.Fatalf("missing test cases %s: %v", name, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		t.Fatalf("testdata/cases/%s.json: %v", name, err)
	}
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
	var file struct {
		Comment string
		Cases   []struct {
			Configured string
			Want       []string
		}
	}
	loadCases(t, "candidates", &file)
	for _, tc := range file.Cases {
		if got := Candidates(tc.Configured); !slices.Equal(got, tc.Want) {
			t.Errorf("Candidates(%q) = %v, want %v", tc.Configured, got, tc.Want)
		}
	}
}

func TestProberChecksUptimeEndpointAndSendsCookie(t *testing.T) {
	t.Parallel()

	// A signed-in probe gets this answer from the uptime endpoint.
	up := page(t, "uptime_signed_in.json")
	var seen *http.Request
	client := &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		seen = req
		return serve(req, http.StatusOK, up), nil
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

	// An uptime answer padded past the read limit is cut off mid-string.
	oversized, err := json.Marshal(map[string]any{"aa_logged_in": 1, "padding": strings.Repeat("x", maxProbeBodyBytes)})
	if err != nil {
		t.Fatal(err)
	}
	rejected := map[string]string{"oversized response": string(oversized)}
	for _, name := range []string{"probe_challenge.html", "archive_page.html", "json_without_aa_logged_in.json"} {
		rejected[name] = page(t, name)
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

	var file struct {
		Comment string
		Cases   []struct{ Name, Raw, Want string }
	}
	loadCases(t, "base_urls", &file)
	for _, test := range file.Cases {
		t.Run(test.Name, func(t *testing.T) {
			got, err := ParseBaseURL(test.Raw)
			if test.Want == "" {
				if err == nil {
					t.Fatalf("expected %q to fail, got %q", test.Raw, got)
				}
				return
			}
			if err != nil || got != test.Want {
				t.Fatalf("ParseBaseURL(%q) = %q, %v; want %q", test.Raw, got, err, test.Want)
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
