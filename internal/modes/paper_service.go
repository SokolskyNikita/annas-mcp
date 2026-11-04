package modes

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/papers"
)

var openAlexQueryID = regexp.MustCompile(`(?i)^W[0-9]+$`)

func (s *service) paperClient() (*papers.Client, error) {
	s.papersOnce.Do(func() {
		// Tests and embedders may supply a coordinator directly.
		if s.papers != nil {
			return
		}
		s.papers, s.papersErr = papers.NewClient(s.paperConfig())
	})
	return s.papers, s.papersErr
}

func (s *service) paperConfig() papers.Config {
	config := papers.Config{}
	if s.hasArchiveCredentials() {
		config.Archive = s.archive
	}
	if s.config != nil {
		config.OpenAlexAPIKey = s.config.OpenAlexAPIKey
		config.DownloadPath = s.config.DownloadPath
		if s.config.SessionFile != "" {
			config.CacheDir = filepath.Dir(s.config.SessionFile)
		}
	}
	return config
}

func (s *service) hasArchiveCredentials() bool {
	if s.config == nil {
		// A supplied archive in tests or an embedding application is already
		// configured by its owner.
		return s.archive != nil
	}
	return strings.TrimSpace(s.config.SecretKey) != "" || strings.TrimSpace(s.config.AccountCookie) != ""
}

func (s *service) hasOpenAlex() bool {
	if s.config != nil {
		return s.config.OpenAlexAPIKey != ""
	}
	return s.papers != nil
}

func isOpenAlexInput(raw string) bool {
	raw = strings.TrimSpace(raw)
	return openAlexQueryID.MatchString(raw) || strings.HasPrefix(strings.ToLower(raw), "https://openalex.org/")
}

func (s *service) legacyArticleSearch(params ArticleSearchParams) bool {
	p := strings.ToLower(strings.TrimSpace(params.Provider))
	if p != "" && p != "auto" && p != "anna" {
		return false
	}
	if params.WorkType != "" || params.AuthorID != "" || params.InstitutionID != "" || params.RepositoryID != "" || params.Relation != "" || params.WorkID != "" || params.Cursor != "" || isOpenAlexInput(params.Query) {
		return false
	}
	return p == "anna" || !s.hasOpenAlex()
}

func (s *service) paperSearch(ctx context.Context, params ArticleSearchParams, timeout time.Duration, defaultLimit int) (any, error) {
	if s.legacyArticleSearch(params) {
		return s.articleSearch(ctx, params.SearchParams, timeout, defaultLimit)
	}
	client, err := s.paperClient()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := searchOperationContext(ctx, timeout)
	defer cancel()
	result, err := client.Search(opCtx, papers.SearchOptions{
		Query: params.Query, Provider: params.Provider, Title: params.Title, Author: params.Author,
		Publisher: params.Publisher, Year: params.Year, Language: params.Language, Format: params.Format,
		Source: params.Source, Sort: params.Sort, Content: params.Content, WorkType: params.WorkType,
		AuthorID: params.AuthorID, InstitutionID: params.InstitutionID, RepositoryID: params.RepositoryID,
		Relation: params.Relation, WorkID: params.WorkID, Cursor: params.Cursor,
		DownloadableOnly: params.DownloadableOnly, SearchDescriptions: params.SearchDescriptions,
		IncludeStats: params.IncludeStats, Page: params.Page, Limit: params.Limit,
	})
	if err != nil {
		return nil, err
	}
	if result.Work != nil {
		return result.Work, nil
	}
	return result, nil
}

const paperLocationsPerPage = 5

func (s *service) paperDetails(ctx context.Context, params ArticleDetailsParams, timeout time.Duration) (any, error) {
	maxPage := int(^uint(0)>>1) / paperLocationsPerPage
	if params.LocationsPage < 0 || params.LocationsPage > maxPage {
		return nil, anna.NewError(anna.InvalidArgument, "locations_page is outside the supported range")
	}
	client, err := s.paperClient()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := searchOperationContext(ctx, timeout)
	defer cancel()
	page := params.LocationsPage
	if page == 0 {
		page = 1
	}
	result, err := client.Details(opCtx, params.ID, papers.DetailsOptions{Provider: params.Provider, LocationOffset: (page - 1) * paperLocationsPerPage, LocationLimit: paperLocationsPerPage})
	if result != nil && result.NextDetailsArgs != nil {
		result.NextDetailsArgs = map[string]any{"id": result.OpenAlexID, "provider": "openalex", "locations_page": page + 1}
	}
	return result, err
}

func (s *service) paperDownload(ctx context.Context, params ArticleDownloadParams, timeout time.Duration, progress anna.ProgressFunc) (any, error) {
	p := strings.ToLower(strings.TrimSpace(params.Provider))
	plain := params.OpenAlexID == "" && params.LocationID == "" && params.Version == "" && params.Artifact == "" && !isOpenAlexInput(params.DOI)
	if plain && (p == "" || p == "auto" || p == "anna") && (p == "anna" || !s.hasOpenAlex()) {
		return s.articleDownload(ctx, params, timeout, progress)
	}
	client, err := s.paperClient()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := searchOperationContext(ctx, timeout)
	defer cancel()
	return client.Download(opCtx, papers.DownloadOptions{
		DOI: params.DOI, OpenAlexID: params.OpenAlexID, Hash: params.Hash,
		Provider: params.Provider, Title: params.Title, Format: params.Format,
		Artifact: params.Artifact, LocationID: params.LocationID, Version: params.Version,
	}, progress)
}

func (s *service) paperRead(ctx context.Context, params ArticleReadParams, timeout time.Duration) (any, error) {
	client, err := s.paperClient()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := searchOperationContext(ctx, timeout)
	defer cancel()
	return client.Read(opCtx, papers.ReadOptions{ID: params.ID, Continuation: params.Continuation, Limit: params.Limit})
}

func (s *service) providerStatus(ctx context.Context, params AccountStatusParams, timeout time.Duration) (any, error) {
	provider := strings.ToLower(strings.TrimSpace(params.Provider))
	if provider == "" {
		provider = "anna"
		if s.config != nil && s.config.OpenAlexAPIKey != "" && !s.hasArchiveCredentials() {
			provider = "openalex"
		}
	}
	if provider == "anna" {
		return s.accountStatus(ctx, timeout)
	}
	if provider != "openalex" {
		return nil, anna.NewError(anna.InvalidArgument, "provider must be anna or openalex")
	}
	client, err := s.paperClient()
	if err != nil {
		return nil, err
	}
	opCtx, cancel := searchOperationContext(ctx, timeout)
	defer cancel()
	return client.Status(opCtx)
}
