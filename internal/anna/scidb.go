package anna

import (
	"bufio"
	"bytes"
	"context"
	"errors"
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
	downloadURL, hash, err := sciDBFile(doc, finalURL.String())
	if err != nil {
		return "", "", err
	}
	if paper.Hash != "" && !strings.EqualFold(hash, paper.Hash) {
		return "", "", apperr.New(apperr.Upstream, fmt.Sprintf("SciDB file does not match the selected archive hash %s", paper.Hash))
	}
	return downloadURL, hash, nil
}

// sciDBFile reads a SciDB page's file and record hash. A page without them
// is NOT_FOUND; a page URL that is not HTTPS is the upstream's fault.
func sciDBFile(doc *goquery.Document, pageURL string) (downloadURL, hash string, err error) {
	downloadURL, hash, err = parse.SciDBFile(doc, pageURL)
	switch {
	case errors.Is(err, parse.ErrSciDBPageURL):
		return "", "", apperr.New(apperr.Upstream, err.Error())
	case err != nil:
		return "", "", apperr.New(apperr.NotFound, err.Error())
	}
	return downloadURL, hash, nil
}
