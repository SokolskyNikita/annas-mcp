package anna

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// fastAPI imitates /dyn/api/fast_download.json for a file stored in some
// number of collections. answer decides each in-range path and server.
type fastAPI struct {
	collections int
	answer      func(path, domain int) (status int, body string)
	files       map[string][]byte

	mu    sync.Mutex
	calls []string
}

func (api *fastAPI) client(t *testing.T) *Client {
	return NewClient(Config{
		BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(api.roundTrip)},
	})
}

func (api *fastAPI) roundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host == "files.example" {
		if body, ok := api.files[req.URL.Path]; ok {
			return testDownloadResponse(req, http.StatusOK, "application/pdf", string(body)), nil
		}
		return testDownloadResponse(req, http.StatusNotFound, "text/plain", "missing"), nil
	}
	if req.URL.Path != "/dyn/api/fast_download.json" {
		return nil, fmt.Errorf("unexpected request %s", req.URL)
	}
	query := req.URL.Query()
	var path, domain int
	fmt.Sscan(query.Get("path_index"), &path)
	fmt.Sscan(query.Get("domain_index"), &domain)
	api.mu.Lock()
	api.calls = append(api.calls, fmt.Sprintf("%d/%d", path, domain))
	api.mu.Unlock()
	if path >= api.collections || domain >= len(domainIndexes) {
		return testDownloadResponse(req, http.StatusBadRequest, "text/json", `{"download_url":null,"error":"Invalid domain_index or path_index"}`), nil
	}
	status, body := api.answer(path, domain)
	return testDownloadResponse(req, status, "text/json", body), nil
}

func grantJSON(url string, left int, recent ...string) string {
	payload, _ := json.Marshal(map[string]any{
		"download_url": url,
		"account_fast_download_info": map[string]any{
			"downloads_left": left, "downloads_per_day": 50, "recently_downloaded_md5s": append([]string{}, recent...),
		},
	})
	return string(payload)
}

func TestDownloadTriesAnotherCollectionAfterFileFailures(t *testing.T) {
	t.Parallel()

	paper := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	api := &fastAPI{collections: 2, files: map[string][]byte{"/good.pdf": paper}}
	api.answer = func(path, domain int) (int, string) {
		if path == 0 {
			// The first API answer counts the download; later ones see it as recent.
			left := 10
			recent := []string{}
			if domain > 0 {
				left, recent = 9, []string{hash}
			}
			return http.StatusOK, grantJSON(fmt.Sprintf("https://files.example/missing-%d.pdf", domain), left, recent...)
		}
		return http.StatusOK, grantJSON("https://files.example/good.pdf", 9, hash)
	}
	result, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: hash, Title: "Paper", Format: "pdf"}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(api.calls, " "); got != "0/0 0/1 0/2 1/0" {
		t.Fatalf("API calls = %s", got)
	}
	if result.DownloadsLeft == nil || *result.DownloadsLeft != 9 {
		t.Fatalf("downloads left = %v, want 9: the file was already counted", result.DownloadsLeft)
	}
	if data, err := os.ReadFile(result.Path); err != nil || string(data) != string(paper) {
		t.Fatalf("saved file: %v", err)
	}
}

func TestDownloadStopsAfterTheFilesLastCollection(t *testing.T) {
	t.Parallel()

	api := &fastAPI{collections: 1}
	api.answer = func(path, domain int) (int, string) {
		return http.StatusOK, grantJSON(fmt.Sprintf("https://files.example/missing-%d.pdf", domain), 5)
	}
	_, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "X", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if got := strings.Join(api.calls, " "); got != "0/0 0/1 0/2 1/0" {
		t.Fatalf("API calls = %s", got)
	}
	// Running out of collections is not itself a failure worth reporting.
	if message := err.Error(); strings.Contains(message, "collection 2") || strings.Contains(message, "no fast download") || !strings.Contains(message, "fast server 3 (file)") {
		t.Fatalf("diagnostic = %v", err)
	}
	if apperr.CodeOf(err) != apperr.Upstream {
		t.Fatalf("code = %s", apperr.CodeOf(err))
	}
}

func TestDownloadTriesAtMostThreeCollections(t *testing.T) {
	t.Parallel()

	api := &fastAPI{collections: 10}
	api.answer = func(path, domain int) (int, string) {
		return http.StatusOK, grantJSON(fmt.Sprintf("https://files.example/missing-%d-%d.pdf", path, domain), 5)
	}
	_, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "X", Format: "pdf"}, time.Second, nil)
	if err == nil || len(api.calls) != maxDownloadPaths*len(domainIndexes) {
		t.Fatalf("calls = %v, err = %v", api.calls, err)
	}
	if !strings.Contains(err.Error(), "collection 3, fast server 3 (file)") || !strings.Contains(err.Error(), "fast server 1 (file)") {
		t.Fatalf("diagnostic = %v", err)
	}
}

