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

func (c *Client) validateDownload(format string, requireKey bool) error {
	if format != "" {
		if _, err := normalizeFormat(format); err != nil {
			return err
		}
	}
	if c.config.DownloadPath == "" || !filepath.IsAbs(c.config.DownloadPath) {
		return apperr.New(apperr.Config, "ANNAS_DOWNLOAD_PATH must be an absolute directory path")
	}
	if requireKey && strings.TrimSpace(c.config.SecretKey) == "" {
		return apperr.New(apperr.Config, "ANNAS_SECRET_KEY is required for fast downloads")
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
	if err := c.validateDownload(book.Format, true); err != nil {
		return DownloadResult{}, err
	}
	client := c.requestClient(0)
	var lastErr error
	for _, domainIndex := range domainIndexes {
		if err := ctx.Err(); err != nil {
			return DownloadResult{}, err
		}
		base, err := c.baseURLFor(ctx)
		if err != nil {
			return DownloadResult{}, err
		}
		downloadURL, err := c.resolveDownloadURL(ctx, client, base, hash, c.config.SecretKey, domainIndex)
		if err != nil {
			lastErr = err
			continue
		}
		result, err := downloadFileWithGetter(ctx, client, downloadURL, c.config.DownloadPath, book.Title, book.Format, hash, progress, c.doGet)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if err := ctx.Err(); err != nil {
		return DownloadResult{}, err
	}
	if lastErr == nil {
		lastErr = errors.New("no download server succeeded")
	}
	return DownloadResult{}, fmt.Errorf("download failed: %w", redactErr(lastErr, c.config.SecretKey))
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
	if err := c.validateDownload(options.Format, false); err != nil {
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
	if paper.Hash != "" && c.config.SecretKey != "" {
		var result DownloadResult
		result, fastErr = c.downloadBook(ctx, &Book{Hash: paper.Hash, Title: paper.Title, Format: options.Format}, progress)
		if fastErr == nil {
			return result, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return DownloadResult{}, err
	}
	result, err := c.downloadPaper(ctx, paper, options.Format, progress)
	if err != nil && fastErr != nil {
		return DownloadResult{}, fmt.Errorf("fast download failed (%v); SciDB fallback failed: %w", redactErr(fastErr, c.config.SecretKey), err)
	}
	return result, err
}

func (c *Client) downloadPaper(ctx context.Context, paper *Paper, format string, progress ProgressFunc) (DownloadResult, error) {
	if paper == nil {
		return DownloadResult{}, apperr.New(apperr.InvalidArgument, "paper is required")
	}
	if err := c.validateDownload(format, false); err != nil {
		return DownloadResult{}, err
	}
	if paper.DownloadURL == "" {
		return DownloadResult{}, apperr.New(apperr.NotFound, "no download URL is available for this paper")
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
	downloadURL, err := resolvePaperDownloadURL(base, paper.DownloadURL)
	if err != nil {
		return DownloadResult{}, err
	}
	name := paper.Title
	if name == "" {
		name = paper.DOI
	}
	return downloadFileWithGetter(ctx, c.requestClient(0), downloadURL, c.config.DownloadPath, name, format, expectedMD5, progress, c.doGet)
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
		return "", redactErr(err, secretKey)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if response.StatusCode == http.StatusForbidden {
		return "", apperr.New(apperr.UpstreamBlocked, "fast download API returned 403; check account access and credentials")
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest {
		return "", fmt.Errorf("fast download API failed with status %d for domain_index=%d", response.StatusCode, domainIndex)
	}
	var payload fastDownloadResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("domain_index=%d: failed to decode API response: %w", domainIndex, err)
	}
	if payload.Error != "" {
		return "", fmt.Errorf("domain_index=%d: %s", domainIndex, payload.Error)
	}
	if payload.DownloadURL == nil || strings.TrimSpace(*payload.DownloadURL) == "" {
		return "", fmt.Errorf("domain_index=%d: API returned an empty download URL", domainIndex)
	}
	downloadURL := strings.TrimSpace(*payload.DownloadURL)
	parsed, err := url.Parse(downloadURL)
	if err != nil || !parsed.IsAbs() || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return "", fmt.Errorf("domain_index=%d: API returned an invalid download URL", domainIndex)
	}
	return parsed.String(), nil
}
