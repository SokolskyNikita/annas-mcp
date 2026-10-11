package anna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/env"
)

var domainIndexes = []int{0, 1, 2}

// maxDownloadPaths bounds how many of a file's collections are tried when
// it is stored in more than one. Only the first API answer for a file counts
// against the account's allowance, so later collections cost time, not
// downloads.
const maxDownloadPaths = 3

// Anna's API gives this error for a path or domain index beyond what it has
// for the file. At the first index of both, the file has no fast download
// copy at all, so trying the other servers cannot help.
var errNoFastDownload = apperr.New(apperr.NotFound, "Anna's Archive has no fast download for this file; choose another copy from search")

const noFastDownloadAPIError = "invalid domain_index or path_index"

// stopDownload marks an API answer about the account or the file that every
// other server and collection would repeat.
type stopDownload struct{ error }

func (e stopDownload) Unwrap() error { return e.error }

// fastDownloadGrant is a download URL and the account allowance the API
// reported with it.
type fastDownloadGrant struct {
	url     string
	account *fastDownloadAccounts
}

func (c *Client) validateDownload(ctx context.Context, format string) error {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	if format != "" {
		if _, err := normalizeFormat(format); err != nil {
			return err
		}
	}
	if c.config.DownloadPath == "" {
		return apperr.New(apperr.Config, "set ANNAS_DOWNLOAD_PATH to an absolute folder path; the default folder in your home folder is unavailable because the home folder could not be determined")
	}
	if !filepath.IsAbs(c.config.DownloadPath) {
		return apperr.New(apperr.Config, "ANNAS_DOWNLOAD_PATH must be an absolute folder path")
	}
	if strings.TrimSpace(c.config.SecretKey) == "" {
		return apperr.New(apperr.Config, "ANNAS_SECRET_KEY is required for downloads")
	}
	return prepareDownloadFolder(c.config.DownloadPath)
}

// prepareDownloadFolder creates the download folder and checks that a file
// can be written there, so a folder problem is reported before anything is
// downloaded rather than once per download server.
func prepareDownloadFolder(path string) error {
	err := os.MkdirAll(path, 0o755)
	if err == nil {
		var probe *os.File
		if probe, err = os.CreateTemp(path, ".annas-mcp-write-test-*"); err == nil {
			_ = probe.Close()
			return os.Remove(probe.Name())
		}
	}
	// The path is already in the message; keep only the reason.
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		err = pathErr.Err
	}
	err = apperr.Wrap(apperr.Config, "cannot write to the download folder "+path, err)
	if !errors.Is(err, fs.ErrPermission) {
		return err
	}
	advice := "Set ANNAS_DOWNLOAD_PATH to a folder you can write to."
	if runtime.GOOS == "darwin" {
		advice = "On macOS, Desktop, Documents, Downloads and iCloud Drive need permission: allow your MCP client (for example Claude or Cursor) in System Settings > Privacy & Security > Files and Folders, or set ANNAS_DOWNLOAD_PATH to a folder directly in your home folder, such as ~/" + env.DefaultDownloadFolder + "."
	}
	return fmt.Errorf("%w. %s", err, advice)
}

func (c *Client) downloadBook(ctx context.Context, book *Book, progress ProgressFunc) (DownloadResult, error) {
	if book == nil {
		return DownloadResult{}, apperr.New(apperr.InvalidArgument, "book is required")
	}
	hash, err := normalizeHash(book.Hash)
	if err != nil {
		return DownloadResult{}, err
	}
	if err := c.validateDownload(ctx, book.Format); err != nil {
		return DownloadResult{}, err
	}
	client := c.requestClient(0)
	var attempts []error
	var account *fastDownloadAccounts
collections:
	for pathIndex := 0; pathIndex < maxDownloadPaths; pathIndex++ {
		// Another collection only helps when this one's files failed. When the
		// API itself failed, the next collection would ask the same API.
		granted := false
	servers:
		for _, domainIndex := range domainIndexes {
			if err := ctx.Err(); err != nil {
				return DownloadResult{}, failedDownload(attempts, err)
			}
			base, err := c.baseURLFor(ctx)
			if err != nil {
				return DownloadResult{}, failedDownload(attempts, err)
			}
			server := fastServerLabel(pathIndex, domainIndex)
			// A mirror that fails here is retried on another mirror for
			// the same server, so it does not use up one of the servers.
			var grant fastDownloadGrant
			err = c.onMirrorFrom(ctx, base, func(base string) error {
				var err error
				grant, err = c.resolveDownloadURL(ctx, client, base, hash, c.config.SecretKey, pathIndex, domainIndex)
				return err
			})
			if errors.Is(err, errNoFastDownload) {
				switch {
				case pathIndex == 0 && domainIndex == 0:
					attempts = append(attempts, fmt.Errorf("%s (API): %w", server, err))
					break collections
				case domainIndex == 0:
					// The file is in fewer collections than this.
					break collections
				default:
					// Fewer download servers than domainIndexes.
					break servers
				}
			}
			if err != nil {
				attempts = append(attempts, fmt.Errorf("%s (API): %w", server, redactErr(err, c.config.SecretKey)))
				if errors.As(err, new(stopDownload)) {
					break collections
				}
				continue
			}
			granted = true
			if grant.account != nil {
				account = grant.account
			}
			result, err := downloadFileWithGetter(ctx, client, grant.url, c.config.DownloadPath, book.Title, book.Format, hash, progress, c.doGetWith)
			if err == nil {
				result.DownloadsLeft = downloadsLeftAfter(account, hash)
				return result, nil
			}
			attempts = append(attempts, fmt.Errorf("%s (file): %w", server, redactErr(err, c.config.SecretKey)))
		}
		if !granted {
			break
		}
	}
	return DownloadResult{}, failedDownload(attempts, ctx.Err())
}

