package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/papers"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// journeyTransport serves fixtures at the production origins, so the adapter
// test covers request construction and auth without a socket or live credential.
type journeyTransport struct {
	t            *testing.T
	mu           sync.Mutex
	contentCalls int
	otherOrigins int
}

func (j *journeyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	name, mime := "", "application/json"
	switch req.URL.Host {
	case "api.openalex.org":
		if req.Header.Get("Authorization") != "Bearer fixture-key" {
			return nil, fmt.Errorf("API request omitted its configured credential")
		}
		switch req.URL.Path {
		case "/works":
			if req.URL.Query().Get("corpus") != "all" || !strings.Contains(req.URL.Query().Get("filter"), "type:dissertation") {
				return nil, fmt.Errorf("dissertation request omitted its corpus or type")
			}
			name = "search.json"
		case "/works/W1588575005":
			name = "thesis.json"
		case "/works/https://doi.org/10.1000/example":
			name = "paper.json"
		case "/works/W999":
			name = "unsafe.json"
		}
	case "content.openalex.org":
		if req.Header.Get("Authorization") != "Bearer fixture-key" {
			return nil, fmt.Errorf("hosted content request omitted its credential")
		}
		j.contentCalls++
		name, mime = "paper.pdf", "application/pdf"
	default:
		j.otherOrigins++
		return nil, fmt.Errorf("unexpected request origin %s", req.URL.Host)
	}
	if name == "" {
		return nil, fmt.Errorf("unexpected request path %s", req.URL.Path)
	}
	body := fixture(j.t, filepath.Join("paper_journey", name))
	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {mime}},
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req,
	}, nil
}

func journeyService(t *testing.T, archive archiveClient) (*service, *journeyTransport) {
	t.Helper()
	transport := &journeyTransport{t: t}
	directory := t.TempDir()
	client, err := papers.NewClient(papers.Config{
		OpenAlexAPIKey: "fixture-key", DownloadPath: directory,
		CacheDir: t.TempDir(), Archive: archive,
		HTTPClient: &http.Client{Transport: transport}, ContentHTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &service{archive: archive, papers: client}, transport
}

func journeySession(t *testing.T, svc *service) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	t.Cleanup(cancel)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer(svc, defaultSearchTimeout, defaultDownloadTimeout)
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "paper-journey"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func journeyCLI(t *testing.T, svc *service, args ...string) []byte {
	t.Helper()
	var output bytes.Buffer
	cmd := newRootCommand(func() (*service, error) { return svc, nil })
	cmd.SetOut(&output)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestPaperJourneyReplaysNoDOIThesisAndReusesAcrossAdapters(t *testing.T) {
	t.Parallel()
	svc, transport := journeyService(t, nil)
	session := journeySession(t, svc)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "article_search", Arguments: map[string]any{"query": "graph algorithms", "work_type": "dissertation"},
	})
	if err != nil || result.IsError {
		t.Fatalf("thesis search failed: result=%+v err=%v", result, err)
	}
	var page papers.SearchResult
	if err := json.Unmarshal([]byte(toolText(t, result)), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 1 || page.Results[0].DOI != "" || page.Results[0].DownloadArgs == nil {
		t.Fatalf("search did not return an actionable no-DOI thesis: %+v", page)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "article_download", Arguments: page.Results[0].DownloadArgs})
	if err != nil || result.IsError {
		t.Fatalf("replayed download failed: result=%+v err=%v", result, err)
	}
	var downloaded papers.DownloadResult
	if err := json.Unmarshal([]byte(toolText(t, result)), &downloaded); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(downloaded.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, fixture(t, "paper_journey/paper.pdf")) || !filepath.IsAbs(downloaded.Path) || downloaded.OpenAlexID != "W1588575005" {
		t.Fatalf("download did not deliver the selected complete file: %+v", downloaded)
	}
	var reused papers.DownloadResult
	if err := json.Unmarshal(journeyCLI(t, svc, "article-download", "--openalex-id", page.Results[0].OpenAlexID, "--json"), &reused); err != nil {
		t.Fatal(err)
	}
	if !reused.Reused || reused.Path != downloaded.Path || reused.SHA256 != downloaded.SHA256 || transport.contentCalls != 1 {
		t.Fatalf("CLI repeated the paid transfer instead of reusing MCP output: %+v", reused)
	}
}

func TestPaperJourneyDOIFallbackPreservesCitationAndAttempts(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	svc, transport := journeyService(t, archive)
	var result papers.DownloadResult
	if err := json.Unmarshal(journeyCLI(t, svc, "article-download", "10.1000/example", "--json"), &result); err != nil {
		t.Fatal(err)
	}
	if result.Provider != "anna" || result.DOI != "10.1000/example" || result.FallbackReason == "" {
		t.Fatalf("fallback lost its source or reason: %+v", result)
	}
	if archive.calls != 1 || archive.article.DOI != result.DOI || archive.article.ExpectedTitle != "Fixture paper" || transport.contentCalls != 0 {
		t.Fatalf("fallback did not preserve selected identity: %+v", archive.article)
	}
}

func TestPaperJourneyUnsafeLocationReturnsRecoverableMCPFailure(t *testing.T) {
	t.Parallel()
	svc, transport := journeyService(t, nil)
	session := journeySession(t, svc)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "article_download", Arguments: map[string]any{"openalex_id": "W999", "location_id": "repository:private"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.StructuredContent == nil || transport.contentCalls != 0 || transport.otherOrigins != 0 {
		t.Fatalf("unsafe access was not a structured tool failure: %+v", result)
	}
	encoded, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var failure papers.Error
	if err := json.Unmarshal(encoded, &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Reason == "" || failure.LandingPage == "" || failure.WorkID != "W999" || len(failure.NextActions) == 0 || strings.Contains(string(encoded), "fixture-key") {
		t.Fatalf("failure lost access evidence or exposed credentials: %s", encoded)
	}
}
