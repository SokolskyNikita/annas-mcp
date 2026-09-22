package anna

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	AnnasSciDBEndpointFormat = "https://%s/scidb/%s"
	DefaultSearchTimeout     = 60 * time.Second
	DefaultDownloadTimeout   = 30 * time.Minute
	BrowserUserAgent         = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	maxDownloadBytes         = 8 << 30
)

var (
	unsafeFilenameChars   = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)
	accountCookieWarnOnce sync.Once
	domainIndexes         = []int{0, 1, 2}
	authorSelector        = "a[href^='/search'] span.icon-\\[mdi--user-edit\\]"
	publisherSelector     = "a[href^='/search'] span.icon-\\[mdi--company\\]"
)

func FindBook(ctx context.Context, query string, options SearchOptions, timeout time.Duration) ([]*Book, error) {
	books, _, err := search(ctx, query, options, "book_any", timeout)
	return books, err
}

func FindArticle(ctx context.Context, query string, options SearchOptions, timeout time.Duration) ([]*Paper, error) {
	books, _, err := search(ctx, query, options, "journal", timeout)
	if err != nil {
		return nil, err
	}

	papers := make([]*Paper, 0, len(books))
	for _, book := range books {
		papers = append(papers, &Paper{
			Title:   book.Title,
			Authors: book.Authors,
			Journal: book.Publisher,
			Size:    book.Size,
			Hash:    book.Hash,
			PageURL: book.URL,
		})
	}
	return papers, nil
}

func search(ctx context.Context, query string, options SearchOptions, defaultContent string, timeout time.Duration) ([]*Book, string, error) {
	l := logger.GetLogger()
	if strings.TrimSpace(query) == "" {
		return nil, "", errors.New("search query is empty")
	}

	content := options.Content
	if content == "" {
		content = defaultContent
	}
	pageURL := buildSearchURL(env.GetAnnasBaseURL(), query, content, options.Language, options.Page)
	l.Info("Searching", zap.String("url", pageURL))

	doc, _, err := fetchDocument(ctx, timeout, pageURL)
	if err != nil {
		return nil, pageURL, err
	}

	books := parseBooks(doc, pageURL)
	l.Info("Search completed", zap.Int("results", len(books)))
	return books, pageURL, nil
}

