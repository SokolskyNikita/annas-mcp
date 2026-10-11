package anna

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// Config contains all state needed by a Client. AccountCookie may be either
// the complete Cookie header value or the raw aa_account_id2 value.
// SessionFile keeps the signed-in session between runs, and mirror health
// beside it; empty disables both.
// With AutoBaseURL, BaseURL is the preferred mirror and the official mirrors
// stand in for it; without it, BaseURL is the only mirror.
type Config struct {
	BaseURL       string
	SecretKey     string
	DownloadPath  string
	AccountCookie string
	SessionFile   string
	AutoBaseURL   bool
	HTTPClient    *http.Client
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
	config    Config
	http      *http.Client
	configErr error
	// baseURL is the configured mirror's origin.
	baseURL string
	// mirrors and health choose the mirror when AutoBaseURL is set.
	mirrors   *mirror.Selector
	health    *mirror.Health
	sessionMu sync.Mutex
	cookie    string
	// configuredCookie is ANNAS_ACCOUNT_COOKIE, kept to recognize it later.
	configuredCookie string
	signingIn        chan struct{}
	signedInAt       time.Time
	signInErr        error
	// downloads makes concurrent requests for the same hash reuse one file.
	downloadMu sync.Mutex
	downloads  map[string]chan struct{}
	// fileStatsSlots bounds optional public-statistics requests across calls.
	fileStatsSlots   chan struct{}
	fileStatsInitMu  sync.Mutex
	fileStatsCache   statsCache
	fileStatsTimeout time.Duration
	// Limits on how long one request may wait; tests shorten them.
	headerTimeout time.Duration
	idleTimeout   time.Duration
	doiTimeout    time.Duration
	signInTimeout time.Duration
}

const defaultClientBaseURL = "https://annas-archive.gl"

// mirrorStateFile sits next to the session file and shares mirror health
// with other runs.
const mirrorStateFile = "mirrors.json"

func NewClient(config Config) *Client {
	transport := config.HTTPClient
	if transport == nil {
		transport = &http.Client{Transport: newTransport()}
	}
	c := &Client{
		config: config, http: transport, baseURL: defaultClientBaseURL,
		headerTimeout: responseHeaderTimeout, idleTimeout: bodyIdleTimeout, doiTimeout: doiMetadataTimeout,
		signInTimeout: signInTimeout, fileStatsSlots: make(chan struct{}, maxConcurrentFileStats),
		fileStatsCache: newStatsCache(), fileStatsTimeout: fileStatsBudget,
	}
	if config.BaseURL != "" {
		host, err := mirror.ParseBaseURL(config.BaseURL)
		if err != nil {
			c.configErr = apperr.Wrap(apperr.Config, "invalid archive base URL", err)
		} else {
			c.baseURL = "https://" + host
		}
	}
	c.config.BaseURL = c.baseURL
	c.configuredCookie = normalizeCookieHeader(config.AccountCookie)
	c.cookie = c.configuredCookie
	c.restoreSession()
	if c.config.AutoBaseURL && c.configErr == nil {
		c.health = mirror.NewHealth()
		if config.SessionFile != "" {
			mirror.NewStore(filepath.Join(filepath.Dir(config.SessionFile), mirrorStateFile), c.health)
		}
		prober := mirror.NewProber(c.requestClient(0), c.sessionCookie)
		c.mirrors = mirror.NewSelector(mirror.Candidates(strings.TrimPrefix(c.baseURL, "https://")), c.health, prober.Check)
	}
	return c
}

// requireArchiveAccess keeps credential failures deterministic and local.
// Every archive operation needs a session: either a configured cookie or the
// secret key to sign in with. The upstream service remains responsible for
// deciding whether the session is still valid or has sufficient access.
func (c *Client) requireArchiveAccess(ctx context.Context) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if c.sessionCookie() == "" && strings.TrimSpace(c.config.SecretKey) == "" {
		return apperr.New(apperr.Config, "ANNAS_SECRET_KEY is required for archive access: copy the secret key from your Anna's Archive account page")
	}
	return nil
}

// FindBook returns one page of book search results.
func (c *Client) FindBook(ctx context.Context, query string, options SearchOptions, timeout time.Duration) (SearchPage[*Book], error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	results, err := c.search(ctx, query, options, "")
	if err != nil {
		return SearchPage[*Book]{}, err
	}
	return filePage(results, func(book *Book) *Book { return book }), nil
}

// FindArticle returns one page of journal article search results.
func (c *Client) FindArticle(ctx context.Context, query string, options SearchOptions, timeout time.Duration) (SearchPage[*Paper], error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	results, err := c.search(ctx, query, options, "journals")
	if err != nil {
		return SearchPage[*Paper]{}, err
	}
	return filePage(results, func(book *Book) *Paper { return paperFromBook(book, book.DOI) }), nil
}

// FindMetadata searches catalog records, which describe books without
// holding a file, such as Open Library, WorldCat and ISBNdb entries.
func (c *Client) FindMetadata(ctx context.Context, query string, options SearchOptions, timeout time.Duration) (SearchPage[*MetadataRecord], error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	options.Index = "meta"
	results, err := c.search(ctx, query, options, "")
	if err != nil {
		return SearchPage[*MetadataRecord]{}, err
	}
	return metadataPage(results), nil
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

func normalizeCookieHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "\r\n") {
		return ""
	}
	for _, part := range strings.Split(raw, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && key == accountCookieName {
			return accountCookieName + "=" + strings.TrimSpace(value)
		}
	}
	if strings.Contains(raw, ";") {
		return ""
	}
	return accountCookieName + "=" + raw
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
