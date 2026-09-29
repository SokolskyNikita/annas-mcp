package anna

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
)

// Opt-in integration check: exercises real SciDB HTML and PDF bytes while
// deliberately disabling the fast API so that a working API cannot mask a
// broken fallback. Uses the account cookie on the official mirror only.
func TestLiveSciDBFallback(t *testing.T) {
	if os.Getenv("ANNAS_MCP_TEST_SCIDB_LIVE") != "1" {
		t.Skip("set ANNAS_MCP_TEST_SCIDB_LIVE=1 to test the real SciDB fallback")
	}
	cookie := os.Getenv("ANNAS_ACCOUNT_COOKIE")
	if cookie == "" {
		values, err := godotenv.Read("../../.env")
		if err != nil {
			t.Fatal("provide ANNAS_ACCOUNT_COOKIE or a repository .env for the live check")
		}
		cookie = values["ANNAS_ACCOUNT_COOKIE"]
	}
	if cookie == "" {
		t.Fatal("ANNAS_ACCOUNT_COOKIE is required for the live check")
	}
	apiCalls, pdfCalls := 0, 0
	client := NewClient(Config{
		BaseURL: "https://annas-archive.gl", AccountCookie: cookie,
		SecretKey: "disabled-for-live-fallback-test", DownloadPath: t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/dyn/api/fast_download.json" {
				apiCalls++
				return &http.Response{StatusCode: http.StatusBadRequest, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":"fast API disabled for SciDB integration check"}`)), Request: req}, nil
			}
			if req.URL.Host != "annas-archive.gl" {
				pdfCalls++
				if req.Header.Get("Cookie") != "" || req.URL.Query().Has("key") {
					t.Fatal("archive credentials were attached to an external request")
				}
			}
			return http.DefaultTransport.RoundTrip(req)
		})},
	})
	result, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1038/nature12373", Title: "SciDB live check"}, 90*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hasher := md5.New()
	n, err := io.Copy(hasher, file)
	if err != nil || n != result.Bytes || hex.EncodeToString(hasher.Sum(nil)) != "d89c394b00116f093b5d9d6a6611f975" {
		t.Fatalf("SciDB file failed independent size/checksum verification: bytes=%d, err=%v", n, err)
	}
	if apiCalls != len(domainIndexes) || pdfCalls == 0 || filepath.Ext(result.Path) != ".pdf" {
		t.Fatalf("fallback was not exercised: api=%d pdf=%d", apiCalls, pdfCalls)
	}
	t.Logf("SciDB fallback verified: %d bytes, MD5 d89c394b00116f093b5d9d6a6611f975; fast API disabled", n)
}
