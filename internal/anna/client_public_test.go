package anna

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestFindArticleReturnsMappedPapers(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/search" || req.URL.Query().Get("index") != "journals" {
				return nil, errors.New("unexpected article search URL: " + req.URL.String())
			}
			return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
		})},
	})

	papers, err := client.FindArticle(context.Background(), "attention", SearchOptions{}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(papers) != 1 {
		t.Fatalf("expected one paper, got %d", len(papers))
	}
	if papers[0].Title != "Attention Is All You Need" || papers[0].DOI != "10.48550/arXiv.1706.03762" {
		t.Fatalf("unexpected paper: %+v", papers[0])
	}
}

func TestLookupDOIReadsSciDBAndDetailPages(t *testing.T) {
	t.Parallel()

	hash := fixtureHash(t, "paper.pdf")
	client := NewClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/scidb/10.1000/example":
				return fixtureResponse(t, req, http.StatusOK, "scidb_result.html", "text/html", map[string]string{"{{HASH}}": hash}), nil
			case "/md5/" + hash:
				return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", nil), nil
			default:
				return nil, errors.New("unexpected DOI lookup URL: " + req.URL.String())
			}
		})},
	})

	paper, err := client.LookupDOI(context.Background(), "https://doi.org/10.1000/example", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if paper.DOI != "10.1000/example" || paper.Hash != hash || paper.Title != "Attention Is All You Need" {
		t.Fatalf("unexpected DOI result: %+v", paper)
	}
	if paper.Authors != "Ashish Vaswani" || paper.Journal != "NeurIPS" || paper.Size != "2.0MB" {
		t.Fatalf("detail page metadata was not parsed: %+v", paper)
	}
}

func TestLookupDOIFallsBackToArchiveSearchAfterSciDBMiss(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/scidb/10.48550/arXiv.1706.03762" {
				return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Path == "/search" {
				return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
			}
			return nil, errors.New("unexpected DOI fallback URL: " + req.URL.String())
		})},
	})

	paper, err := client.LookupDOI(context.Background(), "10.48550/arXiv.1706.03762", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if paper.Hash != "0123456789abcdef0123456789abcdef" || paper.DOI != "10.48550/arXiv.1706.03762" {
		t.Fatalf("archive search fallback returned %+v", paper)
	}
}

func TestLookupDOIFallsBackThroughCitationTitle(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "doi.org" {
				return fixtureResponse(t, req, http.StatusOK, "doi_metadata.json", "application/json", nil), nil
			}
			if req.URL.Path == "/scidb/10.1000/example" {
				return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Path == "/search" {
				if req.URL.Query().Get("q") == "Attention Is All You Need" {
					return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
				}
				return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
			}
			return nil, errors.New("unexpected DOI title fallback URL: " + req.URL.String())
		})},
	})

	paper, err := client.LookupDOI(context.Background(), "10.1000/example", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if paper.Title != "Attention Is All You Need" || paper.Hash != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("citation title fallback returned %+v", paper)
	}
}

func TestDownloadBookUsesFastDownloadAndVerifiesFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	hash := fixtureHash(t, "paper.pdf")
	client := NewClient(Config{
		BaseURL:      "annas.test",
		SecretKey:    "test-secret",
		DownloadPath: directory,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "annas.test":
				if req.URL.Path != "/dyn/api/fast_download.json" {
					return nil, errors.New("unexpected archive URL: " + req.URL.String())
				}
				return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
			case "downloads.example":
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
			default:
				return nil, errors.New("unexpected download host: " + req.URL.Host)
			}
		})},
	})

	result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Downloaded paper", Format: "pdf"}, time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.Path) != "Downloaded paper.pdf" || result.Bytes != int64(len(readFixture(t, "paper.pdf"))) {
		t.Fatalf("unexpected download result: %+v", result)
	}
}

func TestPublicMethodsHonorCanceledContextBeforeNetwork(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network should not be reached")
		})},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FindBook(ctx, "example", SearchOptions{}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindBook returned %v", err)
	}
	if _, err := client.FindArticle(ctx, "example", SearchOptions{}, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindArticle returned %v", err)
	}
	if _, err := client.LookupDOI(ctx, "10.1000/example", time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupDOI returned %v", err)
	}
}
