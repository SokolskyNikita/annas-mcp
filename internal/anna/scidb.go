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
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func buildSciDBURL(base, doi string) string {
	u, _ := url.Parse(buildPathURL(base, "/scidb"))
	u.Path += "/" + NormalizeDOI(doi)
	u.RawPath, u.RawQuery, u.Fragment = "", "", ""
	return u.String()
}

func isSciDBDOIPage(page *url.URL, base, doi string) bool {
	want, err := url.Parse(buildSciDBURL(base, doi))
	return err == nil && sameOrigin(page, want) &&
		(strings.EqualFold(page.Path, want.Path) || strings.EqualFold(page.Path, want.Path+"/"))
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
func (c *Client) getSciDBPDF(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	response, err := c.doGet(ctx, client, rawURL)
	if err != nil || response.StatusCode != http.StatusOK {
		return response, err
	}
	reader := bufio.NewReader(response.Body)
	response.Body = &bufferedReadCloser{Reader: reader, closer: response.Body}
	header, err := reader.Peek(5)
	if err != nil || !bytes.Equal(header, []byte("%PDF-")) {
		response.Body.Close()
		return nil, apperr.New(apperr.Upstream, "SciDB download did not contain a PDF")
	}
	return response, nil
}

func (c *Client) resolveSciDBFile(ctx context.Context, base string, paper *Paper) (string, string, error) {
	pageURL := buildSciDBURL(base, paper.DOI)
	doc, finalURL, err := c.fetchDocument(ctx, pageURL)
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
