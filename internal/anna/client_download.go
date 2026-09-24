package anna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

var domainIndexes = []int{0, 1, 2}

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
	if c.config.DownloadPath == "" || !filepath.IsAbs(c.config.DownloadPath) {
		return apperr.New(apperr.Config, "ANNAS_DOWNLOAD_PATH must be an absolute directory path")
	}
	if strings.TrimSpace(c.config.SecretKey) == "" {
		return apperr.New(apperr.Config, "ANNAS_SECRET_KEY is required for downloads")
	}
	if strings.TrimSpace(c.config.AccountCookie) == "" {
		return apperr.New(apperr.Config, "ANNAS_ACCOUNT_COOKIE is required for archive access")
	}
	return nil
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
	for _, domainIndex := range domainIndexes {
		if err := ctx.Err(); err != nil {
			return DownloadResult{}, failedDownload(attempts, err)
		}
		base, err := c.baseURLFor(ctx)
		if err != nil {
			return DownloadResult{}, failedDownload(attempts, err)
		}
		downloadURL, err := c.resolveDownloadURL(ctx, client, base, hash, c.config.SecretKey, domainIndex)
		if err != nil {
			attempts = append(attempts, fmt.Errorf("fast server %d (API): %w", domainIndex+1, redactErr(err, c.config.SecretKey)))
			continue
		}
		result, err := downloadFileWithGetter(ctx, client, downloadURL, c.config.DownloadPath, book.Title, book.Format, hash, progress, c.doGet)
		if err == nil {
			return result, nil
		}
		attempts = append(attempts, fmt.Errorf("fast server %d (file): %w", domainIndex+1, redactErr(err, c.config.SecretKey)))
	}
	return DownloadResult{}, failedDownload(attempts, ctx.Err())
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
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return DownloadResult{}, err
	}
	sourceURL := paper.DownloadURL
	if sourceURL == "" {
		if _, valid := ParseDOI(paper.DOI); !valid {
			return DownloadResult{}, apperr.New(apperr.NotFound, "no DOI is available for SciDB lookup")
		}
		sourceURL, expectedMD5, err = c.resolveSciDBFile(ctx, base, paper)
		if err != nil {
			return DownloadResult{}, err
		}
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

func (c *Client) resolveDownloadURL(ctx context.Context, client *http.Client, base, hash, secretKey string, domainIndex int) (string, error) {
	values := url.Values{}
	values.Set("md5", hash)
	values.Set("key", secretKey)
	values.Set("path_index", "0")
	values.Set("domain_index", fmt.Sprintf("%d", domainIndex))
	apiURL := buildPathURL(base, "/dyn/api/fast_download.json") + "?" + values.Encode()
	response, err := c.doGet(ctx, client, apiURL)
	if err != nil {
		return "", redactErr(requestFailure(err, apiURL), secretKey)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden {
		return "", apperr.New(apperr.UpstreamBlocked, httpResponseError(response, apiURL).Error()+"; check account access and credentials")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest {
		return "", httpResponseError(response, apiURL)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("could not read API response from %s: %w", responseHost(response, apiURL), err)
	}
	var payload fastDownloadResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("invalid JSON from %s (HTTP status %d): %w", responseHost(response, apiURL), response.StatusCode, err)
	}
	if payload.Error != "" {
		return "", fmt.Errorf("API rejected request at %s (HTTP status %d): %s", responseHost(response, apiURL), response.StatusCode, apiErrorMessage(redactErr(errors.New(payload.Error), secretKey).Error()))
	}
	if payload.DownloadURL == nil || strings.TrimSpace(*payload.DownloadURL) == "" {
		return "", fmt.Errorf("API at %s returned an empty download URL", responseHost(response, apiURL))
	}
	downloadURL := strings.TrimSpace(*payload.DownloadURL)
	parsed, err := url.Parse(downloadURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("API at %s returned an invalid download URL", responseHost(response, apiURL))
	}
	return parsed.String(), nil
}
