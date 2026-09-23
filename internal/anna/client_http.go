package anna

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func (c *Client) fetchDocument(ctx context.Context, timeout time.Duration, rawURL string) (*goquery.Document, *url.URL, error) {
	response, err := c.doGet(ctx, c.requestClient(timeout), rawURL)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	var finalURL *url.URL
	if response.Request != nil {
		finalURL = response.Request.URL
	}
	if response.StatusCode == http.StatusForbidden {
		return nil, finalURL, apperr.New(apperr.UpstreamBlocked, "Anna's Archive returned 403; check the mirror and refresh ANNAS_ACCOUNT_COOKIE if the browser session has expired")
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, finalURL, apperr.New(apperr.NotFound, "archive record was not found")
	}
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return nil, finalURL, fmt.Errorf("request failed with status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return nil, finalURL, err
	}
	if len(body) > 8<<20 {
		return nil, finalURL, apperr.New(apperr.Upstream, "archive response exceeded the HTML size limit")
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
	if err != nil {
		return nil, finalURL, fmt.Errorf("failed to parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func (c *Client) doGet(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if request.URL.Scheme != "https" || request.URL.Host == "" || request.URL.User != nil {
		return nil, apperr.New(apperr.Upstream, "upstream URL must be HTTPS without credentials")
	}
	request.Header.Set("User-Agent", BrowserUserAgent)
	c.baseMu.Lock()
	base := c.baseURL
	c.baseMu.Unlock()
	baseURL, _ := url.Parse(base)
	if sameOrigin(request.URL, baseURL) && c.config.AccountCookie != "" {
		request.Header.Set("Cookie", c.config.AccountCookie)
	}
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() == nil {
			c.invalidateMirror(rawURL)
		}
		return nil, redactErr(err, c.config.SecretKey)
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode >= 500 {
		c.invalidateMirror(rawURL)
	}
	return response, nil
}

func (c *Client) requestClient(timeout time.Duration) *http.Client {
	client := *c.http
	// A cookie jar must not bypass the explicit credential-origin policy.
	client.Jar = nil
	if timeout > 0 {
		client.Timeout = timeout
	}
	originalRedirect := c.http.CheckRedirect
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("stopped after 10 redirects")
		}
		if request.URL.Scheme != "https" || request.URL.User != nil {
			return errors.New("refused an unsafe upstream redirect")
		}
		if len(via) > 0 && !sameOrigin(request.URL, via[0].URL) {
			request.Header.Del("Cookie")
			if via[0].URL.Query().Has("key") {
				return errors.New("refused to redirect an authenticated API request to another origin")
			}
		}
		if originalRedirect != nil {
			return originalRedirect(request, via)
		}
		return nil
	}
	return &client
}

func isArchiveDocument(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	title := strings.ToLower(doc.Find("title").First().Text())
	return strings.Contains(title, "anna's archive") || strings.Contains(title, "anna’s archive") || doc.Find("form[action='/search'] input[name='q']").Length() > 0
}
