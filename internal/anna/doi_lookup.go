package anna

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func (c *Client) lookupDOI(ctx context.Context, input string) (*Paper, error) {
	doi := NormalizeDOI(input)
	if doi == "" {
		return nil, apperr.New(apperr.InvalidArgument, "doi is empty")
	}
	if !validDOIPattern.MatchString(doi) {
		return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("invalid DOI: %s", doi))
	}
	if err := c.requireArchiveAccess(ctx); err != nil {
		return nil, err
	}
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return nil, err
	}

	// A canonical SciDB record path is an authoritative DOI-to-file mapping.
	// Mirrors sometimes redirect this route to a generic search page, so only
	// accept its contents when the final URL is still the exact requested path.
	scidbURL := buildSciDBURL(base, doi)
	doc, finalURL, scidbErr := c.fetchDocument(ctx, scidbURL)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if doc != nil && isSciDBDOIPage(finalURL, base, doi) {
		paper, parseErr := c.paperFromSciDB(ctx, doc, doi, finalURL.String(), base)
		if parseErr == nil {
			return paper, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		scidbErr = parseErr
	}

	// A miss is only conclusive when every archive request answered: SciDB
	// without the record and each search with a page. Remember the last
	// request that failed instead, so an outage is not reported as the paper
	// being absent. A NOT_FOUND answer is itself one of those misses.
	var failure error
	noteFailure := func(err error) {
		if err != nil && apperr.CodeOf(err) != apperr.NotFound {
			failure = err
		}
	}
	noteFailure(scidbErr)

	metadata, metadataErr := c.citationMetadataFromDOI(ctx, doi)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if metadata.DOI != "" && !strings.EqualFold(NormalizeDOI(metadata.DOI), doi) {
		return nil, apperr.New(apperr.Upstream, "DOI metadata resolved to a different DOI")
	}

	queries := make([]string, 0, 3)
	if metadata.Title != "" {
		queries = append(queries, metadata.Title)
	}
	queries = append(queries, doi)
	if id := arxivID(doi); id != "" {
		queries = append(queries, id)
	}
	seenQueries := make(map[string]struct{}, len(queries))
	seenBooks := make(map[string]struct{})
	var candidates []*Book
	for _, query := range queries {
		key := normalizeTitle(query)
		if key == "" {
			continue
		}
		if _, seen := seenQueries[key]; seen {
			continue
		}
		seenQueries[key] = struct{}{}
		for _, index := range []string{"", "journals"} {
			results, searchErr := c.search(ctx, query, SearchOptions{Index: index, Page: 1}, "")
			if searchErr != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				noteFailure(searchErr)
				continue
			}
			// Partial matches are welcome here: each candidate must still
			// agree on the title and carry the DOI on its record page.
			for _, book := range results.books() {
				if book == nil || book.Hash == "" {
					continue
				}
				// When CSL identity is available, only inspect cards whose own
				// title agrees. A conflicting title cannot be repaired by a DOI
				// mention in its search snippet.
				if metadata.Title != "" && normalizeTitle(book.Title) != normalizeTitle(metadata.Title) {
					continue
				}
				if _, seen := seenBooks[book.Hash]; seen {
					continue
				}
				seenBooks[book.Hash] = struct{}{}
				candidates = append(candidates, book)
				// A common title may have many identical editions. Keep DOI
				// lookup bounded to a small set of likely record pages.
				if len(candidates) >= 12 {
					break
				}
			}
			if len(candidates) >= 12 {
				break
			}
		}
		if len(candidates) >= 12 {
			break
		}
	}

	for _, book := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity, detailErr := c.archiveIdentityForBook(ctx, book)
		if detailErr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			noteFailure(detailErr)
			continue
		}
		if verifyDOIIdentity(doi, metadata, identity) {
			return paperFromBook(book, doi), nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The archive failure keeps its own code (UPSTREAM, UPSTREAM_BLOCKED or
	// REQUEST_TIMEOUT); DOI.org's failure is context only.
	if failure != nil {
		if metadataErr != nil {
			return nil, fmt.Errorf("could not check Anna's Archive for DOI %s (DOI metadata: %v): %w", doi, metadataErr, failure)
		}
		return nil, fmt.Errorf("could not check Anna's Archive for DOI %s: %w", doi, failure)
	}
	return nil, apperr.New(apperr.NotFound, fmt.Sprintf("no verified archive record found for DOI: %s", doi))
}

func (c *Client) archiveIdentityForBook(ctx context.Context, book *Book) (archiveIdentity, error) {
	if book == nil || book.Hash == "" {
		return archiveIdentity{}, apperr.New(apperr.NotFound, "archive search result has no record hash")
	}
	hash, err := normalizeHash(book.Hash)
	if err != nil {
		return archiveIdentity{}, err
	}
	currentBase, err := c.baseURLFor(ctx)
	if err != nil {
		return archiveIdentity{}, err
	}
	requestedURL, err := url.Parse(buildPathURL(currentBase, "/md5/"+hash))
	if err != nil {
		return archiveIdentity{}, err
	}
	if requestedURL.Scheme != "https" || requestedURL.Host == "" || requestedURL.User != nil ||
		strings.TrimSuffix(requestedURL.Path, "/") != "/md5/"+hash {
		return archiveIdentity{}, apperr.New(apperr.NotFound, "archive search result has an invalid record URL")
	}
	doc, finalURL, err := c.fetchDocument(ctx, requestedURL.String())
	if err != nil {
		return archiveIdentity{}, err
	}
	if finalURL == nil || finalURL.User != nil || !sameOrigin(finalURL, requestedURL) ||
		strings.TrimSuffix(finalURL.Path, "/") != strings.TrimSuffix(requestedURL.Path, "/") {
		return archiveIdentity{}, apperr.New(apperr.NotFound, "archive record redirected away from the selected hash")
	}
	return parseArchiveIdentity(doc), nil
}

// citationMetadataFromDOI reads DOI.org's CSL representation. Search snippets
// are only candidate generators; this metadata is the external work identity
// used to verify the archive's own title and complete author list.
//
// DOI.org has its own deadline: the archive can find the paper without it, so
// a DOI.org that hangs must not use up the whole lookup.
func (c *Client) citationMetadataFromDOI(ctx context.Context, doi string) (doiMetadata, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, c.doiTimeout)
	defer cancel()
	metadata, err := c.fetchCitationMetadata(ctx, doi)
	if err != nil && parent.Err() == nil && ctx.Err() != nil {
		err = fmt.Errorf("doi.org did not answer within %s", c.doiTimeout)
	}
	return metadata, err
}