func buildSearchURL(base, query, content, language string, page int) string {
	if page < 1 {
		page = 1
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("content", content)
	if language != "" {
		values.Set("lang", language)
	}
	if page > 1 {
		values.Set("page", strconv.Itoa(page))
	}
	return "https://" + base + "/search?" + values.Encode()
}

func parseBooks(doc *goquery.Document, pageURL string) []*Book {
	l := logger.GetLogger()
	books := make([]*Book, 0)
	doc.Find("a.custom-a.block[href^='/md5/']").Each(func(_ int, element *goquery.Selection) {
		parent := element.Parent()
		info := parent.Find("div.max-w-full")
		if info.Length() == 0 {
			l.Warn("Skipping book: book info container not found")
			return
		}

		title := strings.TrimSpace(info.Find("a[href^='/md5/']").First().Text())
		if title == "" {
			l.Warn("Skipping book: title is empty")
			return
		}

		link, _ := element.Attr("href")
		hash := strings.TrimPrefix(link, "/md5/")
		if hash == "" {
			l.Warn("Skipping book: no hash found", zap.String("title", title))
			return
		}

		language, format, size := extractMetaInformation(info.Find("div.text-gray-800").Text())
		books = append(books, &Book{
			Language:  language,
			Format:    format,
			Size:      size,
			Title:     title,
			Publisher: strings.TrimSpace(info.Find(publisherSelector).Parent().Text()),
			Authors:   strings.TrimSpace(info.Find(authorSelector).Parent().Text()),
			URL:       absoluteURL(pageURL, link),
			Hash:      hash,
		})
	})
	return books
}

func LookupDOI(ctx context.Context, doi string, timeout time.Duration) (*Paper, error) {
	l := logger.GetLogger()
	doi = strings.TrimSpace(doi)
	if doi == "" {
		return nil, errors.New("doi is empty")
	}

	annasBaseURL := env.GetAnnasBaseURL()
	scidbURL := fmt.Sprintf(AnnasSciDBEndpointFormat, annasBaseURL, doi)
	l.Info("Looking up DOI", zap.String("url", scidbURL))

	doc, finalURL, err := fetchDocument(ctx, timeout, scidbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup DOI: %w", err)
	}
	if finalURL == nil || !strings.Contains(finalURL.Path, "/scidb/") {
		return nil, fmt.Errorf("no paper found for DOI: %s", doi)
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
		return nil, fmt.Errorf("no paper found for DOI: %s", doi)
	}

	detailURL := fmt.Sprintf("https://%s/md5/%s", annasBaseURL, paper.Hash)
	detail, _, err := fetchDocument(ctx, timeout, detailURL)
	if err != nil {
		l.Warn("Failed to fetch paper details", zap.String("hash", paper.Hash), zap.Error(err))
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

func (b *Book) Download(ctx context.Context, secretKey, folderPath string, timeout time.Duration, progress ProgressFunc) (DownloadResult, error) {
	l := logger.GetLogger()
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(b.Hash) == "" {
		return DownloadResult{}, errors.New("book hash is empty")
	}
	if secretKey == "" {
		return DownloadResult{}, errors.New("ANNAS_SECRET_KEY is empty")
	}

	client := newHTTPClient(timeout)
	var lastErr error
	for _, domainIndex := range domainIndexes {
		downloadURL, err := resolveDownloadURL(ctx, client, env.GetAnnasBaseURL(), b.Hash, secretKey, domainIndex)
		if err != nil {
			lastErr = err
			l.Warn("Fast download URL failed",
				zap.String("hash", b.Hash),
				zap.Int("domainIndex", domainIndex),
				zap.Error(err),
			)
			continue
		}

		result, err := downloadFile(ctx, client, downloadURL, folderPath, b.Title, b.Format, b.Hash, progress)
		if err != nil {
			lastErr = redactErr(err, secretKey)
			l.Warn("Fast download file failed",
				zap.String("hash", b.Hash),
				zap.Int("domainIndex", domainIndex),
				zap.Error(lastErr),
			)
			continue
		}
		l.Info("Download completed successfully",
			zap.String("path", result.Path),
			zap.Int64("bytes", result.Bytes),
		)
		return result, nil
	}

	if lastErr == nil {
		lastErr = errors.New("no download server succeeded")
	}
	return DownloadResult{}, fmt.Errorf("download failed: %w", redactErr(lastErr, secretKey))
}

func (p *Paper) Download(ctx context.Context, folderPath string, timeout time.Duration, progress ProgressFunc) (DownloadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if p.DownloadURL == "" {
		return DownloadResult{}, errors.New("no download URL available for this paper")
	}

	downloadURL := p.DownloadURL
	if !strings.HasPrefix(downloadURL, "http") {
		downloadURL = fmt.Sprintf("https://%s%s", env.GetAnnasBaseURL(), downloadURL)
	}

	name := p.Title
	if name == "" {
		name = p.DOI
	}
	return downloadFile(ctx, newHTTPClient(timeout), downloadURL, folderPath, name, "", "", progress)
}

func resolveDownloadURL(ctx context.Context, client *http.Client, base, hash, secretKey string, domainIndex int) (string, error) {
	values := url.Values{}
	values.Set("md5", hash)
	values.Set("key", secretKey)
	values.Set("path_index", "0")
	values.Set("domain_index", strconv.Itoa(domainIndex))
	apiURL := "https://" + base + "/dyn/api/fast_download.json?" + values.Encode()

	resp, err := doGet(ctx, client, apiURL)
	if err != nil {
		return "", redactErr(err, secretKey)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		return "", fmt.Errorf("fast download API failed with status %d for domain_index=%d", resp.StatusCode, domainIndex)
	}

	var apiResp fastDownloadResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return "", fmt.Errorf("domain_index=%d: failed to decode API response: %w", domainIndex, err)
	}
	if apiResp.Error != "" {
		return "", fmt.Errorf("domain_index=%d: %s", domainIndex, apiResp.Error)
	}
	if apiResp.DownloadURL == nil || *apiResp.DownloadURL == "" {
		return "", fmt.Errorf("domain_index=%d: API returned an empty download URL", domainIndex)
	}
	return *apiResp.DownloadURL, nil
}

func downloadFile(ctx context.Context, client *http.Client, rawURL, folderPath, title, format, expectedMD5 string, progress ProgressFunc) (DownloadResult, error) {
	resp, err := doGet(ctx, client, rawURL)
	if err != nil {
		return DownloadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return DownloadResult{}, fmt.Errorf("download failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.ContentLength > maxDownloadBytes {
		return DownloadResult{}, fmt.Errorf("download is larger than %d bytes", maxDownloadBytes)
	}

	if format == "" {
		format = formatFromResponse(resp)
	}
	path, err := reserveDownloadPath(folderPath, title, format, expectedMD5)
	if err != nil {
		return DownloadResult{}, err
	}

	file, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		return DownloadResult{}, err
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(path)
		}
	}()

	written, err := copyWithHash(ctx, file, resp.Body, resp.ContentLength, expectedMD5, progress)
	if err != nil {
		return DownloadResult{}, err
	}
	if err := file.Sync(); err != nil {
		return DownloadResult{}, fmt.Errorf("failed to sync file to disk: %w", err)
	}
	success = true
	return DownloadResult{Path: path, Bytes: written}, nil
}

func copyWithHash(ctx context.Context, destination io.Writer, source io.Reader, contentLength int64, expectedMD5 string, progress ProgressFunc) (int64, error) {
	hasher := md5.New()
	buffer := make([]byte, 32*1024)
	var written int64
	var reported int64
	total := contentLength
	if total < 0 {
		total = 0
	}

	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			if written+int64(read) > maxDownloadBytes {
				return written, fmt.Errorf("download exceeded %d bytes", maxDownloadBytes)
			}
			if _, err := hasher.Write(buffer[:read]); err != nil {
				return written, err
			}
			if _, err := destination.Write(buffer[:read]); err != nil {
				return written, fmt.Errorf("failed to write file (wrote %d bytes): %w", written, err)
			}
			written += int64(read)
			if progress != nil && (written-reported >= 512*1024 || readErr == io.EOF) {
				progress(written, total)
				reported = written
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return written, fmt.Errorf("failed to write file (wrote %d bytes): %w", written, readErr)
		}
	}

	if progress != nil && reported != written {
		progress(written, total)
	}
	if expectedMD5 != "" && !strings.EqualFold(hex.EncodeToString(hasher.Sum(nil)), expectedMD5) {
		return written, fmt.Errorf("downloaded file checksum does not match %s", expectedMD5)
	}
	return written, nil
}

