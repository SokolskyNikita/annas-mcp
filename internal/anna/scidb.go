package anna

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// buildSciDBURL is the SciDB page for doi in the form SciDB redirects to:
// lowercase with a trailing slash. Asking for it directly saves up to four
// redirects, each of which takes the archive seconds to answer.
func buildSciDBURL(base, doi string) string {
	u, _ := url.Parse(buildPathURL(base, "/scidb"))
	u.Path += "/" + strings.ToLower(parse.NormalizeDOI(doi)) + "/"
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String()
}

func isSciDBDOIPage(page *url.URL, base, doi string) bool {
	want, err := url.Parse(buildSciDBURL(base, doi))
	return err == nil && page != nil && sameOrigin(page, want) &&
		strings.EqualFold(strings.TrimSuffix(page.Path, "/"), strings.TrimSuffix(want.Path, "/"))
}

// fetchSciDBPage reads a SciDB page without following a redirect off SciDB.
// For a DOI it does not have, SciDB redirects to a search for the DOI, which
// costs another slow request and is not the paper's page anyway.
func (c *Client) fetchSciDBPage(ctx context.Context, rawURL string) (*goquery.Document, *url.URL, error) {
	client := c.requestClient(0)
	follow := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !strings.HasPrefix(next.URL.Path, "/scidb/") {
			return http.ErrUseLastResponse
		}
		return follow(next, via)
	}
	return c.fetchDocumentWith(ctx, client, rawURL)
}

// SciDB embeds its PDF in the same-origin PDF.js viewer. Only read that
// viewer's file parameter, rather than guessing from arbitrary page links.
func parseSciDBFile(doc *goquery.Document, pageURL string) (downloadURL, hash string, err error) {
	if doc == nil {
		return "", "", apperr.New(apperr.NotFound, "SciDB response was empty")
	}
	page, parseErr := url.Parse(pageURL)
	if parseErr != nil || page.Scheme != "https" || page.Host == "" || page.User != nil {
		return "", "", apperr.New(apperr.Upstream, "invalid SciDB page URL")
	}
	files := make(map[string]struct{})
	doc.Find("iframe[src]").Each(func(_ int, frame *goquery.Selection) {
		src, _ := frame.Attr("src")
		ref, parseErr := url.Parse(src)
		if parseErr != nil {
			return
		}
		viewer := page.ResolveReference(ref)
		if !sameOrigin(page, viewer) || viewer.User != nil || viewer.Path != "/pdfjs/web/viewer.html" {
			return
		}
		file, resolveErr := resolvePaperDownloadURL(pageURL, viewer.Query().Get("file"))
		if resolveErr == nil {
			files[file] = struct{}{}
		}
	})
	if len(files) != 1 {
		return "", "", apperr.New(apperr.NotFound, "SciDB did not provide one usable PDF viewer link")
	}
	hashes := make(map[string]struct{})
	doc.Find("a[href^='/md5/']").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		if md5, normalizeErr := normalizeHash(strings.TrimPrefix(href, "/md5/")); normalizeErr == nil {
			hashes[md5] = struct{}{}
		}
	})
	if len(hashes) != 1 {
		return "", "", apperr.New(apperr.NotFound, "SciDB did not identify one archive file for checksum verification")
	}
	for file := range files {
		downloadURL = file
	}
	for md5 := range hashes {
		hash = md5
	}
	return downloadURL, hash, nil
}

// A successful SciDB transfer must contain a PDF even when the CDN serves it
// as application/octet-stream. Preserve the prefix for the streaming hasher.
// A resumed transfer starts mid-file, so it has no prefix to check.
func (c *Client) getSciDBPDF(ctx context.Context, client *http.Client, rawURL string, header http.Header) (*http.Response, error) {
	response, err := c.doGetWith(ctx, client, rawURL, header)
	if err != nil || response.StatusCode != http.StatusOK || header.Get("Range") != "" {
		return response, err
	}
	reader := bufio.NewReader(response.Body)
	response.Body = &bufferedReadCloser{Reader: reader, closer: response.Body}
	prefix, err := reader.Peek(5)
	if err != nil || !bytes.Equal(prefix, []byte("%PDF-")) {
		response.Body.Close()
		return nil, apperr.New(apperr.Upstream, "SciDB download did not contain a PDF")
	}
	return response, nil
}

func (c *Client) resolveSciDBFile(ctx context.Context, base string, paper *Paper) (string, string, error) {
	pageURL := buildSciDBURL(base, paper.DOI)
	doc, finalURL, err := c.fetchSciDBPage(ctx, pageURL)
	if err != nil {
		return "", "", err
	}
	if !isSciDBDOIPage(finalURL, base, paper.DOI) {
		return "", "", apperr.New(apperr.NotFound, "SciDB redirected without providing the requested paper's PDF")
	}
	downloadURL, hash, err := parseSciDBFile(doc, finalURL.String())
	if err != nil {
		return "", "", err
	}
	if paper.Hash != "" && !strings.EqualFold(hash, paper.Hash) {
		return "", "", apperr.New(apperr.Upstream, fmt.Sprintf("SciDB file does not match the selected archive hash %s", paper.Hash))
	}
	return downloadURL, hash, nil
}
