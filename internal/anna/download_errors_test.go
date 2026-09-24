package anna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestDownloadFailureDiagnosticsIncludeAllFastAttempts(t *testing.T) {
	t.Parallel()

	const (
		configuredSecret = "private-config-key-739"
		querySecret      = "private-cdn-token-284"
		apiHTMLMarker    = "api-html-body-must-not-leak"
		fileHTMLMarker   = "file-html-body-must-not-leak"
	)
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     configuredSecret,
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "annas.test" && req.URL.Path == "/dyn/api/fast_download.json" {
				switch req.URL.Query().Get("domain_index") {
				case "0":
					return testDownloadResponse(req, http.StatusBadGateway, "text/html", "<html><body>"+apiHTMLMarker+" "+configuredSecret+"</body></html>"), nil
				case "1":
					return testDownloadURLResponse(req, "https://cdn-two.example/private/cook.pdf?token="+querySecret), nil
				case "2":
					return testDownloadURLResponse(req, "https://cdn-three.example/private/cook.pdf?token="+querySecret), nil
				}
			}
			switch req.URL.Host {
			case "cdn-two.example":
				return testDownloadResponse(req, http.StatusNotFound, "text/html", "<!doctype html><html><body>"+fileHTMLMarker+" "+querySecret+"</body></html>"), nil
			case "cdn-three.example":
				return testDownloadResponse(req, http.StatusServiceUnavailable, "text/html", "<!doctype html><html><body>"+fileHTMLMarker+" "+querySecret+"</body></html>"), nil
			default:
				return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
			}
		})},
	})

	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Cook review", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected download to fail")
	}
	message := strings.ToLower(err.Error())
	for _, want := range []string{
		"fast server 1 (api)", "fast server 2 (file)", "fast server 3 (file)",
		"502", "bad gateway", "annas.test",
		"404", "not found", "cdn-two.example",
		"503", "service unavailable", "cdn-three.example",
	} {
		if !strings.Contains(message, strings.ToLower(want)) {
			t.Errorf("diagnostic omitted %q: %v", want, err)
		}
	}
	for _, forbidden := range []string{
		configuredSecret, querySecret, apiHTMLMarker, fileHTMLMarker,
		"<!doctype html>", "/private/cook.pdf", "fast_download.json?",
	} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("diagnostic leaked %q: %v", forbidden, err)
		}
	}
}

