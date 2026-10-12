package modes

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

const defaultSearchLimit = 10
const defaultSearchTimeout = 60 * time.Second
const defaultDownloadTimeout = 30 * time.Minute
const maxOperationTimeout = 24 * time.Hour

// maxFilterValues bounds each comma-separated filter.
const maxFilterValues = 20

// archiveClient makes transport adapters testable without a network or credentials.
type archiveClient interface {
	FindBook(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Book], error)
	FindArticle(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Paper], error)
	FindMetadata(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.MetadataRecord], error)
	LookupDOI(context.Context, string, time.Duration) (*anna.Paper, error)
	FileStats(context.Context, []string) (map[string]anna.RecordStats, error)
	Details(context.Context, string, time.Duration) (*anna.Details, error)
	AccountStatus(context.Context, time.Duration) (*anna.AccountStatus, error)
	DownloadBook(context.Context, *anna.Book, time.Duration, anna.ProgressFunc) (anna.DownloadResult, error)
	DownloadArticle(context.Context, anna.ArticleDownloadOptions, time.Duration, anna.ProgressFunc) (anna.DownloadResult, error)
}

type service struct{ archive archiveClient }

// searchResult is one page of search output. matched counts the exact hits
// parsed from this page and total all of them; partial_matches are looser
// hits Anna's Archive adds to a short first page, which may ignore filters.
type searchResult[T any] struct {
	Page              int    `json:"page"`
	Language          string `json:"language"`
	Content           string `json:"content,omitempty"`
	Index             string `json:"index,omitempty"`
	Total             int    `json:"total"`
	TotalIsLowerBound bool   `json:"total_is_lower_bound,omitempty"`
	MorePages         bool   `json:"more_pages"`
	Matched           int    `json:"matched"`
	Limit             int    `json:"limit"`
	Results           []T    `json:"results"`
	PartialMatches    []T    `json:"partial_matches,omitempty"`
}

func loadService() (*service, error) {
	cfg, err := env.Load()
	if err != nil {
		return nil, err
	}
	client, err := anna.NewClient(anna.Config{
		BaseURL: cfg.AnnasBaseURL, AutoBaseURL: cfg.AutoBaseURL,
		SecretKey: cfg.SecretKey, DownloadPath: cfg.DownloadPath,
		AccountCookie: cfg.AccountCookie, SessionFile: cfg.SessionFile,
		Logger: slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})),
	})
	if err != nil {
		return nil, err
	}
	return &service{archive: client}, nil
}

func timeoutFromSeconds(seconds int, fallback time.Duration) (time.Duration, error) {
	if seconds == 0 {
		return fallback, nil
	}
	if seconds < 0 || seconds > int(maxOperationTimeout/time.Second) {
		return 0, anna.NewError(anna.InvalidArgument, "timeout_seconds must be between 1 and 86400")
	}
	return time.Duration(seconds) * time.Second, nil
}

func normalizeSearch(params SearchParams, article bool) (anna.SearchOptions, error) {
	options, err := commonSearchOptions(params.Query, params.Title, params.Author, params.Publisher, params.Year, params.Language, params.Source, params.Sort, params.Page, params.Limit)
	if err != nil {
		return anna.SearchOptions{}, err
	}
	if options.Content, err = filterValues(params.Content, "content"); err != nil {
		return anna.SearchOptions{}, err
	}
	if options.Format, err = filterValues(strings.ReplaceAll(params.Format, ".", ""), "format"); err != nil {
		return anna.SearchOptions{}, err
	}
	options.Descriptions = params.SearchDescriptions
	defaultContent := ""
	if article {
		defaultContent = "journals"
	}
	options, err = anna.NormalizeSearch(params.Query, options, defaultContent)
	if err != nil {
		return anna.SearchOptions{}, err
	}
	if params.DownloadableOnly {
		options.Access = []string{"aa_download"}
		if options.Index == "journals" {
			options.Access = append(options.Access, "aa_scidb")
		}
	}
	return options, nil
}
func normalizeMetadataSearch(params MetadataSearchParams) (anna.SearchOptions, error) {
	options, err := commonSearchOptions(params.Query, params.Title, params.Author, params.Publisher, params.Year, params.Language, params.Source, params.Sort, params.Page, params.Limit)
	if err != nil {
		return anna.SearchOptions{}, err
	}
	options.Index = "meta"
	return anna.NormalizeSearch(params.Query, options, "")
}
func commonSearchOptions(query, title, author, publisher, year, language, source, sort string, page, limit int) (anna.SearchOptions, error) {
	options := anna.SearchOptions{Page: page, Sort: sort, Fields: []anna.SearchField{{Name: "title", Value: title}, {Name: "author", Value: author}, {Name: "publisher", Value: publisher}, {Name: "year", Value: year}}}
	if limit < 0 {
		return options, anna.NewError(anna.InvalidArgument, "limit must be zero or greater")
	}
	var err error
	if options.Language, err = filterValues(language, "language"); err != nil {
		return options, err
	}
	if options.Source, err = filterValues(source, "source"); err != nil {
		return options, err
	}
	return anna.NormalizeSearch(query, options, "")
}

