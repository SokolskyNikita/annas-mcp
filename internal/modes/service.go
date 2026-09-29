package modes

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

const defaultSearchLimit = 10
const maxOperationTimeout = 24 * time.Hour

var languageCodePattern = regexp.MustCompile(`^[a-z]{2}$`)

// archiveClient makes transport adapters testable without a network or credentials.
type archiveClient interface {
	FindBook(context.Context, string, anna.SearchOptions, time.Duration) ([]*anna.Book, error)
	FindArticle(context.Context, string, anna.SearchOptions, time.Duration) ([]*anna.Paper, error)
	LookupDOI(context.Context, string, time.Duration) (*anna.Paper, error)
	DownloadBook(context.Context, *anna.Book, time.Duration, anna.ProgressFunc) (anna.DownloadResult, error)
	DownloadArticle(context.Context, anna.ArticleDownloadOptions, time.Duration, anna.ProgressFunc) (anna.DownloadResult, error)
}

type service struct{ archive archiveClient }

type searchResult[T any] struct {
	Page     int    `json:"page"`
	Language string `json:"language"`
	Content  string `json:"content,omitempty"`
	Index    string `json:"index,omitempty"`
	Matched  int    `json:"matched"`
	Limit    int    `json:"limit"`
	Results  []T    `json:"results"`
}

func loadService() (*service, error) {
	cfg, err := env.Load()
	if err != nil {
		return nil, err
	}
	return &service{archive: anna.NewClient(anna.Config{
		BaseURL: cfg.AnnasBaseURL, AutoBaseURL: cfg.AutoBaseURL,
		SecretKey: cfg.SecretKey, DownloadPath: cfg.DownloadPath,
		AccountCookie: cfg.AccountCookie,
	})}, nil
}

func timeoutFromSeconds(seconds int, fallback time.Duration) (time.Duration, error) {
	if seconds == 0 {
		return fallback, nil
	}
	if seconds < 0 || seconds > int(maxOperationTimeout/time.Second) {
		return 0, apperr.New(apperr.InvalidArgument, "timeout_seconds must be between 1 and 86400")
	}
	return time.Duration(seconds) * time.Second, nil
}

func normalizeSearch(params SearchParams, article bool) (anna.SearchOptions, error) {
	if strings.TrimSpace(params.Query) == "" {
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, "search query is empty")
	}
	if params.Page < 0 {
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, "page must be 1 or greater")
	}
	if params.Limit < 0 {
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, "limit must be zero or greater")
	}
	language := strings.ToLower(strings.TrimSpace(params.Language))
	if language != "" && !languageCodePattern.MatchString(language) {
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, "language must be a two-letter ISO 639-1 code such as en")
	}
	content := strings.ToLower(strings.TrimSpace(params.Content))
	// Preserve old client inputs at the boundary while using current upstream filters.
	switch content {
	case "", "book_fiction", "book_nonfiction", "book_unknown", "book_comic", "magazine", "standards_document", "book_any", "journal":
	default:
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, fmt.Sprintf("unsupported content filter %q", content))
	}
	index := ""
	if content == "journal" {
		content, index = "", "journals"
	}
	if content == "book_any" {
		content = ""
	}
	if article && content == "" {
		index = "journals"
	}
	page := params.Page
	if page == 0 {
		page = 1
	}
	return anna.SearchOptions{Content: content, Language: language, Index: index, Page: page}, nil
}

func pageResult[T any](items []T, options anna.SearchOptions, limit, defaultLimit int) searchResult[T] {
	matched := len(items)
	if limit == 0 {
		limit = defaultLimit
	}
	if limit == 0 || limit > matched {
		limit = matched
	}
	if items == nil {
		items = []T{}
	}
	return searchResult[T]{Page: options.Page, Language: options.Language, Content: options.Content, Index: options.Index, Matched: matched, Limit: limit, Results: items[:limit]}
}

func (s *service) bookSearch(ctx context.Context, params SearchParams, timeout time.Duration, defaultLimit int) (searchResult[*anna.Book], error) {
	options, err := normalizeSearch(params, false)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	books, err := s.archive.FindBook(ctx, strings.TrimSpace(params.Query), options, timeout)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	return pageResult(books, options, params.Limit, defaultLimit), nil
}

func (s *service) articleSearch(ctx context.Context, params SearchParams, timeout time.Duration, defaultLimit int) (any, error) {
	options, err := normalizeSearch(params, true)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(params.Query)
	if anna.IsDOIQuery(query) {
		return s.archive.LookupDOI(ctx, query, timeout)
	}
	papers, err := s.archive.FindArticle(ctx, query, options, timeout)
	if err != nil {
		return nil, err
	}
	return pageResult(papers, options, params.Limit, defaultLimit), nil
}

func (s *service) bookDownload(ctx context.Context, params BookDownloadParams, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	return s.archive.DownloadBook(ctx, &anna.Book{Hash: params.Hash, Title: params.Title, Format: params.Format}, timeout, progress)
}

func (s *service) articleDownload(ctx context.Context, params ArticleDownloadParams, timeout time.Duration, progress anna.ProgressFunc) (anna.DownloadResult, error) {
	if (strings.TrimSpace(params.DOI) == "") == (strings.TrimSpace(params.Hash) == "") {
		return anna.DownloadResult{}, apperr.New(apperr.InvalidArgument, "pass exactly one of doi or hash")
	}
	return s.archive.DownloadArticle(ctx, anna.ArticleDownloadOptions{DOI: params.DOI, Hash: params.Hash, Title: params.Title, Format: params.Format}, timeout, progress)
}

func codedError(err error) string {
	if err == nil {
		return "[UPSTREAM] unknown error"
	}
	return fmt.Sprintf("[%s] %s", apperr.CodeOf(err), err)
}
