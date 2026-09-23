package anna

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	DefaultSearchTimeout   = 60 * time.Second
	DefaultDownloadTimeout = 30 * time.Minute
	BrowserUserAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	authorSelector      = "a[href^='/search'] span.icon-\\[mdi--user-edit\\]"
	publisherSelector   = "a[href^='/search'] span.icon-\\[mdi--company\\]"
	doiPattern          = regexp.MustCompile(`(?i)10\.\d{4,9}/[-._;()/:A-Za-z0-9]+`)
	validDOIPattern     = regexp.MustCompile(`(?i)^10\.\d{4,9}/\S+$`)
	descriptionSelector = "div.text-gray-600"
)

// ResolveSearch applies legacy content values and the default for a search.
// book_any is not a current Anna filter and makes later pages empty, so it is dropped.
// journal is the old article default and maps to the journals index.
func ResolveSearch(options SearchOptions, defaultContent string) SearchOptions {
	options.Content = strings.ToLower(strings.TrimSpace(options.Content))
	options.Index = strings.ToLower(strings.TrimSpace(options.Index))
	options.Language = strings.ToLower(strings.TrimSpace(options.Language))
	if options.Page < 1 {
		options.Page = 1
	}
	if options.Content == "journal" {
		options.Content = ""
		if options.Index == "" {
			options.Index = "journals"
		}
	}
	if options.Content == "book_any" {
		options.Content = ""
	}
	if options.Content == "" && options.Index == "" {
		switch defaultContent {
		case "journal", "journals":
			options.Index = "journals"
		case "", "book_any":
		default:
			options.Content = defaultContent
		}
	}
	return options
}

func parseBooks(doc *goquery.Document, pageURL string) []*Book {
	if doc == nil {
		return nil
	}
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
		description := snippet(info.Find(descriptionSelector).First().Text())
		books = append(books, &Book{
			Language:    language,
			Format:      format,
			Size:        size,
			Title:       title,
			Publisher:   strings.TrimSpace(info.Find(publisherSelector).Parent().Text()),
			Authors:     strings.TrimSpace(info.Find(authorSelector).Parent().Text()),
			Description: description,
			DOI:         firstDOI(title + " " + description),
			URL:         absoluteURL(pageURL, link),
			Hash:        hash,
		})
	})
	return books
}

func paperFromBook(book *Book, doi string) *Paper {
	if book == nil {
		return nil
	}
	if doi == "" {
		doi = book.DOI
	}
	paper := &Paper{
		DOI:         doi,
		Title:       book.Title,
		Authors:     book.Authors,
		Journal:     book.Publisher,
		Format:      book.Format,
		Size:        book.Size,
		Hash:        book.Hash,
		Description: book.Description,
		PageURL:     book.URL,
	}
	if doi != "" {
		paper.DownloadURL = fmt.Sprintf("/scidb?doi=%s", url.QueryEscape(doi))
	}
	return paper
}

func selectTitleHit(books []*Book, title string) *Book {
	want := normalizeTitle(title)
	if want == "" {
		return nil
	}
	for _, book := range books {
		if book == nil || book.Hash == "" || looksLikeFilename(book.Title) {
			continue
		}
		if normalizeTitle(book.Title) == want {
			return book
		}
	}
	return nil
}

