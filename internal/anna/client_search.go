package anna

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// searchResults is one parsed results page.
type searchResults struct {
	cards   []parse.Card
	doc     *goquery.Document
	pageURL string
	page    int
}

func (c *Client) search(ctx context.Context, query string, options SearchOptions, defaultContent string) (searchResults, error) {
	query = strings.TrimSpace(query)
	if query == "" && len(options.Fields) == 0 {
		return searchResults{}, apperr.New(apperr.InvalidArgument, "search query is empty")
	}
	if err := c.requireArchiveAccess(ctx); err != nil {
		return searchResults{}, err
	}
	options = ResolveSearch(options, defaultContent)
	var results searchResults
	err := c.onMirror(ctx, func(base string) error {
		var err error
		results, err = c.searchOn(ctx, base, query, options)
		return err
	})
	return results, err
}

func (c *Client) searchOn(ctx context.Context, base, query string, options SearchOptions) (searchResults, error) {
	pageURL := buildSearchURL(base, query, options)
	doc, _, err := c.fetchDocument(ctx, pageURL)
	if err != nil {
		return searchResults{pageURL: pageURL}, err
	}
	cards := parse.SearchCards(doc)
	if len(cards) == 0 && !parse.IsArchivePage(doc) {
		c.mirrorFailed(pageURL, mirror.Failure{Reason: "answered a search with a page that is not Anna's Archive", Blocked: true})
		return searchResults{pageURL: pageURL}, apperr.New(apperr.UpstreamBlocked, "archive returned an unexpected page; check the mirror and account access")
	}
	if len(cards) == 0 && parse.ShowsResults(doc, options.Page) {
		return searchResults{pageURL: pageURL}, apperr.New(apperr.Upstream, "Anna's Archive returned search results this version cannot read; its page layout may have changed. Check for a newer release, or report it at https://github.com/SokolskyNikita/annas-mcp/issues")
	}
	if err := checkFacetFilter(doc, "src", "source", options.Source); err != nil {
		return searchResults{pageURL: pageURL}, err
	}
	if err := checkFacetFilter(doc, "content", "content", options.Content); err != nil {
		return searchResults{pageURL: pageURL}, err
	}
	return searchResults{cards: cards, doc: doc, pageURL: pageURL, page: options.Page}, nil
}

// filePage builds a page of file results, converting each card with convert.
func filePage[T any](results searchResults, convert func(*Book) T) SearchPage[T] {
	page := splitPage(results.cards,
		func(card parse.Card) bool { return card.Prefix == "md5" },
		func(card parse.Card) T { return convert(bookFromCard(card, results.pageURL)) })
	pageTotals(&page, results.doc, results.page)
	return page
}

// metadataPage builds a page of catalog records.
func metadataPage(results searchResults) SearchPage[*MetadataRecord] {
	page := splitPage(results.cards,
		func(card parse.Card) bool { return card.Prefix != "md5" },
		func(card parse.Card) *MetadataRecord { return metadataFromCard(card, results.pageURL) })
	pageTotals(&page, results.doc, results.page)
	return page
}

func (c *Client) paperFromSciDB(ctx context.Context, doc *goquery.Document, doi, scidbURL, base string) (*Paper, error) {
	downloadURL, hash, err := sciDBFile(doc, scidbURL)
	if err != nil {
		return nil, err
	}
	paper := &Paper{DOI: doi, PageURL: scidbURL, Hash: hash, DownloadURL: downloadURL, Format: "PDF"}
	detailURL := buildPathURL(base, "/md5/"+paper.Hash)
	detail, _, err := c.fetchDocument(ctx, detailURL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return paper, nil
	}
	addRecordDetails(paper, detail)
	return paper, nil
}

// addRecordDetails fills in a SciDB paper's title, journal, authors and size
// from its record page.
func addRecordDetails(paper *Paper, detail *goquery.Document) {
	details := parse.ReadRecordDetails(detail)
	paper.Title, paper.Journal, paper.Authors, paper.Size = details.Title, details.Journal, details.Authors, details.Size
}

// pageTotals fills in totals for a parsed page. Without Anna's count, a
// full page is taken to mean that more results may follow. A page past the
// last result shows no count either; the total is then unknown, so it is
// reported as a lower bound of zero, with no more pages.
func pageTotals[T any](page *SearchPage[T], doc *goquery.Document, pageNumber int) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	total, lowerBound, found := parse.ResultsTotal(doc)
	if !found && pageNumber > 1 && len(page.Results) == 0 && len(page.Partial) == 0 {
		page.Total, page.TotalIsLowerBound, page.MorePages = 0, true, false
		return
	}
	if !found {
		total = (pageNumber-1)*parse.ResultsPerPage + len(page.Results)
		lowerBound = len(page.Results) >= parse.ResultsPerPage
	}
	page.Total, page.TotalIsLowerBound = total, lowerBound
	page.MorePages = pageNumber < parse.MaxResultPages && (total > pageNumber*parse.ResultsPerPage || lowerBound)
}

// checkFacetFilter rejects requested values the page does not offer. Anna
// ignores an unknown value and returns no exact matches, which would read
// as the work being absent. Pages without the facet are not checked.
func checkFacetFilter(doc *goquery.Document, name, label string, requested []string) error {
	if len(requested) == 0 || doc == nil {
		return nil
	}
	offered := parse.FacetValues(doc, name)
	if len(offered) == 0 {
		return nil
	}
	known := make(map[string]bool, len(offered))
	for _, value := range offered {
		known[value] = true
	}
	for _, value := range requested {
		value = strings.TrimPrefix(value, "-")
		if !known[value] {
			sort.Strings(offered)
			return apperr.New(apperr.InvalidArgument, fmt.Sprintf("unknown %s %q for this search; use one of: %s", label, value, strings.Join(offered, ", ")))
		}
	}
	return nil
}
