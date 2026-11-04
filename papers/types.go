// Package papers searches, inspects and retrieves scholarly works through
// OpenAlex with optional Anna's Archive fallback.
package papers

import (
	"context"
	"github.com/SokolskyNikita/annas-mcp/anna"
	"net/http"
	"time"
)

// Archive is the public Anna SDK surface used by the coordinator. Implementations
// may initialize the SDK lazily, so unrelated configuration cannot block OpenAlex.
type Archive interface {
	FindArticle(context.Context, string, anna.SearchOptions, time.Duration) (anna.SearchPage[*anna.Paper], error)
	LookupDOI(context.Context, string, time.Duration) (*anna.Paper, error)
	Details(context.Context, string, time.Duration) (*anna.Details, error)
	FileStats(context.Context, []string) (map[string]anna.RecordStats, error)
	DownloadArticle(context.Context, anna.ArticleDownloadOptions, time.Duration, anna.ProgressFunc) (anna.DownloadResult, error)
}

type Config struct {
	OpenAlexAPIKey    string `json:"-"`
	DownloadPath      string
	CacheDir          string
	Archive           Archive
	HTTPClient        *http.Client `json:"-"`
	ContentHTTPClient *http.Client `json:"-"`
}

type SearchOptions struct {
	Query              string `json:"query,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Title              string `json:"title,omitempty"`
	Author             string `json:"author,omitempty"`
	Publisher          string `json:"publisher,omitempty"`
	Year               string `json:"year,omitempty"`
	Language           string `json:"language,omitempty"`
	Format             string `json:"format,omitempty"`
	Source             string `json:"source,omitempty"`
	Sort               string `json:"sort,omitempty"`
	Content            string `json:"content,omitempty"`
	WorkType           string `json:"work_type,omitempty"`
	AuthorID           string `json:"author_id,omitempty"`
	InstitutionID      string `json:"institution_id,omitempty"`
	RepositoryID       string `json:"repository_id,omitempty"`
	Relation           string `json:"relation,omitempty"`
	WorkID             string `json:"work_id,omitempty"`
	Cursor             string `json:"cursor,omitempty"`
	DownloadableOnly   bool   `json:"downloadable_only,omitempty"`
	SearchDescriptions bool   `json:"search_descriptions,omitempty"`
	IncludeStats       bool   `json:"include_stats,omitempty"`
	Page               int    `json:"page,omitempty"`
	Limit              int    `json:"limit,omitempty"`
}

type DetailsOptions struct {
	Provider                      string
	LocationOffset, LocationLimit int
}

type DownloadOptions struct {
	DOI        string `json:"doi,omitempty"`
	OpenAlexID string `json:"openalex_id,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Title      string `json:"title,omitempty"`
	Format     string `json:"format,omitempty"`
	Artifact   string `json:"artifact,omitempty"`
	LocationID string `json:"location_id,omitempty"`
	Version    string `json:"version,omitempty"`
}

type Work struct {
	Provider        string            `json:"provider"`
	OpenAlexID      string            `json:"openalex_id,omitempty"`
	DOI             string            `json:"doi,omitempty"`
	Title           string            `json:"title,omitempty"`
	Authors         string            `json:"authors"`
	Authorships     []Author          `json:"authorships,omitempty"`
	Journal         string            `json:"journal,omitempty"`
	Year            string            `json:"year,omitempty"`
	PublicationDate string            `json:"publication_date,omitempty"`
	WorkType        string            `json:"work_type,omitempty"`
	Language        string            `json:"language,omitempty"`
	Format          string            `json:"format,omitempty"`
	Size            string            `json:"size,omitempty"`
	Sources         []string          `json:"sources,omitempty"`
	FastDownload    bool              `json:"fast_download,omitempty"`
	PossibleIssues  bool              `json:"possible_issues,omitempty"`
	Hash            string            `json:"hash,omitempty"`
	Description     string            `json:"description,omitempty"`
	Abstract        string            `json:"abstract,omitempty"`
	PageURL         string            `json:"page_url"`
	URL             string            `json:"url,omitempty"`
	CitationCount   int               `json:"citation_count,omitempty"`
	Access          []string          `json:"access"`
	Locations       []Location        `json:"locations,omitempty"`
	LocationTotal   int               `json:"location_total,omitempty"`
	NextDetailsArgs map[string]any    `json:"next_details_args,omitempty"`
	DetailsArgs     map[string]any    `json:"details_args,omitempty"`
	DownloadArgs    map[string]any    `json:"download_args,omitempty"`
	Stats           *anna.RecordStats `json:"stats,omitempty"`
	ContentPDFURL   string            `json:"-"`
	ContentXMLURL   string            `json:"-"`
}

