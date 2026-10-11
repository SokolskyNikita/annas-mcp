package anna

import (
	"context"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// searchResults is one parsed results page.
type searchResults struct {
	cards   []searchCard
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
	cards := parseSearchCards(doc)
	if len(cards) == 0 && !isArchiveDocument(doc) {
		c.mirrorFailed(pageURL, mirror.Failure{Reason: "answered a search with a page that is not Anna's Archive", Blocked: true})
		return searchResults{pageURL: pageURL}, apperr.New(apperr.UpstreamBlocked, "archive returned an unexpected page; check the mirror and account access")
	}
	if len(cards) == 0 && showsResults(doc, options.Page) {
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
		func(card searchCard) bool { return card.prefix == "md5" },
		func(card searchCard) T { return convert(bookFromCard(card, results.pageURL)) })
	pageTotals(&page, results.doc, results.page)
	return page
}

// metadataPage builds a page of catalog records.
func metadataPage(results searchResults) SearchPage[*MetadataRecord] {
	page := splitPage(results.cards,
		func(card searchCard) bool { return card.prefix != "md5" },
		func(card searchCard) *MetadataRecord { return metadataFromCard(card, results.pageURL) })
	pageTotals(&page, results.doc, results.page)
	return page
}

func (c *Client) paperFromSciDB(ctx context.Context, doc *goquery.Document, doi, scidbURL, base string) (*Paper, error) {
	downloadURL, hash, err := parseSciDBFile(doc, scidbURL)
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
	if title := strings.TrimSpace(detail.Find("title").First().Text()); title != "" {
		if idx := strings.Index(title, " - Anna"); idx > 0 {
			paper.Title = strings.TrimSpace(title[:idx])
		}
	}
	if description, ok := detail.Find("meta[name=description]").Attr("content"); ok {
		parts := strings.Split(description, "\n\n")
		switch {
		case len(parts) >= 3:
			paper.Journal = strings.TrimSpace(parts[2])
		case len(parts) >= 2:
			paper.Journal = strings.TrimSpace(parts[1])
		default:
			paper.Journal = strings.TrimSpace(description)
		}
	}
	detail.Find(authorSelector).EachWithBreak(func(_ int, element *goquery.Selection) bool {
		paper.Authors = parse.CleanAuthors(element.Parent().Text())
		return paper.Authors == ""
	})
	detail.Find("div.text-gray-500").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		text := strings.TrimSpace(element.Text())
		if strings.Contains(text, "MB") || strings.Contains(text, "KB") {
			paper.Size = text
			return false
		}
		return true
	})
}