func (c *Client) fetchCitationMetadata(ctx context.Context, doi string) (doiMetadata, error) {
	metadataURL := &url.URL{Scheme: "https", Host: "doi.org", Path: "/" + doi}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return doiMetadata{}, err
	}
	request.Header.Set("Accept", "application/vnd.citationstyles.csl+json")
	request.Header.Set("User-Agent", BrowserUserAgent)
	response, err := c.requestClient(0).Do(request)
	if err != nil {
		return doiMetadata{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return doiMetadata{}, fmt.Errorf("doi.org returned status %d", response.StatusCode)
	}
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return doiMetadata{}, err
	}
	metadata := doiMetadata{
		DOI:     jsonString(payload["DOI"]),
		Title:   jsonStringOrArray(payload["title"]),
		Authors: cslAuthors(payload["author"]),
		Year:    cslYear(payload),
	}
	if metadata.DOI == "" {
		metadata.DOI = jsonString(payload["doi"])
	}
	if metadata.Title == "" {
		return metadata, fmt.Errorf("doi.org metadata had no title")
	}
	return metadata, nil
}

// titleFromDOI remains as a small compatibility helper for package callers
// and tests; DOI lookup itself uses the complete citation identity.
func (c *Client) titleFromDOI(ctx context.Context, doi string) (string, error) {
	metadata, err := c.citationMetadataFromDOI(ctx, doi)
	return metadata.Title, err
}

func jsonString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func jsonStringOrArray(raw json.RawMessage) string {
	if value := jsonString(raw); value != "" {
		return value
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil && len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func cslAuthors(raw json.RawMessage) []string {
	var authors []struct {
		Given   string `json:"given"`
		Family  string `json:"family"`
		Literal string `json:"literal"`
	}
	if json.Unmarshal(raw, &authors) != nil {
		return nil
	}
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		name := strings.TrimSpace(author.Literal)
		if name == "" {
			name = strings.TrimSpace(strings.TrimSpace(author.Given) + " " + strings.TrimSpace(author.Family))
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cslYear(payload map[string]json.RawMessage) string {
	for _, key := range []string{"issued", "published-print", "published-online", "published"} {
		var date struct {
			DateParts [][]int `json:"date-parts"`
		}
		if json.Unmarshal(payload[key], &date) == nil && len(date.DateParts) > 0 && len(date.DateParts[0]) > 0 && date.DateParts[0][0] > 0 {
			return fmt.Sprint(date.DateParts[0][0])
		}
	}
	return ""
}
