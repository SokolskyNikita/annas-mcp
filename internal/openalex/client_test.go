package openalex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestLookupNoDOIThesis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/works/W1588575005" {
			t.Errorf("wrong route %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing scoped authorization")
		}
		if r.URL.Query().Get("api_key") != "" {
			t.Error("key leaked into query")
		}
		_, _ = w.Write(fixture(t, "work.json"))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "test-key", BaseURL: server.URL})
	work, err := client.Lookup(context.Background(), "W1588575005")
	if err != nil {
		t.Fatal(err)
	}
	if work.DOI != "" || !work.HasContent.PDF || work.ShortID() != "W1588575005" {
		t.Fatalf("bad thesis: %#v", work)
	}
	if work.Abstract() != "A graph thesis." {
		t.Fatal(work.Abstract())
	}
	if !work.HasDOI("10.1234/example") || work.HasDOI("10.1234/wrong") || work.HasDOI("") {
		t.Error("DOI location matching failed")
	}
	if work.LandingURL() != "https://theses.gla.ac.uk/123/" {
		t.Fatal(work.LandingURL())
	}
	work.PrimaryLocation = nil
	if work.LandingURL() != work.ID {
		t.Error("expected ID fallback")
	}
	work.Locations[0].LandingPageURL = "https://example.org"
	if work.LandingURL() != "https://example.org" {
		t.Error("missing alternate landing")
	}
}
func TestSearchAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rate-limit" {
			_, _ = w.Write(fixture(t, "status.json"))
			return
		}
		if r.URL.Query().Get("corpus") != "all" {
			t.Error("thesis expansion omitted")
		}
		if !strings.Contains(r.URL.Query().Get("filter"), "type:dissertation") {
			t.Error("thesis filter omitted")
		}
		_, _ = w.Write(fixture(t, "search.json"))
	}))
	defer server.Close()
	client := NewClient(Config{APIKey: "key", BaseURL: server.URL, HTTPClient: server.Client()})
	result, err := client.Search(context.Background(), SearchOptions{Query: "graphs", WorkTypes: []string{"dissertation"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Results) != 1 || result.Meta.NextCursor != "next-cursor" {
		t.Fatal(result)
	}
	status, err := client.Status(context.Background())
	if err != nil || (*status)["credits_remaining"] == nil {
		t.Fatalf("status %v %v", status, err)
	}
}
func TestMissingKeyAndCanceled(t *testing.T) {
	client := NewClient(Config{})
	_, err := client.Lookup(context.Background(), "W123")
	var coded *Error
	if !errors.As(err, &coded) || coded.Reason != "missing_key" {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Lookup(ctx, "W123")
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = client.Lookup(context.Background(), "bad")
	if !errors.As(err, &coded) || coded.ErrorCode() != "INVALID_ARGUMENT" {
		t.Fatal(err)
	}
}
func TestProviderErrors(t *testing.T) {
	var cases []struct {
		Status       int
		Body, Reason string
		Retryable    bool
	}
	if err := json.Unmarshal(fixture(t, "errors.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Reason, func(t *testing.T) {
			response := &http.Response{StatusCode: tc.Status, Header: http.Header{"Retry-After": []string{"3"}}, Body: io.NopCloser(strings.NewReader(tc.Body))}
			err := responseError(response, "secret")
			var coded *Error
			if !errors.As(err, &coded) || coded.Reason != tc.Reason || coded.Retryable != tc.Retryable || coded.RetryAfter != 3 {
				t.Fatal(err)
			}
		})
	}
	if parseRetryAfter("bad") != 0 || parseRetryAfter(time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)) < 50 {
		t.Error("retry parsing")
	}
}
func TestIdentityAndInvalidResponse(t *testing.T) {
	var response []byte = fixture(t, "work.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(response) }))
	defer server.Close()
	client := NewClient(Config{APIKey: "key", BaseURL: server.URL})
	_, err := client.Lookup(context.Background(), "10.1234/wrong")
	var coded *Error
	if !errors.As(err, &coded) || coded.Reason != "identity_mismatch" {
		t.Fatal(err)
	}
	response = []byte("broken")
	_, err = client.Lookup(context.Background(), "W123")
	if !errors.As(err, &coded) || coded.Reason != "invalid_response" {
		t.Fatal(err)
	}
	response = []byte("null")
	_, err = client.Lookup(context.Background(), "W123")
	if !errors.As(err, &coded) || coded.Reason != "invalid_response" {
		t.Fatal(err)
	}
}
func TestRedirectDoesNotLeakKey(t *testing.T) {
	requests := 0
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "" {
			t.Error("key leaked")
		}
	}))
	defer external.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, external.URL, http.StatusFound) }))
	defer origin.Close()
	_, err := NewClient(Config{APIKey: "secret", BaseURL: origin.URL}).Lookup(context.Background(), "W123")
	if err == nil || requests != 0 || strings.Contains(err.Error(), "secret") {
		t.Fatal("redirect must stop with redacted error")
	}
}
func TestSearchFilters(t *testing.T) {
	values, err := searchValues(SearchOptions{Query: "graphs", Title: "trees", Author: "Jane Smith", AuthorID: "A123", InstitutionID: "I123", RepositoryID: "S123", Year: "2020-2024", Language: "en,de", WorkTypes: []string{"any"}, Sort: "most_cited", PerPage: 100, Cursor: "*", DownloadableOnly: true, Relation: "citing", WorkID: "W123"})
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("per_page") != "100" || values.Get("sort") != "cited_by_count:desc" || !strings.Contains(values.Get("filter"), "cites:W123") {
		t.Fatal(values)
	}
	for _, relation := range []string{"references", "related"} {
		_, err = searchValues(SearchOptions{Query: "graphs", Relation: relation, WorkID: "W123"})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, sort := range []string{"newest", "oldest", "relevance"} {
		_, err = searchValues(SearchOptions{Query: "graphs", Sort: sort})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = searchValues(SearchOptions{Query: "graphs", AuthorID: "0000-0001-2345-6789"})
	if err != nil {
		t.Fatal(err)
	}
	var cases []SearchOptions
	if err = json.Unmarshal(fixture(t, "invalid_search.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		if _, err = searchValues(tc); err == nil {
			t.Errorf("case %d unexpectedly valid", i)
		}
	}
}
func TestIdentifiers(t *testing.T) {
	for _, input := range []string{"W123", "https://openalex.org/W123", "w123"} {
		id, err := CanonicalID(input)
		if err != nil || id != "W123" {
			t.Fatal(id, err)
		}
	}
	for _, input := range []string{"doi:10.1234/EXAMPLE", "https://doi.org/10.1234/example", "https://dx.doi.org/10.1234/example"} {
		id, err := CanonicalID(input)
		if err != nil || id != "https://doi.org/10.1234/example" {
			t.Fatal(id, err)
		}
	}
	invalid := url.URL{Scheme: "https", Host: "openalex.org", Path: "/W123", User: url.User("user")}
	if _, err := CanonicalID(invalid.String()); err == nil {
		t.Error("accepted userinfo")
	}
}

func TestRedirectCannotDowngradeTransport(t *testing.T) {
	client := NewClient(Config{APIKey: "secret"})
	initial, _ := http.NewRequest(http.MethodGet, "https://api.openalex.org/works/W123", nil)
	next, _ := http.NewRequest(http.MethodGet, "http://api.openalex.org/works/W123", nil)
	next.Header.Set("Authorization", "Bearer secret")
	if err := client.http.CheckRedirect(next, []*http.Request{initial}); err == nil {
		t.Error("accepted scheme downgrade")
	}
	if next.Header.Get("Authorization") != "" {
		t.Error("credential retained on rejected redirect")
	}
}
func TestInvalidEndpoint(t *testing.T) {
	client := NewClient(Config{APIKey: "secret", BaseURL: "https://user@api.openalex.org"})
	_, err := client.Lookup(context.Background(), "W123")
	var coded *Error
	if !errors.As(err, &coded) || coded.Reason != "invalid_endpoint" {
		t.Fatal(err)
	}
}

func TestDOIPunctuationRemainsInRequestPath(t *testing.T) {
	expected := "/works/https://doi.org/10.1234/a?b#c"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != expected || r.URL.RawQuery != "" {
			t.Errorf("wrong DOI URL: %s", r.URL.String())
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	_, _ = NewClient(Config{APIKey: "key", BaseURL: server.URL}).Lookup(context.Background(), "10.1234/a?b#c")
}
func TestFilterOnlyDiscovery(t *testing.T) {
	_, err := searchValues(SearchOptions{WorkTypes: []string{"dissertation"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = searchValues(SearchOptions{WorkTypes: []string{"any"}, Year: "2020"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStatusNeverEchoesCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(fixture(t, "status.json")) }))
	defer server.Close()
	status, err := NewClient(Config{APIKey: "key", BaseURL: server.URL}).Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "api_key") || strings.Contains(string(raw), "access_token") {
		t.Fatalf("unsanitized status: %s", raw)
	}
	if (*status)["note"] != "[redacted] redacted" {
		t.Error("credential in value not redacted")
	}
}
