package anna

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/match"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func (c *Client) lookupDOI(ctx context.Context, input string) (*Paper, error) {
	doi := parse.NormalizeDOI(input)
	if doi == "" {
		return nil, apperr.New(apperr.InvalidArgument, "doi is empty")
	}
	if !parse.ValidDOI(doi) {
		return nil, apperr.New(apperr.InvalidArgument, fmt.Sprintf("invalid DOI: %s", doi))
	}
	if err := c.requireArchiveAccess(ctx); err != nil {
		return nil, err
	}
	// Without a session or a mirror, every step below would fail the same way.
	if _, err := c.baseURLFor(ctx); err != nil {
		return nil, err
	}

	var paper *Paper
	scidbErr := c.onMirror(ctx, func(base string) error {
		var err error
		paper, err = c.paperFromSciDBPage(ctx, base, doi)
		return err
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scidbErr == nil {
		return paper, nil
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
	if metadata.DOI != "" && !strings.EqualFold(parse.NormalizeDOI(metadata.DOI), doi) {
		return nil, apperr.New(apperr.Upstream, "DOI metadata resolved to a different DOI")
	}

	queries := make([]string, 0, 3)
	if metadata.Title != "" {
		queries = append(queries, metadata.Title)
	}
	queries = append(queries, doi)
	if id := parse.ArxivID(doi); id != "" {
		queries = append(queries, id)
	}
	// Each query's candidates are checked before the next query is searched,
	// so a paper the title search finds costs no further searches. The order
	// of checking, and so the result, is the same as checking all at the end.
	seenQueries := make(map[string]struct{}, len(queries))
	seenBooks := make(map[string]struct{})
	checked := 0
	for _, query := range queries {
		key := match.NormalizeTitle(query)
		if key == "" {
			continue
		}
		if _, seen := seenQueries[key]; seen {
			continue
		}
		seenQueries[key] = struct{}{}
		var candidates []*Book
		for _, results := range c.searchIndexes(ctx, query) {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if results.err != nil {
				noteFailure(results.err)
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
				// mention in its search parse.Snippet.
				if metadata.Title != "" && match.NormalizeTitle(book.Title) != match.NormalizeTitle(metadata.Title) {
					continue
				}
				if _, seen := seenBooks[book.Hash]; seen {
					continue
				}
				// A common title may have many identical editions. Keep DOI
				// lookup bounded to a small set of likely record pages.
				if checked+len(candidates) >= maxDOICandidates {
					break
				}
				seenBooks[book.Hash] = struct{}{}
				candidates = append(candidates, book)
			}
		}
		paper, err := c.verifyCandidates(ctx, doi, metadata, candidates, noteFailure)
		if err != nil || paper != nil {
			return paper, err
		}
		if checked += len(candidates); checked >= maxDOICandidates {
			break
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
	return nil, apperr.New(apperr.NotFound, fmt.Sprintf("no verified archive record found for DOI: %s. The archive may hold another version of the paper, such as the published one under its own DOI: search article_search by the title and download a match by its hash", doi))
}

// paperFromSciDBPage reads the SciDB page for doi. A canonical SciDB record
// path is an authoritative DOI-to-file mapping. Mirrors sometimes redirect
// this route to a generic search page, so its contents only count when the
// final URL is still the exact requested path.
func (c *Client) paperFromSciDBPage(ctx context.Context, base, doi string) (*Paper, error) {
	doc, finalURL, err := c.fetchSciDBPage(ctx, buildSciDBURL(base, doi))
	if err != nil {
		return nil, err
	}
	if !isSciDBDOIPage(finalURL, base, doi) {
		return nil, apperr.New(apperr.NotFound, "SciDB has no page for this DOI")
	}
	return c.paperFromSciDB(ctx, doc, doi, finalURL.String(), base)
}

const (
	// maxDOICandidates bounds how many record pages one DOI lookup reads.
	maxDOICandidates = 12
	// doiRecordChecks is how many record pages are read at once.
	doiRecordChecks = 4
)

// indexResults is one index's search for a DOI lookup.
type indexResults struct {
	searchResults
	err error
}

// searchIndexes searches the main and journal indexes for query at once, and
// returns them in that order.
func (c *Client) searchIndexes(ctx context.Context, query string) [2]indexResults {
	var results [2]indexResults
	var wait sync.WaitGroup
	for i, index := range []string{"", "journals"} {
		wait.Go(func() {
			results[i].searchResults, results[i].err = c.search(ctx, query, SearchOptions{Index: index, Page: 1}, "")
		})
	}
	wait.Wait()
	return results
}

// verifyCandidates reads the candidates' record pages a few at a time and
// returns the first, in candidate order, whose record carries the DOI. It
// returns as soon as that candidate and every one before it have been read,
// and cancels the rest. It returns an error only when ctx ends.
func (c *Client) verifyCandidates(ctx context.Context, doi string, metadata parse.Citation, candidates []*Book, noteFailure func(error)) (*Paper, error) {
	type checked struct {
		identity parse.Identity
		err      error
	}
	for len(candidates) > 0 {
		batch := candidates[:min(doiRecordChecks, len(candidates))]
		candidates = candidates[len(batch):]
		batchCtx, cancel := context.WithCancel(ctx)
		results := make([]chan checked, len(batch))
		for i, book := range batch {
			results[i] = make(chan checked, 1)
			go func() {
				identity, err := c.archiveIdentityForBook(batchCtx, book)
				results[i] <- checked{identity, err}
			}()
		}
		for i, book := range batch {
			var result checked
			select {
			case result = <-results[i]:
			case <-ctx.Done():
				cancel()
				return nil, ctx.Err()
			}
			if result.err != nil {
				noteFailure(result.err)
				continue
			}
			if match.SameWork(doi, metadata, result.identity) {
				cancel()
				return paperFromBook(book, doi), nil
			}
		}
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (c *Client) archiveIdentityForBook(ctx context.Context, book *Book) (parse.Identity, error) {
	if book == nil || book.Hash == "" {
		return parse.Identity{}, apperr.New(apperr.NotFound, "archive search result has no record hash")
	}
	hash, err := normalizeHash(book.Hash)
	if err != nil {
		return parse.Identity{}, err
	}
	var identity parse.Identity
	err = c.onMirror(ctx, func(base string) error {
		var err error
		identity, err = c.archiveIdentityOn(ctx, base, hash)
		return err
	})
	return identity, err
}

func (c *Client) archiveIdentityOn(ctx context.Context, base, hash string) (parse.Identity, error) {
	requestedURL, err := url.Parse(buildPathURL(base, "/md5/"+hash))
	if err != nil {
		return parse.Identity{}, err
	}
	if requestedURL.Scheme != "https" || requestedURL.Host == "" || requestedURL.User != nil ||
		strings.TrimSuffix(requestedURL.Path, "/") != "/md5/"+hash {
		return parse.Identity{}, apperr.New(apperr.NotFound, "archive search result has an invalid record URL")
	}
	doc, finalURL, err := c.fetchDocument(ctx, requestedURL.String())
	if err != nil {
		return parse.Identity{}, err
	}
	if finalURL == nil || finalURL.User != nil || !sameOrigin(finalURL, requestedURL) ||
		strings.TrimSuffix(finalURL.Path, "/") != strings.TrimSuffix(requestedURL.Path, "/") {
		return parse.Identity{}, apperr.New(apperr.NotFound, "archive record redirected away from the selected hash")
	}
	return parse.RecordIdentity(doc), nil
}

// citationMetadataFromDOI reads DOI.org's CSL representation. Search snippets
// are only candidate generators; this metadata is the external work identity
// used to verify the archive's own title and complete author list.
//
// DOI.org has its own deadline: the archive can find the paper without it, so
// a DOI.org that hangs must not use up the whole lookup.
func (c *Client) citationMetadataFromDOI(ctx context.Context, doi string) (parse.Citation, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, c.doiTimeout)
	defer cancel()
	metadata, err := c.fetchCitationMetadata(ctx, doi)
	if err != nil && parent.Err() == nil && ctx.Err() != nil {
		err = fmt.Errorf("doi.org did not answer within %s", c.doiTimeout)
	}
	return metadata, err
}

func (c *Client) fetchCitationMetadata(ctx context.Context, doi string) (parse.Citation, error) {
	metadataURL := &url.URL{Scheme: "https", Host: "doi.org", Path: "/" + doi}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return parse.Citation{}, err
	}
	request.Header.Set("Accept", "application/vnd.citationstyles.csl+json")
	request.Header.Set("User-Agent", BrowserUserAgent)
	response, err := c.requestClient(0).Do(request)
	if err != nil {
		return parse.Citation{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return parse.Citation{}, fmt.Errorf("doi.org returned status %d", response.StatusCode)
	}
	return parse.ParseCitation(response.Body)
}

// titleFromDOI remains as a small compatibility helper for package callers
// and tests; DOI lookup itself uses the complete citation identity.
func (c *Client) titleFromDOI(ctx context.Context, doi string) (string, error) {
	metadata, err := c.citationMetadataFromDOI(ctx, doi)
	return metadata.Title, err
}
