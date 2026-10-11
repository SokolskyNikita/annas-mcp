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

// maxFilterValues bounds each comma-separated filter.
const maxFilterValues = 20

var (
	languageCodePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
	formatPattern       = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	sourcePattern       = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	yearPattern         = regexp.MustCompile(`^\d{1,4}$`)
)

var contentTypes = map[string]bool{
	"book_fiction": true, "book_nonfiction": true, "book_unknown": true, "book_comic": true,
	"magazine": true, "standards_document": true, "musical_score": true, "audiobook": true,
	"other": true, "journal_article": true,
	// Legacy values that ResolveSearch maps or drops.
	"book_any": true, "journal": true,
}

// Anna's sort values; relevance is its default, sent as no sort at all.
var sortValues = map[string]string{
	"": "", "relevance": "", "newest": "newest", "oldest": "oldest",
	"largest": "largest", "smallest": "smallest",
	"newest_added": "newest_added", "oldest_added": "oldest_added", "random": "random",
}

// archiveClient makes transport adapters testable without a network or credentials.
type archiveClient interface {
	FindBook(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Book], error)
	FindArticle(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Paper], error)
	FindMetadata(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.MetadataRecord], error)
	LookupDOI(context.Context, string, time.Duration) (*anna.Paper, error)
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
	return &service{archive: anna.NewClient(anna.Config{
		BaseURL: cfg.AnnasBaseURL, AutoBaseURL: cfg.AutoBaseURL,
		SecretKey: cfg.SecretKey, DownloadPath: cfg.DownloadPath,
		AccountCookie: cfg.AccountCookie, SessionFile: cfg.SessionFile,
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
	options, err := commonSearchOptions(params.Query, params.Title, params.Author, params.Publisher, params.Year, params.Language, params.Source, params.Sort, params.Page, params.Limit)
	if err != nil {
		return anna.SearchOptions{}, err
	}
	if options.Content, err = filterValues(params.Content, "content", func(value string) bool { return contentTypes[value] }); err != nil {
		return anna.SearchOptions{}, err
	}
	if options.Format, err = filterValues(strings.ReplaceAll(params.Format, ".", ""), "format", formatPattern.MatchString); err != nil {
		return anna.SearchOptions{}, err
	}
	options.Descriptions = params.SearchDescriptions
	defaultContent := ""
	if article {
		defaultContent = "journals"
	}
	options = anna.ResolveSearch(options, defaultContent)
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
	if options.Sort == "largest" || options.Sort == "smallest" {
		return anna.SearchOptions{}, apperr.New(apperr.InvalidArgument, "catalog records have no file size; sort by relevance, newest, oldest, newest_added, oldest_added or random")
	}
	options.Index = "meta"
	return options, nil
}

// commonSearchOptions validates what every search accepts, before any request.
func commonSearchOptions(query, title, author, publisher, year, language, source, sort string, page, limit int) (anna.SearchOptions, error) {
	var options anna.SearchOptions
	for _, field := range []anna.SearchField{{Name: "title", Value: title}, {Name: "author", Value: author}, {Name: "publisher", Value: publisher}, {Name: "year", Value: year}} {
		field.Value = strings.Join(strings.Fields(field.Value), " ")
		if field.Value == "" {
			continue
		}
		if len(field.Value) > 300 {
			return options, apperr.New(apperr.InvalidArgument, field.Name+" must be at most 300 characters")
		}
		if field.Name == "year" && !yearPattern.MatchString(field.Value) {
			return options, apperr.New(apperr.InvalidArgument, "year must be a publication year such as 2008")
		}
		options.Fields = append(options.Fields, field)
	}
	if strings.TrimSpace(query) == "" && len(options.Fields) == 0 {
		return options, apperr.New(apperr.InvalidArgument, "search query is empty; pass a query or at least one of title, author, publisher or year")
	}
	if page < 0 {
		return options, apperr.New(apperr.InvalidArgument, "page must be 1 or greater")
	}
	if limit < 0 {
		return options, apperr.New(apperr.InvalidArgument, "limit must be zero or greater")
	}
	options.Page = max(page, 1)
	var err error
	if options.Language, err = filterValues(language, "language", languageCodePattern.MatchString); err != nil {
		return options, apperr.New(apperr.InvalidArgument, "language must be a language code such as en, or a comma-separated list such as en,de")
	}
	for i, value := range options.Language {
		options.Language[i] = canonicalLanguage(value)
	}
	if options.Source, err = filterValues(source, "source", sourcePattern.MatchString); err != nil {
		return options, err
	}
	sortValue, ok := sortValues[strings.ToLower(strings.TrimSpace(sort))]
	if !ok {
		return options, apperr.New(apperr.InvalidArgument, fmt.Sprintf("unsupported sort %q; use relevance, newest, oldest, largest, smallest, newest_added, oldest_added or random", sort))
	}
	options.Sort = sortValue
	return options, nil
}

// filterValues splits a comma-separated filter. Values are lowercased and
// keep a leading "-", which marks an exclusion.
func filterValues(raw, name string, valid func(string) bool) ([]string, error) {
	var values []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		excluded := strings.HasPrefix(part, "-")
		value := strings.TrimSpace(strings.TrimPrefix(part, "-"))
		if value == "" {
			if excluded {
				return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("%s has a - without a value", name))
			}
			continue
		}
		if !valid(value) {
			return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("unsupported %s %q", name, value))
		}
		if excluded {
			value = "-" + value
		}
		values = append(values, value)
	}
	if len(values) > maxFilterValues {
		return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("%s accepts at most %d values", name, maxFilterValues))
	}
	return values, nil
}

// canonicalLanguage restores tag casing that Anna's codes use, such as zh-Hant.
func canonicalLanguage(code string) string {
	prefix := ""
	if strings.HasPrefix(code, "-") {
		prefix, code = "-", code[1:]
	}
	parts := strings.Split(code, "-")
	for i := 1; i < len(parts); i++ {
		switch len(parts[i]) {
		case 4:
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		case 2:
			parts[i] = strings.ToUpper(parts[i])
		}
	}
	return prefix + strings.Join(parts, "-")
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

func (s *service) bookSearch(ctx context.Context, params SearchParams, timeout time.Duration, defaultLimit int) (searchResult[*anna.Book], error) {
	options, err := normalizeSearch(params, false)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	page, err := s.archive.FindBook(ctx, strings.TrimSpace(params.Query), options, timeout)
	if err != nil {
		return searchResult[*anna.Book]{}, err
	}
	return pageResult(page, options, params.Limit, defaultLimit), nil
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
	page, err := s.archive.FindArticle(ctx, query, options, timeout)
	if err != nil {
		return nil, err
	}
	return pageResult(page, options, params.Limit, defaultLimit), nil
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