func reserveDownloadPath(folder, title, format, hash string) (string, error) {
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", fmt.Errorf("failed to create download directory: %w", err)
	}
	safeTitle := sanitizeFilename(title)
	if safeTitle == "" {
		safeTitle = "untitled"
	}
	format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), "."))
	if format == "" {
		format = "bin"
	}

	suffix := hash
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	candidates := []string{safeTitle + "." + format}
	if suffix != "" {
		candidates = append(candidates, safeTitle+"-"+suffix+"."+format)
	}
	for i := 2; i <= 20; i++ {
		candidates = append(candidates, fmt.Sprintf("%s-%s-%d.%s", safeTitle, suffix, i, format))
	}

	for _, name := range candidates {
		path := filepath.Join(folder, name)
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("failed to create file: %w", err)
		}
		file.Close()
		return path, nil
	}
	return "", errors.New("could not find a free filename")
}

func formatFromResponse(resp *http.Response) string {
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename"]; ok {
				if ext := strings.TrimPrefix(filepath.Ext(filename), "."); ext != "" {
					return ext
				}
			}
		}
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		return "bin"
	}
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) == 0 {
		return "bin"
	}
	return strings.TrimPrefix(extensions[0], ".")
}

func fetchDocument(ctx context.Context, timeout time.Duration, rawURL string) (*goquery.Document, *url.URL, error) {
	resp, err := doGet(ctx, newHTTPClient(timeout), rawURL)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	finalURL := resp.Request.URL
	if resp.StatusCode == http.StatusForbidden {
		return nil, finalURL, errors.New("Anna's Archive returned 403. Set ANNAS_ACCOUNT_COOKIE to a fresh aa_account_id2 value")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, finalURL, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	doc, err := goquery.NewDocumentFromReader(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, finalURL, fmt.Errorf("failed to parse HTML: %w", err)
	}
	return doc, finalURL, nil
}

func newHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if len(via) > 0 && !sameHost(req.URL, via[0].URL) {
				req.Header.Del("Cookie")
			}
			return nil
		},
	}
}

