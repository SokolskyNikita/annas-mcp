package modes

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
	"github.com/SokolskyNikita/annas-mcp/papers"
)

func TestOpenAlexOnlyConfigurationPreservesNoContentAndStatus(t *testing.T) {
	for _, key := range fixtureJSON[[]string](t, "environment-keys.json") {
		t.Setenv(key, "")
	}
	t.Setenv("OPENALEX_API_KEY", "fixture-key")
	t.Setenv("ANNAS_BASE_URL", "http://unsafe.example")
	t.Setenv("ANNAS_DOWNLOAD_PATH", t.TempDir())
	t.Setenv("ANNAS_MCP_CACHE_DIR", t.TempDir())
	svc, err := loadService()
	if err != nil {
		t.Fatal(err)
	}
	config := svc.paperConfig()
	if config.Archive != nil {
		t.Fatal("an OpenAlex-only configuration enabled archive fallback")
	}
	calls := 0
	config.HTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "api.openalex.org" {
			t.Fatalf("OpenAlex-only request reached another origin: %s", req.URL.Host)
		}
		name := "paper_journey/paper.json"
		if req.URL.Path == "/rate-limit" {
			name = "openalex-status.json"
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(fixture(t, name))), Header: http.Header{}, Request: req}, nil
	})}
	svc.papers, err = papers.NewClient(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.paperDownload(t.Context(), ArticleDownloadParams{DOI: "10.1000/example"}, time.Second, nil)
	var outcome *papers.Error
	if !errors.As(err, &outcome) || outcome.Code != anna.NotFound || outcome.Provider != "openalex" || outcome.Reason != "no_content" || outcome.LandingPage == "" {
		t.Fatalf("OpenAlex no-content evidence was replaced: %v", err)
	}
	archive := svc.archive.(*lazyArchive)
	if archive.client != nil || archive.err != nil {
		t.Fatal("an OpenAlex-only operation initialized Anna's Archive")
	}
	status, err := svc.providerStatus(t.Context(), AccountStatusParams{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var rendered bytes.Buffer
	if err := printResult(&rendered, status, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rendered.String(), "remaining_budget_usd") || !strings.Contains(rendered.String(), "0.75") {
		t.Fatal("default status did not render OpenAlex usage")
	}
	if calls != 2 {
		t.Fatalf("expected one work lookup and one status lookup, got %d requests", calls)
	}
}

func TestPaperConfigRetainsInjectedAndCredentialedArchive(t *testing.T) {
	archive := &fakeArchive{}
	injected := &service{archive: archive}
	if injected.paperConfig().Archive != archive {
		t.Fatal("the application dropped its supplied archive")
	}
	credentialed := &service{archive: archive, config: &env.Env{SecretKey: "archive-key"}}
	if credentialed.paperConfig().Archive != archive {
		t.Fatal("the application dropped its configured archive")
	}
	credentialed.config = &env.Env{AccountCookie: "aa_account_id2=fixture"}
	if credentialed.paperConfig().Archive != archive {
		t.Fatal("the application dropped its configured account cookie")
	}
}