func TestDOIDownloadFailureDiagnosticsIncludeFastAndSciDBAttempts(t *testing.T) {
	t.Parallel()

	const (
		doi              = "10.1000/example"
		configuredSecret = "private-doi-key-517"
		querySecret      = "private-scidb-token-983"
		bodyMarker       = "upstream-html-body-must-not-leak"
	)
	hash := fixtureHash(t, "paper.pdf")
	scidbPage := fmt.Sprintf(`<!doctype html><html><body>
<a href="/md5/%s">Download record</a>
<iframe src="/pdfjs/web/viewer.html?file=https%%3A%%2F%%2Fdownloads.example%%2Fscidb-paper.pdf%%3Ftoken%%3D%s"></iframe>
</body></html>`, hash, querySecret)

	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     configuredSecret,
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "annas.test" && req.URL.Path == "/scidb/"+doi {
				return testDownloadResponse(req, http.StatusOK, "text/html", scidbPage), nil
			}
			if req.URL.Host == "annas.test" && req.URL.Path == "/dyn/api/fast_download.json" {
				switch req.URL.Query().Get("domain_index") {
				case "0":
					return testDownloadResponse(req, http.StatusBadGateway, "text/html", "<html><body>"+bodyMarker+"</body></html>"), nil
				case "1":
					return testDownloadURLResponse(req, "https://cdn-two.example/private/cook.pdf?token="+querySecret), nil
				case "2":
					return testDownloadURLResponse(req, "https://cdn-three.example/private/cook.pdf?token="+querySecret), nil
				}
			}
			switch req.URL.Host {
			case "cdn-two.example":
				return testDownloadResponse(req, http.StatusNotFound, "text/html", "<html><body>"+bodyMarker+"</body></html>"), nil
			case "cdn-three.example":
				return testDownloadResponse(req, http.StatusServiceUnavailable, "text/html", "<html><body>"+bodyMarker+"</body></html>"), nil
			case "downloads.example":
				return testDownloadResponse(req, http.StatusNotFound, "text/html", "<html><body>"+bodyMarker+" "+querySecret+"</body></html>"), nil
			default:
				return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
			}
		})},
	})

	_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: doi, Title: "Cook review", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected DOI download to fail")
	}
	message := strings.ToLower(err.Error())
	for _, want := range []string{
		"fast server 1 (api)", "fast server 2 (file)", "fast server 3 (file)", "scidb",
		"502", "bad gateway", "annas.test",
		"404", "not found", "cdn-two.example", "downloads.example",
		"503", "service unavailable", "cdn-three.example",
	} {
		if !strings.Contains(message, strings.ToLower(want)) {
			t.Errorf("diagnostic omitted %q: %v", want, err)
		}
	}
	for _, forbidden := range []string{
		configuredSecret, querySecret, bodyMarker,
		"<!doctype html>", "/private/cook.pdf", "/scidb-paper.pdf", "fast_download.json?",
	} {
		if strings.Contains(err.Error(), forbidden) {
			t.Errorf("diagnostic leaked %q: %v", forbidden, err)
		}
	}
}

func TestDownloadFailureDiagnosticsRedactAPIErrorAndPreserveTransportCause(t *testing.T) {
	t.Parallel()

	const (
		configuredSecret = "api-payload-secret-104"
		bodyMarker       = "api-html-body-must-not-leak"
	)
	transportCause := errors.New("temporary transport failure")
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     configuredSecret,
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "annas.test" || req.URL.Path != "/dyn/api/fast_download.json" {
				return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
			}
			switch req.URL.Query().Get("domain_index") {
			case "0":
				return nil, transportCause
			case "1":
				return testDownloadResponse(req, http.StatusBadRequest, "application/json", `{"error":"rejected credential `+configuredSecret+`"}`), nil
			case "2":
				return testDownloadResponse(req, http.StatusServiceUnavailable, "text/html", "<html><body>"+bodyMarker+"</body></html>"), nil
			default:
				return nil, fmt.Errorf("unexpected domain index %q", req.URL.Query().Get("domain_index"))
			}
		})},
	})

	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Example", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected download to fail")
	}
	if !errors.Is(err, transportCause) {
		t.Errorf("aggregate did not preserve transport cause for errors.Is: %v", err)
	}
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		t.Errorf("aggregate did not preserve *url.Error for errors.As: %v", err)
	}
	if strings.Contains(err.Error(), configuredSecret) {
		t.Errorf("diagnostic leaked configured secret from API error payload: %v", err)
	}
	if strings.Contains(err.Error(), bodyMarker) || strings.Contains(err.Error(), "<html") {
		t.Errorf("diagnostic leaked upstream HTML: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rejected credential") {
		t.Errorf("diagnostic discarded useful API error text: %v", err)
	}
}

func TestDownloadErrorClassificationUsesTerminalAttemptAndKeepsEarly403(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "annas.test" && req.URL.Path == "/dyn/api/fast_download.json" {
				if req.URL.Query().Get("domain_index") == "0" {
					return testDownloadResponse(req, http.StatusForbidden, "text/plain", "access denied"), nil
				}
				return testDownloadURLResponse(req, "https://cdn.example/paper.pdf"), nil
			}
			if req.URL.Host == "cdn.example" {
				return testDownloadResponse(req, http.StatusNotFound, "text/plain", "missing"), nil
			}
			return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
		})},
	})

	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Example", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected download to fail")
	}
	if got := apperr.CodeOf(err); got != apperr.Upstream {
		t.Errorf("classification followed an earlier 403 instead of terminal 404: got %s, want %s (%v)", got, apperr.Upstream, err)
	}
	var upstreamErr *apperr.Error
	if !errors.As(err, &upstreamErr) || upstreamErr.Code != apperr.UpstreamBlocked {
		t.Errorf("aggregate did not preserve the earlier 403 classification cause: %v", err)
	}
}

