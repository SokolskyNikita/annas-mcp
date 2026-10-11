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

func (c *Client) fetchDocument(ctx context.Context, rawURL string) (*goquery.Document, *url.URL, error) {
	response, err := c.doGet(ctx, c.requestClient(0), rawURL)
	if err != nil {
		return nil, nil, err
	}
	defer response.Body.Close()
	var finalURL *url.URL
	if response.Request != nil {
		finalURL = response.Request.URL
	}
	if response.StatusCode == http.StatusForbidden {
		return nil, finalURL, apperr.New(apperr.UpstreamBlocked, c.forbiddenMessage())
	}
	if response.StatusCode == http.StatusNotFound {
		return nil, finalURL, apperr.New(apperr.NotFound, "archive record was not found")
	}
	if response.StatusCode != http.StatusOK {
		return nil, finalURL, httpResponseError(response, rawURL)
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
	for renewed := false; ; renewed = true {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		if request.URL.Scheme != "https" || request.URL.Host == "" || request.URL.User != nil {
			return nil, apperr.New(apperr.Upstream, "upstream URL must be HTTPS without credentials")
		}
		request.Header.Set("User-Agent", BrowserUserAgent)
		_, archive := c.archiveHost(request.URL)
		cookie := ""
		if archive {
			cookie = c.sessionCookie()
		}
		if cookie != "" {
			request.Header.Set("Cookie", cookie)
		}
		response, err := c.send(client, request)
		if err != nil {
			// The caller's own deadline or cancellation says nothing about
			// the mirror; a refused connection or a server gone quiet does.
			if ctx.Err() == nil {
				c.mirrorFailed(rawURL, unreachable(err))
			}
			return nil, redactErr(err, c.config.SecretKey)
		}
		// An expired session ends in a 403 from DDoS-Guard. Sign in again
		// and repeat the request once before treating the mirror as bad.
		// The fast download API authenticates with its key parameter
		// instead, so its 403s say nothing about the session.
		if archive && !renewed && response.StatusCode == http.StatusForbidden && !request.URL.Query().Has("key") {
			retry, err := c.signIn(ctx, cookie)
			if err != nil || retry {
				response.Body.Close()
				if err != nil {
					return nil, err
				}
				continue
			}
		}
		if failure := responseFailure(request, response); failure != nil {
			c.mirrorFailed(rawURL, *failure)
		}
		return response, nil
	}
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
