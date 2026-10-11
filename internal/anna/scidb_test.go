package anna

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

func TestSciDBDOIURLPreservesSuffix(t *testing.T) {
	t.Parallel()
	const doi = "10.1000/example(02)?section#part"
	page, err := url.Parse(buildSciDBURL("https://annas.test", doi))
	if err != nil || page.Path != "/scidb/"+strings.ToLower(doi)+"/" || page.RawQuery != "" || page.Fragment != "" {
		t.Fatalf("DOI suffix was interpreted as URL metadata: %v, %v", page, err)
	}
	if !isSciDBDOIPage(page, "https://annas.test", doi) {
		t.Fatal("canonical DOI URL did not match")
	}
	page.Path = strings.TrimSuffix(page.Path, "/")
	if !isSciDBDOIPage(page, "https://annas.test", doi) {
		t.Fatal("the page without its trailing slash did not match")
	}
	page.Host = "evil.example"
	if isSciDBDOIPage(page, "https://annas.test", doi) || isSciDBDOIPage(nil, "https://annas.test", doi) {
		t.Fatal("foreign/missing page accepted as the requested DOI")
	}
	page, _ = url.Parse("https://annas.test/scidb/10.1000/example")
	if isSciDBDOIPage(page, "https://annas.test", "10.1000/example/") {
		t.Fatal("redirect removed a significant DOI suffix slash")
	}
}

func TestSciDBFallbackResolvesViewerForSearchResult(t *testing.T) {
	t.Parallel()
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "matching hash", true: "different file"}[mismatch], func(t *testing.T) {
			hash := fixtureHash(t, "paper.pdf")
			requests := 0
			client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-key", DownloadPath: t.TempDir(), HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				if req.URL.Host == "annas.test" {
					if req.URL.Path != "/scidb/10.1000/example/" || req.Header.Get("Cookie") != "aa_account_id2=test" {
						t.Fatal("SciDB request used wrong path or lacked its account cookie")
					}
					return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
				}
				if req.URL.String() != sciDBFileURL || req.Header.Get("Cookie") != "" || req.URL.Query().Has("key") {
					t.Fatal("SciDB file request escaped credential policy")
				}
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/octet-stream", nil), nil
			})}})
			selectedHash := hash
			if mismatch {
				selectedHash = "0123456789abcdef0123456789abcdef"
			}
			ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
			defer cancel()
			paper := paperFromBook(&Book{Hash: selectedHash, Title: "Paper"}, "10.1000/example")
			result, err := client.downloadPaper(ctx, paper, "", nil)
			if mismatch {
				if err == nil || requests != 1 {
					t.Fatalf("downloaded a different SciDB file: requests=%d err=%v", requests, err)
				}
				return
			}
			if err != nil || filepath.Ext(result.Path) != ".pdf" || result.Bytes != int64(len(readFixture(t, "paper.pdf"))) {
				t.Fatalf("SciDB PDF fallback failed: %+v, %v", result, err)
			}
		})
	}
}

func TestSciDBRejectsHTMLInsteadOfPDF(t *testing.T) {
	t.Parallel()
	client := NewClient(Config{BaseURL: "annas.test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return fixtureResponse(t, req, http.StatusOK, noResultsPage, "application/octet-stream", nil), nil
	})}})
	if _, err := client.getSciDBPDF(context.Background(), client.requestClient(0), "https://downloads.example/paper.pdf", nil); err == nil {
		t.Fatal("HTML from the PDF CDN was accepted")
	}
}

func TestBuildSciDBURLPreservesBalancedDOISuffix(t *testing.T) {
	t.Parallel()

	input := "https://doi.org/10.1000/example(suffix)"
	doi := parse.NormalizeDOI(input)
	if doi != "10.1000/example(suffix)" {
		t.Fatalf("normalized DOI suffix = %q", doi)
	}
	page, err := url.Parse(buildSciDBURL("annas.test", doi))
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != "/scidb/10.1000/example(suffix)/" || page.RawQuery != "" || page.Fragment != "" {
		t.Fatalf("SciDB URL corrupted DOI suffix: %s", page.String())
	}
}