type Author struct {
	ID    string `json:"id,omitempty"`
	Name  string `json:"name"`
	ORCID string `json:"orcid,omitempty"`
}
type Location struct {
	DownloadURL    string         `json:"-"`
	ID             string         `json:"id"`
	LandingPageURL string         `json:"landing_page_url,omitempty"`
	PDFURL         string         `json:"pdf_url,omitempty"`
	IsOA           bool           `json:"is_oa"`
	Version        string         `json:"version,omitempty"`
	License        string         `json:"license,omitempty"`
	SourceID       string         `json:"source_id,omitempty"`
	Source         string         `json:"source,omitempty"`
	SourceType     string         `json:"source_type,omitempty"`
	DOI            string         `json:"doi,omitempty"`
	DownloadArgs   map[string]any `json:"download_args,omitempty"`
}

type SearchResult struct {
	Work              *Work          `json:"-"`
	Provider          string         `json:"provider"`
	Page              int            `json:"page"`
	Limit             int            `json:"limit"`
	Total             int            `json:"total"`
	UpstreamTotal     int            `json:"upstream_total,omitempty"`
	TotalIsLowerBound bool           `json:"total_is_lower_bound,omitempty"`
	MorePages         bool           `json:"more_pages"`
	Matched           int            `json:"matched"`
	Results           []*Work        `json:"results"`
	PartialMatches    []*Work        `json:"partial_matches,omitempty"`
	NextSearchArgs    map[string]any `json:"next_search_args,omitempty"`
	Warnings          []Attempt      `json:"warnings,omitempty"`
}

type DownloadResult struct {
	anna.DownloadResult
	Provider       string `json:"provider"`
	Route          string `json:"route"`
	OpenAlexID     string `json:"openalex_id,omitempty"`
	DOI            string `json:"doi,omitempty"`
	Hash           string `json:"hash,omitempty"`
	LocationID     string `json:"location_id,omitempty"`
	Version        string `json:"version,omitempty"`
	MIMEType       string `json:"mime_type,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Reused         bool   `json:"reused"`
	URL            string `json:"url,omitempty"`
	FallbackReason string `json:"fallback_reason,omitempty"`
}

type Attempt struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
	Message  string `json:"message,omitempty"`
}

// Error retains provider failures and recovery evidence for automated callers.
type Error struct {
	Code        string           `json:"code"`
	Message     string           `json:"message"`
	Provider    string           `json:"provider,omitempty"`
	Reason      string           `json:"reason,omitempty"`
	Retryable   bool             `json:"retryable"`
	RetryAfter  int              `json:"retry_after,omitempty"`
	WorkID      string           `json:"work_id,omitempty"`
	LandingPage string           `json:"landing_page,omitempty"`
	Attempts    []Attempt        `json:"attempts,omitempty"`
	NextActions []map[string]any `json:"next_actions,omitempty"`
	Err         error            `json:"-"`
}

func (e *Error) Error() string     { return e.Message }
func (e *Error) Unwrap() error     { return e.Err }
func (e *Error) ErrorCode() string { return e.Code }
