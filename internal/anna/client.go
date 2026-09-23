package anna

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// BaseURLResolver resolves an Anna mirror. It is deliberately a small
// function type so callers can wrap their mirror resolver and tests can use a
// deterministic result without reading process environment state.
type BaseURLResolver func(context.Context) (string, error)

// Config contains all state needed by a Client. AccountCookie may be either
// the complete Cookie header value or the raw aa_account_id2 value.
type Config struct {
	BaseURL       string
	SecretKey     string
	DownloadPath  string
	AccountCookie string
	AutoBaseURL   bool
	HTTPClient    *http.Client
	Resolver      BaseURLResolver
}

// ArticleDownloadOptions describes an article download request. DOI and Hash
// are mutually exclusive at the adapter boundary; DownloadArticle also
// accepts either one for callers using the package directly.
type ArticleDownloadOptions struct {
	DOI    string
	Hash   string
	Title  string
	Format string
}

// Client performs Anna Archive searches and downloads with injected
// configuration and HTTP transport.
type Client struct {
	config      Config
	http        *http.Client
	configErr   error
	baseMu      sync.Mutex
	baseURL     string
	baseExpires time.Time
	resolving   chan struct{}
}

const defaultClientBaseURL = "https://annas-archive.gl"
const mirrorCacheTTL = 10 * time.Minute
const mirrorFailureTTL = 30 * time.Second

func NewClient(config Config) *Client {
	transport := config.HTTPClient
	if transport == nil {
		transport = &http.Client{}
	}
	c := &Client{config: config, http: transport, baseURL: defaultClientBaseURL}
	if config.BaseURL != "" {
		host, err := mirror.ParseBaseURL(config.BaseURL)
		if err != nil {
			c.configErr = apperr.Wrap(apperr.Config, "invalid archive base URL", err)
		} else {
			c.baseURL = "https://" + host
		}
	}
	c.config.BaseURL = c.baseURL
	c.config.AccountCookie = normalizeCookieHeader(config.AccountCookie)
	if c.config.AutoBaseURL && c.config.Resolver == nil {
		resolver := mirror.NewResolver(c.requestClient(5*time.Second), mirror.DefaultStatusPageURL, nil)
		resolver.SetAccountCookie(c.config.AccountCookie)
		c.config.Resolver = func(ctx context.Context) (string, error) { return resolver.Resolve(ctx, mirror.ResolveOptions{}) }
	}
	return c
}

func (c *Client) FindBook(ctx context.Context, query string, options SearchOptions, timeout time.Duration) ([]*Book, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	books, _, err := c.search(ctx, query, options, "")
	return books, err
}

func (c *Client) FindArticle(ctx context.Context, query string, options SearchOptions, timeout time.Duration) ([]*Paper, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	books, _, err := c.search(ctx, query, options, "journals")
	if err != nil {
		return nil, err
	}
	papers := make([]*Paper, 0, len(books))
	for _, book := range books {
		papers = append(papers, paperFromBook(book, book.DOI))
	}
	return papers, nil
}

func (c *Client) LookupDOI(ctx context.Context, doi string, timeout time.Duration) (*Paper, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	return c.lookupDOI(ctx, doi)
}

func (c *Client) DownloadBook(ctx context.Context, book *Book, timeout time.Duration, progress ProgressFunc) (DownloadResult, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	return c.downloadBook(ctx, book, progress)
}

func (c *Client) DownloadArticle(ctx context.Context, options ArticleDownloadOptions, timeout time.Duration, progress ProgressFunc) (DownloadResult, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	return c.downloadArticle(ctx, options, progress)
}

func (c *Client) baseURLFor(ctx context.Context) (string, error) {
	if c.configErr != nil {
		return "", c.configErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		c.baseMu.Lock()
		if !c.config.AutoBaseURL || time.Now().Before(c.baseExpires) {
			base := c.baseURL
			c.baseMu.Unlock()
			return base, nil
		}
		if pending := c.resolving; pending != nil {
			c.baseMu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		c.resolving = pending
		fallback := c.config.BaseURL
		c.baseMu.Unlock()
		resolveCtx, cancel := context.WithTimeout(ctx, mirror.DefaultResolveTimeout)
		resolved, err := c.config.Resolver(resolveCtx)
		cancel()
		if err == nil {
			var host string
			host, err = mirror.ParseCandidateURL(resolved)
			if err == nil {
				resolved = "https://" + host
			}
		}
		c.baseMu.Lock()
		if ctx.Err() == nil {
			if err == nil {
				c.baseURL = resolved
				c.baseExpires = time.Now().Add(mirrorCacheTTL)
			} else {
				c.baseURL = fallback
				c.baseExpires = time.Now().Add(mirrorFailureTTL)
			}
		}
		base := c.baseURL
		c.resolving = nil
		close(pending)
		c.baseMu.Unlock()
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return base, nil
	}
}

// Failed archive requests invalidate only the matching selection; another
// concurrent request may already have moved the client to a healthy mirror.
func (c *Client) invalidateMirror(rawURL string) {
	if !c.config.AutoBaseURL {
		return
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return
	}
	c.baseMu.Lock()
	defer c.baseMu.Unlock()
	current, _ := url.Parse(c.baseURL)
	if sameOrigin(target, current) {
		c.baseExpires = time.Time{}
	}
}

func normalizeCookieHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return ""
	}
	for _, part := range strings.Split(raw, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && key == "aa_account_id2" {
			return "aa_account_id2=" + strings.TrimSpace(value)
		}
	}
	if strings.Contains(raw, ";") {
		return ""
	}
	return "aa_account_id2=" + raw
}

func operationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}