func TestDownloadSkipsMissingServersWithinACollection(t *testing.T) {
	t.Parallel()

	paper := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	api := &fastAPI{collections: 2, files: map[string][]byte{"/good.pdf": paper}}
	api.answer = func(path, domain int) (int, string) {
		switch {
		case path == 0 && domain == 0:
			return http.StatusOK, grantJSON("https://files.example/missing.pdf", 3)
		case path == 0:
			// Anna has fewer servers in reserve mode than domainIndexes lists.
			return http.StatusBadRequest, `{"download_url":null,"error":"Invalid domain_index or path_index"}`
		}
		return http.StatusOK, grantJSON("https://files.example/good.pdf", 2, hash)
	}
	if _, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: hash, Title: "Paper", Format: "pdf"}, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(api.calls, " "); got != "0/0 0/1 1/0" {
		t.Fatalf("API calls = %s", got)
	}
}

func TestDownloadStopsOnAccountAndRecordErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status  int
		body    string
		code    string
		message string
	}{
		{http.StatusTooManyRequests, `{"download_url":null,"error":"No downloads left"}`, apperr.UpstreamBlocked, "no fast downloads left"},
		{http.StatusUnauthorized, `{"download_url":null,"error":"Invalid secret key"}`, apperr.Config, "ANNAS_SECRET_KEY"},
		{http.StatusForbidden, `{"download_url":null,"error":"Not a member"}`, apperr.UpstreamBlocked, "active membership"},
		{http.StatusNotFound, `{"download_url":null,"error":"Record not found"}`, apperr.NotFound, "no file with this hash"},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			t.Parallel()
			api := &fastAPI{collections: 3}
			api.answer = func(int, int) (int, string) { return tc.status, tc.body }
			_, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "X", Format: "pdf"}, time.Second, nil)
			if len(api.calls) != 1 {
				t.Fatalf("API calls = %v; every other server would give the same answer", api.calls)
			}
			if apperr.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.message) || strings.Contains(err.Error(), "test-secret") {
				t.Fatalf("err = %v, want %s containing %q", err, tc.code, tc.message)
			}
		})
	}
}

func TestDownloadRetriesServerErrorsWithoutMovingCollection(t *testing.T) {
	t.Parallel()

	api := &fastAPI{collections: 3}
	api.answer = func(int, int) (int, string) {
		return http.StatusInternalServerError, `{"download_url":null,"error":"Error during fetching"}`
	}
	_, err := api.client(t).DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "X", Format: "pdf"}, time.Second, nil)
	// The API answers for every collection, so a failing API is retried per
	// server but not once more per collection.
	if err == nil || strings.Join(api.calls, " ") != "0/0 0/1 0/2" || !strings.Contains(err.Error(), "Error during fetching") {
		t.Fatalf("calls = %v, err = %v", api.calls, err)
	}
}

func TestDOIDownloadFallsBackToSciDBWhenFastDownloadsRunOut(t *testing.T) {
	t.Parallel()

	paper := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	apiCalls := 0
	client := NewClient(Config{
		BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/dyn/api/fast_download.json":
				apiCalls++
				return testDownloadResponse(req, http.StatusTooManyRequests, "text/json", `{"download_url":null,"error":"No downloads left"}`), nil
			case "/scidb/10.1000/example/":
				return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
			case "/md5/" + hash:
				return fixtureResponse(t, req, http.StatusOK, sciDBRecordPage, "text/html", nil), nil
			case "/scidb/paper.pdf":
				return testDownloadResponse(req, http.StatusOK, "application/pdf", string(paper)), nil
			}
			return nil, fmt.Errorf("unexpected request %s", req.URL)
		})},
	})
	result, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if apiCalls != 1 || result.DownloadsLeft != nil || result.Bytes != int64(len(paper)) {
		t.Fatalf("api calls = %d, result = %+v", apiCalls, result)
	}
}

func TestDownloadsLeftAfterCountsOnlyNewFiles(t *testing.T) {
	t.Parallel()

	if left := downloadsLeftAfter(nil, "a"); left != nil {
		t.Fatal("unknown allowance must stay unknown")
	}
	if left := downloadsLeftAfter(&parse.FastDownloadAccount{DownloadsLeft: 5}, "a"); *left != 4 {
		t.Fatalf("new file: %d", *left)
	}
	if left := downloadsLeftAfter(&parse.FastDownloadAccount{DownloadsLeft: 5, RecentlyDownloadedMD5s: []string{"a"}}, "a"); *left != 5 {
		t.Fatalf("recent file: %d", *left)
	}
	if left := downloadsLeftAfter(&parse.FastDownloadAccount{DownloadsLeft: 0}, "a"); *left != 0 {
		t.Fatalf("never negative: %d", *left)
	}
}