func normalizeTitle(title string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

func looksLikeFilename(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || strings.Contains(title, " ") {
		return false
	}
	switch strings.ToLower(filepath.Ext(title)) {
	case ".pdf", ".epub", ".djvu", ".mobi", ".azw3", ".azw", ".fb2", ".cbz", ".cbr":
		return true
	default:
		return false
	}
}

func selectDOIHit(books []*Book, doi string) *Book {
	needle := NormalizeDOI(doi)
	if needle == "" {
		return nil
	}
	arxiv := arxivID(needle)
	// A DOI field on a result is the archive's authoritative identifier. Give
	// it precedence over mentions in titles/descriptions, which may be
	// citations to a different work.
	for _, book := range books {
		if book == nil || book.Hash == "" {
			continue
		}
		if strings.EqualFold(NormalizeDOI(book.DOI), needle) {
			return book
		}
	}
	// Match complete DOI tokens extracted from the result. Do not use a
	// substring fallback: periods and other DOI punctuation are valid token
	// characters, so a token such as 10.1000/abc must not match
	// 10.1000/abc.123.
	for _, book := range books {
		if book == nil || book.Hash == "" || strings.TrimSpace(book.DOI) != "" {
			continue
		}
		for _, field := range []string{book.Title, book.Description, book.Publisher} {
			for _, candidate := range doiPattern.FindAllString(field, -1) {
				if strings.EqualFold(NormalizeDOI(candidate), needle) {
					return book
				}
			}
		}
	}
	// arXiv identifiers are often shown without a DOI in search cards. Only
	// use that fallback when the card contains no conflicting DOI at all.
	if arxiv != "" {
		for _, book := range books {
			if book == nil || book.Hash == "" {
				continue
			}
			if len(bookDOICandidates(book)) != 0 {
				continue
			}
			hay := strings.ToLower(strings.Join([]string{book.Title, book.Description, book.Publisher}, " "))
			if containsArxivToken(hay, arxiv) {
				return book
			}
		}
	}
	return nil
}

func bookDOICandidates(book *Book) []string {
	if book == nil {
		return nil
	}
	fields := []string{book.DOI, book.Title, book.Description, book.Publisher}
	seen := make(map[string]struct{})
	var candidates []string
	for _, field := range fields {
		for _, candidate := range doiPattern.FindAllString(field, -1) {
			candidate = NormalizeDOI(candidate)
			if candidate == "" {
				continue
			}
			if _, ok := seen[strings.ToLower(candidate)]; ok {
				continue
			}
			seen[strings.ToLower(candidate)] = struct{}{}
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func containsToken(value, token string) bool {
	if token == "" {
		return false
	}
	for start := 0; start < len(value); {
		i := strings.Index(value[start:], token)
		if i < 0 {
			return false
		}
		i += start
		beforeOK := i == 0 || !isTokenByte(value[i-1])
		end := i + len(token)
		afterOK := end == len(value) || !isTokenByte(value[end])
		if beforeOK && afterOK {
			return true
		}
		start = i + 1
	}
	return false
}

func isTokenByte(value byte) bool {
	return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9')
}

func containsArxivToken(value, token string) bool {
	if containsToken(value, token) {
		return true
	}
	for start := 0; start < len(value); {
		i := strings.Index(value[start:], token)
		if i < 0 {
			return false
		}
		i += start
		if i > 0 && isTokenByte(value[i-1]) {
			start = i + 1
			continue
		}
		end := i + len(token)
		if end+1 < len(value) && value[end] == 'v' && value[end+1] >= '0' && value[end+1] <= '9' {
			end++
			for end < len(value) && value[end] >= '0' && value[end] <= '9' {
				end++
			}
			if end == len(value) || !isTokenByte(value[end]) {
				return true
			}
		}
		start = i + 1
	}
	return false
}

// NormalizeDOI removes the common DOI URL and label prefixes and trims
// punctuation commonly attached when a DOI is copied from prose.
func NormalizeDOI(doi string) string {
	doi = strings.TrimSpace(doi)
	lower := strings.ToLower(doi)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(lower, prefix) {
			if parsed, err := url.Parse(doi); err == nil && parsed.Host != "" {
				// URL.Path is decoded by net/url, so an escaped slash in a
				// DOI is treated as the DOI's normal slash separator. Query
				// parameters and fragments are tracking metadata, not DOI text.
				doi = strings.TrimPrefix(parsed.Path, "/")
			} else {
				doi = doi[len(prefix):]
			}
			break
		}
	}
	if strings.HasPrefix(lower, "doi:") {
		doi = doi[len("doi:"):]
	}
	doi = strings.TrimSpace(strings.Trim(doi, "<>\"'"))
	return trimDOIPunctuation(doi)
}

func trimDOIPunctuation(doi string) string {
	for doi != "" {
		last, size := utf8.DecodeLastRuneInString(doi)
		if last == utf8.RuneError && size == 0 {
			return doi
		}
		trim := false
		switch last {
		case '.', ',', ';', ':', ']', '}':
			trim = true
		case ')':
			// A closing parenthesis may be part of the DOI (for example
			// SICI suffixes). Remove it only when it is unmatched.
			trim = strings.Count(doi, ")") > strings.Count(doi, "(")
		}
		if !trim {
			break
		}
		doi = doi[:len(doi)-size]
	}
	return doi
}

// IsDOIQuery reports whether a query is explicitly trying to address a DOI.
// It intentionally returns true for malformed DOI-shaped input so callers can
// report an invalid DOI instead of silently treating it as keyword search.
func IsDOIQuery(query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	return strings.HasPrefix(query, "10.") ||
		strings.HasPrefix(query, "doi:") ||
		strings.HasPrefix(query, "https://doi.org/") ||
		strings.HasPrefix(query, "http://doi.org/") ||
		strings.HasPrefix(query, "https://dx.doi.org/") ||
		strings.HasPrefix(query, "http://dx.doi.org/")
}

// ParseDOI normalizes and validates a DOI query.
func ParseDOI(query string) (string, bool) {
	doi := NormalizeDOI(query)
	return doi, validDOIPattern.MatchString(doi)
}

func arxivID(doi string) string {
	_, rest, ok := strings.Cut(strings.ToLower(doi), "arxiv.")
	if !ok {
		return ""
	}
	rest = strings.Trim(rest, " ./")
	if i := strings.LastIndex(rest, "v"); i > 0 && rest[i+1:] != "" && strings.Trim(rest[i+1:], "0123456789") == "" {
		rest = rest[:i]
	}
	return rest
}

func firstDOI(text string) string {
	return strings.TrimRight(doiPattern.FindString(text), ".,;:)")
}

func snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	const limit = 480
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndex(cut, " "); i > 320 {
		cut = cut[:i]
	}
	return cut
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