func doGet(ctx context.Context, client *http.Client, rawURL string) (*http.Response, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", BrowserUserAgent)
	if shouldSendAccountCookie(req.URL) {
		if cookie := env.AccountCookieHeader(); cookie != "" {
			req.Header.Set("Cookie", cookie)
		} else {
			accountCookieWarnOnce.Do(func() {
				logger.GetLogger().Warn("ANNAS_ACCOUNT_COOKIE is unset; Anna's Archive may block search")
			})
		}
	}
	return client.Do(req)
}

func shouldSendAccountCookie(target *url.URL) bool {
	if target == nil {
		return false
	}
	host := target.Hostname()
	base := env.GetAnnasBaseURL()
	return host == base || strings.HasSuffix(host, "."+base)
}

func sameHost(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Hostname(), right.Hostname())
}

func absoluteURL(pageURL, href string) string {
	page, err := url.Parse(pageURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return page.ResolveReference(ref).String()
}

func redactErr(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "REDACTED"))
}

func extractMetaInformation(meta string) (language, format, size string) {
	parts := strings.Split(meta, " · ")
	if len(parts) < 3 {
		return "", "", ""
	}

	languagePart := strings.TrimSpace(parts[0])
	if idx := strings.Index(languagePart, "["); idx > 0 {
		language = strings.TrimSpace(languagePart[:idx])
		language = strings.TrimPrefix(language, "✅")
		language = strings.TrimSpace(language)
	}

	formatRegex := regexp.MustCompile(`(?i)\b(EPUB|PDF|MOBI|AZW3|AZW|DJVU|CBZ|CBR|FB2|DOCX?|TXT)\b`)
	sizeRegex := regexp.MustCompile(`\d+\.?\d*\s*(MB|KB|GB|TB)`)

	for i := 1; i < len(parts); i++ {
		part := strings.TrimSpace(parts[i])
		if size == "" && sizeRegex.MatchString(part) {
			size = part
		}
		if format == "" && formatRegex.MatchString(part) {
			matches := formatRegex.FindStringSubmatch(part)
			if len(matches) > 0 {
				format = strings.ToUpper(matches[1])
			}
		}
		if format != "" && size != "" {
			break
		}
	}

	return language, format, size
}

func sanitizeFilename(filename string) string {
	safe := unsafeFilenameChars.ReplaceAllString(filename, "_")
	safe = strings.ReplaceAll(safe, "..", "_")
	safe = filepath.Base(safe)

	runes := []rune(safe)
	if len(runes) > 100 {
		safe = string(runes[:100])
	}
	return safe
}

func (b *Book) String() string {
	return fmt.Sprintf("Title: %s\nAuthors: %s\nPublisher: %s\nLanguage: %s\nFormat: %s\nSize: %s\nURL: %s\nHash: %s",
		b.Title, b.Authors, b.Publisher, b.Language, b.Format, b.Size, b.URL, b.Hash)
}

func (b *Book) ToJSON() (string, error) {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
