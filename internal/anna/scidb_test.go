package anna

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestSciDBViewerExtraction(t *testing.T) {
	t.Parallel()
	const page = "https://annas.test/scidb/10.1000/example/"
	const hash = "0123456789abcdef0123456789abcdef"
	const file = "https://downloads.example/paper%20name.pdf?token=a%2Bb&part=1"
	frame := `<iframe src="/pdfjs/web/viewer.html?file=` + url.QueryEscape(file) + `"></iframe>`
	link := `<a href="/md5/` + hash + `">Archive record</a>`
	for _, test := range []struct {
		name, html string
		valid      bool
	}{
		{"current viewer", frame + link, true},
		{"duplicate links", frame + frame + link + link, true},
		{"unrelated PDF anchor", `<a href="` + file + `">PDF</a>` + link, false},
		{"foreign viewer", strings.Replace(frame, `src="/pdfjs`, `src="https://evil.example/pdfjs`, 1) + link, false},
		{"HTTP PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("http://downloads.example/paper.pdf"), 1) + link, false},
		{"credential-bearing PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("https://secret@downloads.example/paper.pdf"), 1) + link, false},
		{"protocol relative foreign PDF", strings.Replace(frame, url.QueryEscape(file), url.QueryEscape("//downloads.example/paper.pdf"), 1) + link, false},
		{"ambiguous PDFs", frame + strings.Replace(frame, "part%3D1", "part%3D2", 1) + link, false},
		{"missing checksum", frame, false},
		{"invalid checksum", frame + `<a href="/md5/not-a-hash">Record</a>`, false},
		{"ambiguous checksums", frame + link + strings.ReplaceAll(link, hash, "abcdef0123456789abcdef0123456789"), false},
		{"missing PDF", link, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(test.html))
			if err != nil {
				t.Fatal(err)
			}
			got, md5, err := parseSciDBFile(doc, page)
			if !test.valid {
				if err == nil {
					t.Fatalf("accepted invalid SciDB page: %q, %q", got, md5)
				}
				return
			}
			if err != nil || got != file || md5 != hash {
				t.Fatalf("got %q, %q, %v; want exactly one query decode", got, md5, err)
			}
		})
	}
}

func TestSciDBDOIURLPreservesSuffix(t *testing.T) {
	t.Parallel()
	const doi = "10.1000/example(02)?section#part"
	page, err := url.Parse(buildSciDBURL("https://annas.test", doi))
	if err != nil || page.Path != "/scidb/"+doi || page.RawQuery != "" || page.Fragment != "" {
		t.Fatalf("DOI suffix was interpreted as URL metadata: %v, %v", page, err)
	}
	if !isSciDBDOIPage(page, "https://annas.test", doi) {
		t.Fatal("canonical DOI URL did not match")
	}
	page.Path += "/"
	if !isSciDBDOIPage(page, "https://annas.test", doi) {
		t.Fatal("trailing slash redirect did not match")
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
					if req.URL.Path != "/scidb/10.1000/example" || req.Header.Get("Cookie") != "aa_account_id2=test" {
						t.Fatal("SciDB request used wrong path or lacked its account cookie")
					}
					return fixtureResponse(t, req, http.StatusOK, "scidb_result.html", "text/html", map[string]string{"{{HASH}}": hash}), nil
				}
				if req.URL.Host != "downloads.example" || req.Header.Get("Cookie") != "" || req.URL.Query().Has("key") {
					t.Fatal("SciDB file request escaped credential policy")
				}
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/octet-stream", nil), nil
			})}})
			selectedHash := hash
			if mismatch {
				selectedHash = "0123456789abcdef0123456789abcdef"
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
		return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "application/octet-stream", nil), nil
	})}})
	if _, err := client.getSciDBPDF(context.Background(), client.requestClient(0), "https://downloads.example/paper.pdf"); err == nil {
		t.Fatal("HTML from the PDF CDN was accepted")
	}
}
