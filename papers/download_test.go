package papers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

type downloadTransport func(*http.Request) (*http.Response, error)

func (f downloadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func downloadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "download", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func downloadClient(t *testing.T, archive Archive, contentStatus int) (*Client, *int) {
	t.Helper()
	apiBody := downloadFixture(t, "work.json")
	calls := 0
	transport := downloadTransport(func(r *http.Request) (*http.Response, error) {
		body := apiBody
		code := http.StatusOK
		if r.URL.Host == "content.openalex.org" || r.URL.Host == "university.example" {
			calls++
			body = downloadFixture(t, "file.pdf")
			code = contentStatus
		}
		return &http.Response{StatusCode: code, Header: make(http.Header), ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})
	client, err := NewClient(Config{OpenAlexAPIKey: "fixture-key", DownloadPath: t.TempDir(), Archive: archive, HTTPClient: &http.Client{Transport: transport}, ContentHTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	return client, &calls
}
func TestDownloadReusesHostedFileAcrossIdentifiers(t *testing.T) {
	client, calls := downloadClient(t, nil, http.StatusOK)
	first, err := client.Download(context.Background(), DownloadOptions{DOI: "10.1234/paper"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewClient(client.config)
	if err != nil {
		t.Fatal(err)
	}
	second, err := restarted.Download(context.Background(), DownloadOptions{OpenAlexID: "W1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || !second.Reused || first.Path != second.Path || first.SHA256 == "" {
		t.Fatalf("unexpected file reuse: calls=%d first=%+v second=%+v", *calls, first, second)
	}
}
func TestDownloadStrictVersionUsesMatchingExternalCopy(t *testing.T) {
	client, calls := downloadClient(t, nil, http.StatusOK)
	result, err := client.Download(context.Background(), DownloadOptions{OpenAlexID: "W1", Version: "accepted"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 || result.Route != "external_pdf" || result.Version != "acceptedVersion" || result.LocationID == "" {
		t.Fatalf("unexpected selected copy: %+v", result)
	}
}
func TestDownloadRejectsUnknownSelectedCopyBeforeContent(t *testing.T) {
	client, calls := downloadClient(t, nil, http.StatusOK)
	_, err := client.Download(context.Background(), DownloadOptions{OpenAlexID: "W1", LocationID: "not-a-location"}, nil)
	var detail *Error
	if !errors.As(err, &detail) || detail.Reason != "unknown_location" || *calls != 0 {
		t.Fatalf("expected invalid location, got %v with %d requests", err, *calls)
	}
}
func TestDownloadNeverSubstitutesUnknownPublishedVersion(t *testing.T) {
	archive := &downloadArchiveFake{}
	client, calls := downloadClient(t, archive, http.StatusOK)
	_, err := client.Download(context.Background(), DownloadOptions{OpenAlexID: "W1", Version: "published"}, nil)
	if err == nil || *calls != 0 || archive.calls != 0 {
		t.Fatalf("strict version substituted a file: %v calls=%d archive=%d", err, *calls, archive.calls)
	}
}
func TestDownloadFallsBackWithVerifiedCitation(t *testing.T) {
	archive := &downloadArchiveFake{}
	client, calls := downloadClient(t, archive, http.StatusNotFound)
	result, err := client.Download(context.Background(), DownloadOptions{DOI: "10.1234/paper"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "anna" || archive.calls != 1 || archive.options.ExpectedTitle != "A selected paper" || *calls != 2 || result.FallbackReason == "" {
		t.Fatalf("unexpected fallback: result=%+v options=%+v calls=%d", result, archive.options, *calls)
	}
}
func TestDownloadInvalidOptionsMakeNoRequests(t *testing.T) {
	var cases []struct {
		Name    string
		Options DownloadOptions
	}
	if err := json.Unmarshal(downloadFixture(t, "options.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			_, err := normalizeDownload(tc.Options)
			if err == nil {
				t.Fatal("expected argument error")
			}
		})
	}
}
func TestDownloadLocalTargetFailureDoesNotFallBack(t *testing.T) {
	archive := &downloadArchiveFake{}
	client, _ := downloadClient(t, archive, http.StatusOK)
	client.config.DownloadPath = ""
	// Construct a client whose target is unavailable before content can start.
	broken, err := NewClient(Config{OpenAlexAPIKey: "fixture-key", Archive: archive, HTTPClient: client.config.HTTPClient})
	if err != nil {
		t.Fatal(err)
	}
	_, err = broken.Download(context.Background(), DownloadOptions{DOI: "10.1234/paper"}, nil)
	if err == nil || archive.calls != 0 {
		t.Fatalf("local target failure retried in archive: %v", err)
	}
}

type downloadArchiveFake struct {
	calls   int
	options anna.ArticleDownloadOptions
}

func (f *downloadArchiveFake) FindArticle(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Paper], error) {
	return anna.SearchPage[*anna.Paper]{}, errors.New("unexpected search")
}
func (f *downloadArchiveFake) LookupDOI(context.Context, string, time.Duration) (*anna.Paper, error) {
	return nil, errors.New("unexpected lookup")
}
func (f *downloadArchiveFake) Details(context.Context, string, time.Duration) (*anna.Details, error) {
	return nil, errors.New("unexpected details")
}
func (f *downloadArchiveFake) FileStats(context.Context, []string) (map[string]anna.RecordStats, error) {
	return nil, errors.New("unexpected stats")
}
func (f *downloadArchiveFake) DownloadArticle(_ context.Context, options anna.ArticleDownloadOptions, _ time.Duration, _ anna.ProgressFunc) (anna.DownloadResult, error) {
	f.calls++
	f.options = options
	return anna.DownloadResult{Path: "/selected/file.pdf", Bytes: 12}, nil
}
