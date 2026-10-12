package anna

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindArticleReturnsMappedPapers(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path != "/search" || req.URL.Query().Get("index") != "journals" {
				return nil, errors.New("unexpected article search URL: " + req.URL.String())
			}
			return fixtureResponse(t, req, http.StatusOK, searchAttentionPage, "text/html", nil), nil
		})},
	})

	page, err := client.FindArticle(context.Background(), "attention", SearchOptions{}, callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	papers := page.Results
	if len(papers) != 9 {
		t.Fatalf("expected nine papers, got %d", len(papers))
	}
	if papers[0].Title != attentionTitle || papers[0].Hash != attentionHash || papers[0].DOI != "" || papers[0].PageURL != "https://annas.test/md5/"+attentionHash {
		t.Fatalf("unexpected paper: %+v", papers[0])
	}
}

func TestLookupDOIReadsSciDBAndDetailPages(t *testing.T) {
	t.Parallel()

	hash := fixtureHash(t, "paper.pdf")
	client := mustClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Path {
			case "/scidb/10.1000/example/":
				return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
			case "/md5/" + hash:
				return fixtureResponse(t, req, http.StatusOK, sciDBRecordPage, "text/html", nil), nil
			default:
				return nil, errors.New("unexpected DOI lookup URL: " + req.URL.String())
			}
		})},
	})

	paper, err := client.LookupDOI(context.Background(), "https://doi.org/10.1000/example", callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if paper.DOI != "10.1000/example" || paper.Hash != hash || paper.Title != "Equity-premium prediction: attention is all you need." || paper.DownloadURL != sciDBFileURL {
		t.Fatalf("unexpected DOI result: %+v", paper)
	}
	if paper.Authors != "Luiz Renato Lima; Lucas Lúcio Godeiro" || paper.Journal != "Wiley, Journal of Applied Econometrics, 38, 2022" ||
		paper.Year != "2022" || paper.Size != "1.4MB" || !paper.FastDownload {
		t.Fatalf("detail page metadata was not parsed: %+v", paper)
	}
}

func TestLookupDOIFallsBackToArchiveSearchAfterSciDBMiss(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "doi.org" {
				return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Path == "/scidb/"+liangDOI+"/" {
				return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Path == "/search" {
				return htmlResponse(req, http.StatusOK, searchPageWith(t, capturedCard(t, searchJournalsPage, liangHash))), nil
			}
			// Without DOI.org, only the DOI in the record's identifiers
			// panel can verify it.
			if req.URL.Path == "/md5/"+liangHash {
				return fixtureResponse(t, req, http.StatusOK, recordLiangPage, "text/html", nil), nil
			}
			return nil, errors.New("unexpected DOI fallback URL: " + req.URL.String())
		})},
	})

	paper, err := client.LookupDOI(context.Background(), liangDOI, callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if paper.Hash != liangHash || paper.DOI != liangDOI {
		t.Fatalf("archive search fallback returned %+v", paper)
	}
}

func TestLookupDOIRequiresVerifiedCitationIdentity(t *testing.T) {
	t.Parallel()

	var cases struct {
		Comment string
		Cases   []struct {
			Name    string
			Replace map[string]string
			Match   bool
		}
	}
	loadCases(t, "citation_identity", &cases)
	for _, test := range cases.Cases {
		t.Run(test.Name, func(t *testing.T) {
			client := mustClient(Config{
				BaseURL:       "annas.test",
				AccountCookie: "aa_account_id2=test",
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Host == "doi.org" {
						if req.Header.Get("Cookie") != "" {
							t.Fatal("archive cookie sent to DOI metadata service")
						}
						return fixtureResponse(t, req, http.StatusOK, attentionCSL, "application/json", nil), nil
					}
					if req.URL.Path == "/scidb/"+strings.ToLower(attentionDOI)+"/" {
						return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
					}
					if req.URL.Path == "/search" {
						if req.URL.Query().Get("q") == attentionTitle {
							return htmlResponse(req, http.StatusOK, searchPageWith(t, capturedCard(t, searchAttentionPage, attentionHash))), nil
						}
						return fixtureResponse(t, req, http.StatusOK, noResultsPage, "text/html", nil), nil
					}
					if req.URL.Path == "/md5/"+attentionHash {
						return fixtureResponse(t, req, http.StatusOK, recordAttentionPage, "text/html", test.Replace), nil
					}
					return nil, errors.New("unexpected DOI title fallback URL: " + req.URL.String())
				})},
			})

			paper, err := client.LookupDOI(context.Background(), attentionDOI, callTimeout)
			if !test.Match {
				if paper != nil || CodeOf(err) != NotFound {
					t.Fatalf("unverified citation was accepted: paper=%+v err=%v", paper, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if paper.Title != attentionTitle || paper.Hash != attentionHash || paper.DOI != attentionDOI || paper.DownloadURL != "" {
				t.Fatalf("citation title fallback returned %+v", paper)
			}
		})
	}
}

func TestDownloadBookUsesFastDownloadAndVerifiesFile(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	hash := fixtureHash(t, "paper.pdf")
	client := mustClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  directory,
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

	result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Downloaded paper", Format: "pdf"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.Path) != "Downloaded paper.pdf" || result.Bytes != int64(len(readFixture(t, "paper.pdf"))) {
		t.Fatalf("unexpected download result: %+v", result)
	}
}

func TestPublicMethodsHonorCanceledContextBeforeNetwork(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{
		BaseURL: "annas.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network should not be reached")
		})},
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FindBook(ctx, "example", SearchOptions{}, callTimeout); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindBook returned %v", err)
	}
	if _, err := client.FindArticle(ctx, "example", SearchOptions{}, callTimeout); !errors.Is(err, context.Canceled) {
		t.Fatalf("FindArticle returned %v", err)
	}
	if _, err := client.LookupDOI(ctx, "10.1000/example", callTimeout); !errors.Is(err, context.Canceled) {
		t.Fatalf("LookupDOI returned %v", err)
	}
	if _, err := client.DownloadBook(ctx, &Book{Hash: "0123456789abcdef0123456789abcdef"}, callTimeout, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadBook returned %v", err)
	}
	if _, err := client.DownloadArticle(ctx, ArticleDownloadOptions{DOI: "10.1000/example"}, callTimeout, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadArticle returned %v", err)
	}
}
