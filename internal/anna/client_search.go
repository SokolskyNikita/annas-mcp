package anna

import (
	"context"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func (c *Client) search(ctx context.Context, query string, options SearchOptions, defaultContent string) ([]*Book, string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, "", apperr.New(apperr.InvalidArgument, "search query is empty")
	}
	if err := c.requireArchiveAccess(ctx); err != nil {
		return nil, "", err
	}
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return nil, "", err
	}
	options = ResolveSearch(options, defaultContent)
	pageURL := buildSearchURL(base, query, options)
	doc, _, err := c.fetchDocument(ctx, pageURL)
	if err != nil {
		return nil, pageURL, err
	}
	books := parseBooks(doc, pageURL)
	if len(books) == 0 && !isArchiveDocument(doc) {
		c.invalidateMirror(pageURL)
		return nil, pageURL, apperr.New(apperr.UpstreamBlocked, "archive returned an unexpected page; check the mirror and account cookie")
	}
	return books, pageURL, nil
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
		paper.Authors = strings.TrimSpace(element.Parent().Text())
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
	return paper, nil
}
