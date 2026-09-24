package modes

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestDownloadToolValidatesBeforeNetworkAndWritesVerifiedFiles(t *testing.T) {
	t.Parallel()
	body := fixture(t, "document.pdf")
	digest := md5.Sum(body)
	hash := hex.EncodeToString(digest[:])
	dir := t.TempDir()
	calls := 0
	client := anna.NewClient(anna.Config{BaseURL: "https://annas.example", DownloadPath: dir, AccountCookie: "test-cookie", SecretKey: "test-secret", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		responseBody := body
		contentType := "application/pdf"
		if req.URL.Path == "/dyn/api/fast_download.json" {
			if req.URL.Query().Get("md5") != hash {
				t.Errorf("wrong requested hash")
			}
			responseBody = fixture(t, "fast-download.json")
			contentType = "application/json"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(responseBody)), ContentLength: int64(len(responseBody)), Request: req}, nil
	})}})
	ctx := t.Context()
	session := connectTestServer(t, client)
	for _, args := range []map[string]any{
		{"hash": hash, "title": "test", "format": "pdf/../../outside"},
		{"hash": "not-a-hash", "title": "test"},
	} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "book_download", Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if !result.IsError || !strings.HasPrefix(toolText(t, result), "[INVALID_ARGUMENT]") {
			t.Fatalf("expected invalid argument, got %s", toolText(t, result))
		}
	}
	if calls != 0 {
		t.Fatalf("validation made %d network calls", calls)
	}
	var previous string
	for i := 0; i < 2; i++ {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "book_download", Arguments: map[string]any{"hash": hash, "title": "Test", "format": "pdf"}})
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError {
			t.Fatal(toolText(t, result))
		}
		var downloaded anna.DownloadResult
		if err := json.Unmarshal([]byte(toolText(t, result)), &downloaded); err != nil {
			t.Fatal(err)
		}
		if downloaded.Bytes != int64(len(body)) || filepath.Dir(downloaded.Path) != dir {
			t.Fatalf("wrong result: %+v", downloaded)
		}
		if downloaded.Path == previous {
			t.Fatal("second call replaced the first file")
		}
		actual, err := os.ReadFile(downloaded.Path)
		if err != nil || !bytes.Equal(actual, body) {
			t.Fatalf("wrong saved body: %v", err)
		}
		previous = downloaded.Path
	}
}

func TestDownloadToolReportsAttemptsWithoutHTML(t *testing.T) {
	t.Parallel()
	client := anna.NewClient(anna.Config{
		BaseURL: "https://annas.example", DownloadPath: t.TempDir(),
		AccountCookie: "test-cookie", SecretKey: "test-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			status, contentType := http.StatusNotFound, "text/html"
			body := []byte("<html><body>upstream error body</body></html>")
			if req.URL.Path == "/dyn/api/fast_download.json" {
				status, contentType = http.StatusOK, "application/json"
				body = fixture(t, "fast-download.json")
			}
			return &http.Response{
				StatusCode: status, Header: http.Header{"Content-Type": {contentType}},
				Body: io.NopCloser(bytes.NewReader(body)), Request: req,
			}, nil
		})},
	})
	session := connectTestServer(t, client)
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "article_download", Arguments: map[string]any{"hash": "0123456789abcdef0123456789abcdef"},
	})
	if err != nil {
		t.Fatal(err)
	}
	message := toolText(t, result)
	if !result.IsError || !strings.HasPrefix(message, "[UPSTREAM] download failed:") {
		t.Fatalf("expected coded tool failure, got %q", message)
	}
	for _, want := range []string{"fast server 1 (file)", "fast server 2 (file)", "fast server 3 (file)", "HTTP status 404 Not Found"} {
		if !strings.Contains(message, want) {
			t.Errorf("tool result omitted %q: %s", want, message)
		}
	}
	if strings.Contains(message, "<html>") || strings.Contains(message, "upstream error body") {
		t.Fatalf("tool result included upstream HTML: %s", message)
	}
}