// filterValues only decodes the interface's comma-separated input and bounds its size.
func filterValues(raw, name string) ([]string, error) {
	var values []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	if len(values) > maxFilterValues {
		return nil, anna.NewError(anna.InvalidArgument, fmt.Sprintf("%s accepts at most %d values", name, maxFilterValues))
	}
	return values, nil
}

// pageResult trims a page to limit. Partial matches only fill the room the
// exact results leave, so the output stays within limit.
func pageResult[T any](page anna.SearchPage[T], options anna.SearchOptions, limit, defaultLimit int) searchResult[T] {
	if limit == 0 {
		limit = defaultLimit
	}
	results, partial := page.Results, page.Partial
	if results == nil {
		results = []T{}
	}
	if limit > 0 {
		results = results[:min(limit, len(results))]
		partial = partial[:min(limit-len(results), len(partial))]
	}
	if len(partial) == 0 {
		partial = nil
	}
	return searchResult[T]{
		Page: options.Page, Language: strings.Join(options.Language, ","), Content: strings.Join(options.Content, ","), Index: options.Index,
		Total: page.Total, TotalIsLowerBound: page.TotalIsLowerBound, MorePages: page.MorePages,
		Matched: len(page.Results), Limit: len(results), Results: results, PartialMatches: partial,
	}
}

// searchOperationContext gives a search plus optional stats enrichment one
// shared timeout. Client operations started with opCtx remain bounded by this
// deadline even though each archive method also applies its own timeout.
func searchOperationContext(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout > 0 {
		return context.WithTimeout(ctx, timeout)
	}
	return context.WithCancel(ctx)
}

func (s *service) enrichStats(ctx context.Context, hashes []string, attach func(string, *anna.RecordStats)) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	seen := make(map[string]struct{}, len(hashes))
	unique := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToLower(hash)
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		unique = append(unique, hash)
	}
	if len(unique) == 0 {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	stats, err := s.archive.FileStats(ctx, unique)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		// Statistics are optional. Only cancellation and the shared operation
		// deadline should turn an otherwise successful search into an error.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return nil
	}
	for hash, stat := range stats {
		copy := stat
		attach(strings.ToLower(hash), &copy)
	}
	return nil
}

func enrichBookPageStats(ctx context.Context, s *service, result *searchResult[*anna.Book]) error {
	byHash := make(map[string][]*anna.Book, len(result.Results)+len(result.PartialMatches))
	hashes := make([]string, 0, len(result.Results)+len(result.PartialMatches))
	for _, books := range [][]*anna.Book{result.Results, result.PartialMatches} {
		for _, book := range books {
			if book == nil || book.Hash == "" {
				continue
			}
			hash := strings.ToLower(book.Hash)
			if _, ok := byHash[hash]; !ok {
				hashes = append(hashes, hash)
			}
			byHash[hash] = append(byHash[hash], book)
		}
	}
	return s.enrichStats(ctx, hashes, func(hash string, stats *anna.RecordStats) {
		for _, book := range byHash[hash] {
			book.Stats = stats
		}
	})
}

func enrichPaperPageStats(ctx context.Context, s *service, result *searchResult[*anna.Paper]) error {
	byHash := make(map[string][]*anna.Paper, len(result.Results)+len(result.PartialMatches))
	hashes := make([]string, 0, len(result.Results)+len(result.PartialMatches))
	for _, papers := range [][]*anna.Paper{result.Results, result.PartialMatches} {
		for _, paper := range papers {
			if paper == nil || paper.Hash == "" {
				continue
			}
			hash := strings.ToLower(paper.Hash)
			if _, ok := byHash[hash]; !ok {
				hashes = append(hashes, hash)
			}
			byHash[hash] = append(byHash[hash], paper)
		}
	}
	return s.enrichStats(ctx, hashes, func(hash string, stats *anna.RecordStats) {
		for _, paper := range byHash[hash] {
			paper.Stats = stats
		}
	})
}