func TestDownloadErrorClassificationUsesTerminalAttemptAndKeepsEarlyDNSTimeout(t *testing.T) {
	t.Parallel()

	dnsTimeout := &net.DNSError{Err: "temporary lookup timeout", Name: "annas.test", IsTimeout: true}
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "annas.test" && req.URL.Path == "/dyn/api/fast_download.json" {
				if req.URL.Query().Get("domain_index") == "0" {
					return nil, dnsTimeout
				}
				return testDownloadURLResponse(req, "https://cdn.example/paper.pdf"), nil
			}
			if req.URL.Host == "cdn.example" {
				return testDownloadResponse(req, http.StatusNotFound, "text/plain", "missing"), nil
			}
			return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
		})},
	})

	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Example", Format: "pdf"}, time.Second, nil)
	if err == nil {
		t.Fatal("expected download to fail")
	}
	if got := apperr.CodeOf(err); got != apperr.Upstream {
		t.Errorf("classification followed an earlier DNS timeout instead of terminal 404: got %s, want %s (%v)", got, apperr.Upstream, err)
	}
	if !errors.Is(err, dnsTimeout) {
		t.Errorf("aggregate did not preserve the earlier DNS timeout for errors.Is: %v", err)
	}
	var foundTimeout *net.DNSError
	if !errors.As(err, &foundTimeout) || foundTimeout != dnsTimeout {
		t.Errorf("aggregate did not preserve the earlier DNS timeout for errors.As: %v", err)
	}
}

func TestDownloadCancellationStopsRetriesAndRetainsEarlierAttempt(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	apiRequests := 0
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  t.TempDir(),
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "annas.test" || req.URL.Path != "/dyn/api/fast_download.json" {
				return nil, fmt.Errorf("unexpected request to %s%s", req.URL.Host, req.URL.Path)
			}
			apiRequests++
			switch req.URL.Query().Get("domain_index") {
			case "0":
				return testDownloadResponse(req, http.StatusBadGateway, "text/plain", "first attempt failed"), nil
			case "1":
				cancel()
				return nil, context.Canceled
			default:
				return nil, fmt.Errorf("unexpected retry at domain index %s", req.URL.Query().Get("domain_index"))
			}
		})},
	})

	_, err := client.downloadBook(ctx, &Book{Hash: "0123456789abcdef0123456789abcdef", Title: "Example", Format: "pdf"}, nil)
	if err == nil {
		t.Fatal("expected canceled download to fail")
	}
	if apiRequests != 2 {
		t.Errorf("cancellation should stop after the second API request; got %d requests", apiRequests)
	}
	if got := apperr.CodeOf(err); got != apperr.RequestTimeout {
		t.Errorf("cancellation classified as %s, want %s (%v)", got, apperr.RequestTimeout, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("aggregate did not preserve context.Canceled for errors.Is: %v", err)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fast server 1") || !strings.Contains(err.Error(), "502") {
		t.Errorf("cancellation diagnostic discarded the completed first attempt: %v", err)
	}
}

func testDownloadURLResponse(req *http.Request, downloadURL string) *http.Response {
	payload, _ := json.Marshal(fastDownloadResponse{DownloadURL: &downloadURL})
	return testDownloadResponse(req, http.StatusOK, "application/json", string(payload))
}

func testDownloadResponse(req *http.Request, status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode:    status,
		Header:        http.Header{"Content-Type": {contentType}},
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       req,
	}
}
