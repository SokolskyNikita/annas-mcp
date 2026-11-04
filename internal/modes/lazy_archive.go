package modes

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

// lazyArchive defers archive-only settings and client construction until the
// first archive operation. OpenAlex and protocol setup have no archive dependency.
type lazyArchive struct {
	config *env.Env
	once   sync.Once
	client *anna.Client
	err    error
}

func (a *lazyArchive) get() (*anna.Client, error) {
	a.once.Do(func() {
		if a.config.ArchiveError != nil {
			a.err = a.config.ArchiveError
			return
		}
		a.client, a.err = anna.NewClient(anna.Config{
			BaseURL: a.config.AnnasBaseURL, AutoBaseURL: a.config.AutoBaseURL,
			SecretKey: a.config.SecretKey, DownloadPath: a.config.DownloadPath,
			AccountCookie: a.config.AccountCookie, SessionFile: a.config.SessionFile,
			Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
		})
	})
	return a.client, a.err
}

func (a *lazyArchive) FindBook(ctx context.Context, query string, options anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.Book], error) {
	c, err := a.get()
	if err != nil {
		return anna.SearchPage[*anna.Book]{}, err
	}
	return c.FindBook(ctx, query, options, timeout)
}

func (a *lazyArchive) FindArticle(ctx context.Context, query string, options anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.Paper], error) {
	c, err := a.get()
	if err != nil {
		return anna.SearchPage[*anna.Paper]{}, err
	}
	return c.FindArticle(ctx, query, options, timeout)
}

func (a *lazyArchive) FindMetadata(ctx context.Context, query string, options anna.SearchOptions, timeout time.Duration) (anna.SearchPage[*anna.MetadataRecord], error) {
	c, err := a.get()
	if err != nil {
		return anna.SearchPage[*anna.MetadataRecord]{}, err
	}
	return c.FindMetadata(ctx, query, options, timeout)
}

func (a *lazyArchive) LookupDOI(ctx context.Context, doi string, timeout time.Duration) (*anna.Paper, error) {
	c, err := a.get()
	if err != nil {
		return nil, err
	}
	return c.LookupDOI(ctx, doi, timeout)
}

func (a *lazyArchive) FileStats(ctx context.Context, hashes []string) (map[string]anna.RecordStats, error) {
	c, err := a.get()
	if err != nil {
		return nil, err
	}
	return c.FileStats(ctx, hashes)
}

func (a *lazyArchive) Details(ctx context.Context, id string, timeout time.Duration) (*anna.Details, error) {
	c, err := a.get()
	if err != nil {
		return nil, err
	}
	return c.Details(ctx, id, timeout)
}

func (a *lazyArchive) AccountStatus(ctx context.Context, timeout time.Duration) (*anna.AccountStatus, error) {
	c, err := a.get()
	if err != nil {
		return nil, err
	}
	return c.AccountStatus(ctx, timeout)
}

func (a *lazyArchive) DownloadBook(ctx context.Context, book *anna.Book, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	c, err := a.get()
	if err != nil {
		return anna.DownloadResult{}, err
	}
	return c.DownloadBook(ctx, book, timeout, progress)
}

func (a *lazyArchive) DownloadArticle(ctx context.Context, options anna.ArticleDownloadOptions, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	c, err := a.get()
	if err != nil {
		return anna.DownloadResult{}, err
	}
	return c.DownloadArticle(ctx, options, timeout, progress)
}