func (s *service) bookSearch(ctx context.Context, params SearchParams, timeout time.Duration, defaultLimit int) (searchResult[*anna.Book], error) {
	options, err := normalizeSearch(params, false)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	opCtx, cancel := ctx, func() {}
	if params.IncludeStats {
		opCtx, cancel = searchOperationContext(ctx, timeout)
	}
	defer cancel()
	page, err := s.archive.FindBook(opCtx, strings.TrimSpace(params.Query), options, timeout)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	if params.IncludeStats && opCtx.Err() != nil {
		return searchResult[*anna.Book]{}, opCtx.Err()
	}
	result := pageResult(page, options, params.Limit, defaultLimit)
	if params.IncludeStats {
		if err := enrichBookPageStats(opCtx, s, &result); err != nil {
			return searchResult[*anna.Book]{}, err
		}
	}
	return result, nil
}

func (s *service) articleSearch(ctx context.Context, params SearchParams, timeout time.Duration, defaultLimit int) (any, error) {
	options, err := normalizeSearch(params, true)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(params.Query)
	opCtx, cancel := ctx, func() {}
	if params.IncludeStats {
		opCtx, cancel = searchOperationContext(ctx, timeout)
	}
	defer cancel()
	if anna.IsDOIQuery(query) {
		paper, err := s.archive.LookupDOI(opCtx, query, timeout)
		if err != nil {
			return nil, err
		}
		if params.IncludeStats && opCtx.Err() != nil {
			return nil, opCtx.Err()
		}
		if params.IncludeStats && paper != nil && paper.Hash != "" {
			if err := s.enrichStats(opCtx, []string{strings.ToLower(paper.Hash)}, func(hash string, stats *anna.RecordStats) {
				if hash == strings.ToLower(paper.Hash) {
					paper.Stats = stats
				}
			}); err != nil {
				return nil, err
			}
		}
		return paper, nil
	}
	page, err := s.archive.FindArticle(opCtx, query, options, timeout)
	if err != nil {
		return nil, err
	}
	if params.IncludeStats && opCtx.Err() != nil {
		return nil, opCtx.Err()
	}
	result := pageResult(page, options, params.Limit, defaultLimit)
	if params.IncludeStats {
		if err := enrichPaperPageStats(opCtx, s, &result); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *service) metadataSearch(ctx context.Context, params MetadataSearchParams, timeout time.Duration, defaultLimit int) (searchResult[*anna.MetadataRecord], error) {
	options, err := normalizeMetadataSearch(params)
	if err != nil {
		return searchResult[*anna.MetadataRecord]{}, err
	}
	page, err := s.archive.FindMetadata(ctx, strings.TrimSpace(params.Query), options, timeout)
	if err != nil {
		return searchResult[*anna.MetadataRecord]{}, err
	}
	return pageResult(page, options, params.Limit, defaultLimit), nil
}

func (s *service) details(ctx context.Context, params DetailsParams, timeout time.Duration) (*anna.Details, error) {
	if _, _, err := anna.ParseRecordID(params.ID); err != nil {
		return nil, err
	}
	return s.archive.Details(ctx, params.ID, timeout)
}

func (s *service) accountStatus(ctx context.Context, timeout time.Duration) (*anna.AccountStatus, error) {
	return s.archive.AccountStatus(ctx, timeout)
}

func (s *service) bookDownload(ctx context.Context, params BookDownloadParams, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	return s.archive.DownloadBook(ctx, &anna.Book{Hash: params.Hash, Title: params.Title, Format: params.Format}, timeout, progress)
}

func (s *service) articleDownload(ctx context.Context, params ArticleDownloadParams, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	if (strings.TrimSpace(params.DOI) == "") == (strings.TrimSpace(params.Hash) == "") {
		return anna.DownloadResult{}, anna.NewError(anna.InvalidArgument, "pass exactly one of doi or hash")
	}
	return s.archive.DownloadArticle(ctx, anna.ArticleDownloadOptions{DOI: params.DOI, Hash: params.Hash, Title: params.Title, Format: params.Format}, timeout, progress)
}

func codedError(err error) string {
	if err == nil {
		return "[UPSTREAM] unknown error"
	}
	return fmt.Sprintf("[%s] %s", anna.CodeOf(err), applicationError(err))
}
