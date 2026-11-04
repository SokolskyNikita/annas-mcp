package papers

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/openalex"
)

// Search resolves an exact identifier or returns one provider's result page.
func (c *Client) Search(ctx context.Context, o SearchOptions) (SearchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p, err := provider(o.Provider)
	if err != nil {
		return SearchResult{}, err
	}
	if o.Page < 0 || o.Limit < 0 || o.Limit > 100 {
		return SearchResult{}, invalid("page must be nonnegative and limit between zero and 100")
	}
	if o.Page == 0 {
		o.Page = 1
	}
	if o.Limit == 0 {
		o.Limit = 10
	}
	if strings.TrimSpace(o.Query+o.Title+o.Author+o.Publisher+o.Year+o.AuthorID+o.InstitutionID+o.RepositoryID+o.Relation+o.WorkType) == "" {
		return SearchResult{}, invalid("provide query, title, author or another scholarly filter")
	}
	exact := openalex.IsWorkID(o.Query) || openalex.NormalizeDOI(o.Query) != ""
	if exact && (o.Title != "" || o.Author != "" || o.Publisher != "" || o.Year != "" || o.Language != "" || o.WorkType != "" || o.AuthorID != "" || o.InstitutionID != "" || o.RepositoryID != "" || o.Relation != "" || o.WorkID != "" || o.Cursor != "" || o.Page > 1) {
		return SearchResult{}, invalid("exact identifier lookup cannot combine discovery filters; use article_details or a keyword query")
	}
	aaOnly := o.Source != "" || o.Format != "" || o.Content != "" || o.Publisher != "" || o.SearchDescriptions || o.IncludeStats || o.Sort == "largest" || o.Sort == "smallest" || o.Sort == "newest_added" || o.Sort == "oldest_added" || o.Sort == "random"
	oaOnly := o.WorkType != "" || o.AuthorID != "" || o.InstitutionID != "" || o.RepositoryID != "" || o.Relation != "" || o.WorkID != "" || o.Cursor != "" || o.Sort == "most_cited" || strings.Contains(o.Year, "-") || openalex.IsWorkID(o.Query)
	if aaOnly && oaOnly {
		return SearchResult{}, invalid("OpenAlex-only and Anna-only search filters cannot be combined")
	}
	if (p == "openalex" && aaOnly) || (p == "anna" && oaOnly) {
		return SearchResult{}, invalid("search filters are incompatible with the selected provider")
	}
	if p == "anna" || aaOnly || (p == "auto" && c.config.OpenAlexAPIKey == "" && !oaOnly) {
		return c.searchArchive(ctx, o)
	}
	if openalex.IsWorkID(o.Query) || openalex.NormalizeDOI(o.Query) != "" {
		w, err := c.lookup(ctx, o.Query)
		if err == nil {
			return SearchResult{Provider: "openalex", Work: workSummary(w)}, nil
		}
		if p == "auto" && !oaOnly && c.config.Archive != nil && fallbackAllowed(ctx, err) {
			return c.fallbackSearch(ctx, o, err)
		}
		return SearchResult{}, err
	}
	workTypes := csv(o.WorkType)
	if len(workTypes) == 0 {
		workTypes = []string{"article", "review", "preprint", "conference-paper"}
	}
	if o.WorkType == "any" || (o.WorkType == "" && o.Relation != "") {
		workTypes = []string{"any"}
	}
	remote := openalex.SearchOptions{Query: o.Query, Title: o.Title, Author: o.Author, AuthorID: o.AuthorID, InstitutionID: o.InstitutionID, RepositoryID: o.RepositoryID, Year: o.Year, Language: o.Language, WorkTypes: workTypes, Sort: o.Sort, Page: o.Page, PerPage: o.Limit, Cursor: o.Cursor, Relation: o.Relation, WorkID: o.WorkID}
	// The direct-PDF route needs location evidence. Filter the bounded page locally,
	// retain the upstream count separately, and expose continuation for sparse pages.
	page, err := c.oa.Search(ctx, remote)
	if err != nil {
		if p == "auto" && !oaOnly && c.config.Archive != nil && o.Page == 1 && fallbackAllowed(ctx, err) {
			return c.fallbackSearch(ctx, o, c.wrap(err, "openalex"))
		}
		return SearchResult{}, c.wrap(err, "openalex")
	}
	var warnings []Attempt
	if page.Meta.Count == 0 && p == "auto" && !oaOnly && c.config.Archive != nil && o.Page == 1 {
		fallback, fallbackErr := c.searchArchive(ctx, o)
		if fallbackErr == nil {
			return fallback, nil
		}
		if ctx.Err() != nil {
			return SearchResult{}, ctx.Err()
		}
		warnings = []Attempt{{Provider: "anna", Reason: "fallback_unavailable", Message: fallbackErr.Error()}}
	}
	result := SearchResult{Provider: "openalex", Page: o.Page, Limit: o.Limit, Total: page.Meta.Count, Results: []*Work{}, Warnings: warnings}
	for i := range page.Results {
		work := mapWork(&page.Results[i], false)
		if o.DownloadableOnly && work.DownloadArgs == nil {
			continue
		}
		result.Results = append(result.Results, work)
	}
	result.Matched = len(result.Results)
	if o.Cursor != "" {
		result.MorePages = page.Meta.NextCursor != ""
	} else {
		lastPage := 0
		if page.Meta.Count > 0 {
			lastPage = 1 + (page.Meta.Count-1)/o.Limit
		}
		result.MorePages = o.Page < lastPage && o.Page < 10000/o.Limit
	}
	if o.DownloadableOnly {
		result.UpstreamTotal = page.Meta.Count
		result.TotalIsLowerBound = true
		result.Total = len(result.Results)
	}
	if result.MorePages {
		next := o
		next.Provider = "openalex"
		if page.Meta.NextCursor != "" {
			next.Cursor = page.Meta.NextCursor
		} else {
			next.Page++
		}
		result.NextSearchArgs = searchArgs(next)
	}
	return result, nil
}
func (c *Client) fallbackSearch(ctx context.Context, o SearchOptions, first error) (SearchResult, error) {
	result, err := c.searchArchive(ctx, o)
	if err != nil {
		out := c.wrap(first, "openalex")
		if typed, ok := out.(*Error); ok {
			typed.Attempts = append(typed.Attempts, Attempt{Provider: "anna", Reason: "fallback_failed", Message: err.Error()})
		}
		return SearchResult{}, out
	}
	result.Warnings = []Attempt{{Provider: "openalex", Reason: "fallback", Message: first.Error()}}
	return result, nil
}
func (c *Client) searchArchive(ctx context.Context, o SearchOptions) (SearchResult, error) {
	if c.config.Archive == nil {
		return SearchResult{}, missingArchive()
	}
	if openalex.NormalizeDOI(o.Query) != "" {
		w, err := c.config.Archive.LookupDOI(ctx, o.Query, 0)
		if err != nil {
			return SearchResult{}, c.wrap(err, "anna")
		}
		mapped := mapArchive(w)
		if o.IncludeStats {
			c.enrichStats(ctx, []*Work{mapped})
			if ctx.Err() != nil {
				return SearchResult{}, ctx.Err()
			}
		}
		return SearchResult{Provider: "anna", Work: mapped}, nil
	}
	opts := anna.SearchOptions{Page: o.Page, Sort: o.Sort, Content: csv(o.Content), Language: csv(o.Language), Source: csv(o.Source), Format: csv(o.Format), Descriptions: o.SearchDescriptions, Fields: []anna.SearchField{{Name: "title", Value: o.Title}, {Name: "author", Value: o.Author}, {Name: "publisher", Value: o.Publisher}, {Name: "year", Value: o.Year}}}
	var err error
	opts, err = anna.NormalizeSearch(o.Query, opts, "journals")
	if err != nil {
		return SearchResult{}, c.wrap(err, "anna")
	}
	if o.DownloadableOnly {
		opts.Access = []string{"aa_download", "aa_scidb"}
	}
	page, err := c.config.Archive.FindArticle(ctx, o.Query, opts, 0)
	if err != nil {
		return SearchResult{}, c.wrap(err, "anna")
	}
	result := SearchResult{Provider: "anna", Page: opts.Page, Limit: o.Limit, Total: page.Total, TotalIsLowerBound: page.TotalIsLowerBound, MorePages: page.MorePages, Matched: len(page.Results), Results: []*Work{}}
	for _, w := range page.Results {
		if len(result.Results) >= o.Limit {
			break
		}
		if w != nil {
			result.Results = append(result.Results, mapArchive(w))
		}
	}
	for _, w := range page.Partial {
		if len(result.Results)+len(result.PartialMatches) >= o.Limit {
			break
		}
		if w != nil {
			result.PartialMatches = append(result.PartialMatches, mapArchive(w))
		}
	}
	if o.IncludeStats {
		c.enrichStats(ctx, result.Results)
		c.enrichStats(ctx, result.PartialMatches)
	}
	if result.MorePages {
		next := o
		next.Provider = "anna"
		next.Page++
		result.NextSearchArgs = searchArgs(next)
	}
	return result, nil
}
func (c *Client) enrichStats(ctx context.Context, works []*Work) {
	hashes := []string{}
	for _, w := range works {
		if w.Hash != "" {
			hashes = append(hashes, w.Hash)
		}
	}
	if len(hashes) == 0 {
		return
	}
	stats, err := c.config.Archive.FileStats(ctx, hashes)
	if err != nil {
		return
	}
	for _, w := range works {
		if s, ok := stats[w.Hash]; ok {
			copy := s
			w.Stats = &copy
		}
	}
}
func searchArgs(o SearchOptions) map[string]any {
	raw, _ := json.Marshal(o)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	return result
}

func fallbackAllowed(ctx context.Context, err error) bool {
	return ctx.Err() == nil && anna.CodeOf(err) != anna.InvalidArgument
}

func workSummary(w *Work) *Work {
	copy := *w
	copy.Abstract = ""
	copy.Description = ""
	copy.Locations = nil
	copy.NextDetailsArgs = nil
	copy.Authorships = nil
	if len(w.Authorships) > 5 {
		names := make([]string, 0, 5)
		for _, a := range w.Authorships[:5] {
			names = append(names, a.Name)
		}
		copy.Authors = strings.Join(names, ", ") + " et al."
	}
	return &copy
}