// fastServerLabel names an attempt in diagnostics. Most files are in one
// collection, so the first keeps the short form.
func fastServerLabel(pathIndex, domainIndex int) string {
	if pathIndex == 0 {
		return fmt.Sprintf("fast server %d", domainIndex+1)
	}
	return fmt.Sprintf("collection %d, fast server %d", pathIndex+1, domainIndex+1)
}

// downloadsLeftAfter adjusts the allowance the API read before counting this
// download: a file fetched recently is free, anything else uses one.
func downloadsLeftAfter(account *fastDownloadAccounts, hash string) *int {
	if account == nil {
		return nil
	}
	left := account.DownloadsLeft
	if !slices.Contains(account.RecentlyDownloadedMD5s, hash) {
		left = max(0, left-1)
	}
	return &left
}

func (c *Client) downloadArticle(ctx context.Context, options ArticleDownloadOptions, progress ProgressFunc) (DownloadResult, error) {
	hash, doi := strings.TrimSpace(options.Hash), strings.TrimSpace(options.DOI)
	if (hash == "") == (doi == "") {
		return DownloadResult{}, apperr.New(apperr.InvalidArgument, "pass exactly one of doi or hash")
	}
	if hash != "" {
		return c.downloadBook(ctx, &Book{Hash: hash, Title: options.Title, Format: options.Format}, progress)
	}
	if _, ok := ParseDOI(doi); !ok {
		return DownloadResult{}, apperr.New(apperr.InvalidArgument, "doi must be a valid DOI or DOI URL")
	}
	if err := c.validateDownload(ctx, options.Format); err != nil {
		return DownloadResult{}, err
	}
	paper, err := c.lookupDOI(ctx, doi)
	if err != nil {
		return DownloadResult{}, err
	}
	if options.Title != "" {
		paper.Title = options.Title
	}
	var fastErr error
	if paper.Hash != "" {
		var result DownloadResult
		result, fastErr = c.downloadBook(ctx, &Book{Hash: paper.Hash, Title: paper.Title, Format: options.Format}, progress)
		if fastErr == nil {
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		if fastErr != nil {
			var fastAttempts *downloadAttemptsError
			if errors.As(fastErr, &fastAttempts) {
				return DownloadResult{}, failedDownload(fastAttempts.attempts, err)
			}
			return DownloadResult{}, failedDownload([]error{fmt.Errorf("fast download: %w", fastErr)}, err)
		}
		return DownloadResult{}, err
	}
	result, err := c.downloadPaper(ctx, paper, options.Format, progress)
	if err != nil {
		var attempts []error
		var fastAttempts *downloadAttemptsError
		if errors.As(fastErr, &fastAttempts) {
			attempts = append(attempts, fastAttempts.attempts...)
		} else if fastErr != nil {
			attempts = append(attempts, fmt.Errorf("fast download: %w", fastErr))
		}
		attempts = append(attempts, fmt.Errorf("SciDB fallback: %w", redactErr(err, c.config.SecretKey)))
		return DownloadResult{}, failedDownload(attempts, ctx.Err())
	}
	return result, err
}

func (c *Client) downloadPaper(ctx context.Context, paper *Paper, format string, progress ProgressFunc) (DownloadResult, error) {
	if paper == nil {
		return DownloadResult{}, apperr.New(apperr.InvalidArgument, "paper is required")
	}
	if err := c.validateDownload(ctx, format); err != nil {
		return DownloadResult{}, err
	}
	expectedMD5 := ""
	var err error
	if paper.Hash != "" {
		expectedMD5, err = normalizeHash(paper.Hash)
		if err != nil {
			return DownloadResult{}, err
		}
	}
	var base string
	sourceURL := paper.DownloadURL
	if sourceURL == "" {
		if _, valid := ParseDOI(paper.DOI); !valid {
			return DownloadResult{}, apperr.New(apperr.NotFound, "no DOI is available for SciDB lookup")
		}
		err = c.onMirror(ctx, func(mirrorBase string) error {
			base = mirrorBase
			var err error
			sourceURL, expectedMD5, err = c.resolveSciDBFile(ctx, base, paper)
			return err
		})
	} else {
		base, err = c.baseURLFor(ctx)
	}
	if err != nil {
		return DownloadResult{}, err
	}
	downloadURL, err := resolvePaperDownloadURL(base, sourceURL)
	if err != nil {
		return DownloadResult{}, err
	}
	name := paper.Title
	if name == "" {
		name = paper.DOI
	}
	if format == "" {
		format = "pdf"
	}
	return downloadFileWithGetter(ctx, c.requestClient(0), downloadURL, c.config.DownloadPath, name, format, expectedMD5, progress, c.getSciDBPDF)
}

func (c *Client) resolveDownloadURL(ctx context.Context, client *http.Client, base, hash, secretKey string, pathIndex, domainIndex int) (fastDownloadGrant, error) {
	values := url.Values{}
	values.Set("md5", hash)
	values.Set("key", secretKey)
	values.Set("path_index", strconv.Itoa(pathIndex))
	values.Set("domain_index", strconv.Itoa(domainIndex))
	apiURL := buildPathURL(base, "/dyn/api/fast_download.json") + "?" + values.Encode()
	response, err := c.doGet(ctx, client, apiURL)
	if err != nil {
		return fastDownloadGrant{}, redactErr(requestFailure(err, apiURL), secretKey)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return fastDownloadGrant{}, fmt.Errorf("could not read API response from %s: %w", responseHost(response, apiURL), err)
	}
	var payload fastDownloadResponse
	jsonErr := json.Unmarshal(body, &payload)
	apiError := strings.TrimSpace(payload.Error)
	if jsonErr != nil || (response.StatusCode != http.StatusOK && apiError == "") {
		// Not the API's own answer: a proxy, browser check or outage.
		switch {
		case response.StatusCode == http.StatusForbidden:
			return fastDownloadGrant{}, apperr.New(apperr.UpstreamBlocked, httpResponseError(response, apiURL).Error()+"; check account access and credentials")
		case response.StatusCode != http.StatusOK:
			return fastDownloadGrant{}, httpResponseError(response, apiURL)
		default:
			return fastDownloadGrant{}, fmt.Errorf("invalid JSON from %s (HTTP status %d): %w", responseHost(response, apiURL), response.StatusCode, jsonErr)
		}
	}
	if apiError != "" {
		if response.StatusCode == http.StatusBadRequest && strings.EqualFold(apiError, noFastDownloadAPIError) {
			return fastDownloadGrant{}, errNoFastDownload
		}
		return fastDownloadGrant{}, fastDownloadRejection(response, apiURL, redactErr(errors.New(apiError), secretKey).Error())
	}
	if payload.DownloadURL == nil || strings.TrimSpace(*payload.DownloadURL) == "" {
		return fastDownloadGrant{}, fmt.Errorf("API at %s returned an empty download URL", responseHost(response, apiURL))
	}
	downloadURL := strings.TrimSpace(*payload.DownloadURL)
	parsed, err := url.Parse(downloadURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fastDownloadGrant{}, fmt.Errorf("API at %s returned an invalid download URL", responseHost(response, apiURL))
	}
	return fastDownloadGrant{url: parsed.String(), account: payload.AccountInfo}, nil
}

// fastDownloadRejection classifies the API's own error answers. Those about
// the account or the file stop the download; the rest may be one server's.
func fastDownloadRejection(response *http.Response, apiURL, apiError string) error {
	switch response.StatusCode {
	case http.StatusTooManyRequests:
		return stopDownload{apperr.New(apperr.UpstreamBlocked, "no fast downloads left: this membership's allowance for the rolling window is used up. Files downloaded recently can still be downloaded again; account_status shows the allowance")}
	case http.StatusUnauthorized:
		return stopDownload{apperr.New(apperr.Config, "Anna's Archive rejected ANNAS_SECRET_KEY for fast downloads: copy the secret key from your Anna's Archive account page")}
	case http.StatusForbidden:
		return stopDownload{apperr.New(apperr.UpstreamBlocked, "fast downloads need an active membership, and Anna's Archive says this account has none")}
	case http.StatusNotFound:
		return stopDownload{apperr.New(apperr.NotFound, "Anna's Archive has no file with this hash; choose a hash from search")}
	}
	return fmt.Errorf("API rejected request at %s (HTTP status %d): %s", responseHost(response, apiURL), response.StatusCode, apiErrorMessage(apiError))
}
