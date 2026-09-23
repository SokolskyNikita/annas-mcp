package anna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func (c *Client) search(ctx context.Context, query string, options SearchOptions, defaultContent string, timeout time.Duration) ([]*Book, string, error) {
	if strings.TrimSpace(query) == "" {
		return nil, "", apperr.New(apperr.InvalidArgument, "search query is empty")
	}
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return nil, "", err
	}
	options = ResolveSearch(options, defaultContent)
	pageURL := buildSearchURL(base, query, options)
	doc, _, err := c.fetchDocument(ctx, timeout, pageURL)
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

func (c *Client) lookupDOI(ctx context.Context, doi string, timeout time.Duration) (*Paper, error) {
	doi = NormalizeDOI(doi)
	if doi == "" {
		return nil, apperr.New(apperr.InvalidArgument, "doi is empty")
	}
	if !validDOIPattern.MatchString(doi) {
		return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("invalid DOI: %s", doi))
	}
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return nil, err
	}
	scidbURL := buildPathURL(base, "/scidb/"+doi)
	doc, finalURL, err := c.fetchDocument(ctx, timeout, scidbURL)
	if err != nil && apperr.CodeOf(err) != apperr.NotFound {
		return nil, fmt.Errorf("failed to lookup DOI: %w", err)
	}
	if doc != nil && finalURL != nil && strings.Contains(finalURL.Path, "/scidb/") {
		paper, sciErr := c.paperFromSciDB(ctx, doc, doi, scidbURL, base, timeout)
		if sciErr == nil {
			return paper, nil
		}
	} else if doc != nil {
		page := scidbURL
		if finalURL != nil {
			page = finalURL.String()
		}
		if book := selectDOIHit(parseBooks(doc, page), doi); book != nil {
			return paperFromBook(book, doi), nil
		}
	}

	paper, err := c.searchForDOI(ctx, doi, timeout)
	if err != nil {
		return nil, err
	}
	if paper != nil {
		return paper, nil
	}
	title, titleErr := c.titleFromDOI(ctx, doi, timeout)
	if titleErr == nil && title != "" {
		for _, index := range []string{"", "journals"} {
			books, _, searchErr := c.search(ctx, title, SearchOptions{Index: index, Page: 1}, "", timeout)
			if searchErr != nil {
				continue
			}
			if book := selectTitleHit(books, title); book != nil {
				return paperFromBook(book, doi), nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, apperr.New(apperr.NotFound, fmt.Sprintf("no paper found for DOI: %s", doi))
}

func (c *Client) searchForDOI(ctx context.Context, doi string, timeout time.Duration) (*Paper, error) {
	queries := []string{doi}
	if id := arxivID(doi); id != "" {
		queries = append(queries, id)
	}
	var lastErr error
	for _, query := range queries {
		for _, index := range []string{"journals", ""} {
			books, _, err := c.search(ctx, query, SearchOptions{Index: index, Page: 1}, "", timeout)
			if err != nil {
				lastErr = err
				continue
			}
			if book := selectDOIHit(books, doi); book != nil {
				return paperFromBook(book, doi), nil
			}
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, nil
}

func (c *Client) titleFromDOI(ctx context.Context, doi string, timeout time.Duration) (string, error) {
	doiURL := &url.URL{Scheme: "https", Host: "doi.org", Path: "/" + doi}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, doiURL.String(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.citationstyles.csl+json")
	request.Header.Set("User-Agent", BrowserUserAgent)
	response, err := c.requestClient(timeout).Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("doi.org returned status %d", response.StatusCode)
	}
	var payload struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return "", err
	}
	return strings.TrimSpace(payload.Title), nil
}

func (c *Client) paperFromSciDB(ctx context.Context, doc *goquery.Document, doi, scidbURL, base string, timeout time.Duration) (*Paper, error) {
	if doc == nil {
		return nil, errors.New("SciDB response was empty")
	}
	paper := &Paper{DOI: doi, PageURL: scidbURL}
	doc.Find("a[href^='/md5/']").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		href, _ := element.Attr("href")
		hash := strings.TrimPrefix(href, "/md5/")
		if hash == "" {
			return true
		}
		paper.Hash = hash
		return false
	})
	if paper.Hash == "" {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, apperr.New(apperr.NotFound, fmt.Sprintf("no paper found for DOI: %s", doi))
	}
	detailURL := buildPathURL(base, "/md5/"+paper.Hash)
	detail, _, err := c.fetchDocument(ctx, timeout, detailURL)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		paper.DownloadURL = fmt.Sprintf("/scidb?doi=%s", url.QueryEscape(doi))
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
	paper.DownloadURL = fmt.Sprintf("/scidb?doi=%s", url.QueryEscape(doi))
	return paper, nil
}
