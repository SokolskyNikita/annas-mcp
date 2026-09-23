package anna

import (
	"net/url"
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
	return paper
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

// NormalizeDOI removes common DOI URL/label prefixes and enclosing wrappers.
// Suffix punctuation is significant and must not be guessed away as prose.
func NormalizeDOI(doi string) string {
	doi = unwrapDOI(doi)
	if strings.HasPrefix(strings.ToLower(doi), "doi:") {
		doi = unwrapDOI(doi[len("doi:"):])
	}
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
	return strings.TrimSpace(doi)
}

func unwrapDOI(doi string) string {
	doi = strings.TrimSpace(doi)
	for len(doi) >= 2 {
		first, last := doi[0], doi[len(doi)-1]
		if !(first == '<' && last == '>' || first == '(' && last == ')' ||
			first == '[' && last == ']' || first == '{' && last == '}' ||
			first == '"' && last == '"' || first == '\'' && last == '\'') {
			break
		}
		doi = strings.TrimSpace(doi[1 : len(doi)-1])
	}
	return doi
}

// IsDOIQuery reports whether a query is explicitly trying to address a DOI.
// It intentionally returns true for malformed DOI-shaped input so callers can
// report an invalid DOI instead of silently treating it as keyword search.
func IsDOIQuery(query string) bool {
	query = strings.ToLower(unwrapDOI(query))
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
	const prefix = "10.48550/arxiv."
	doi = NormalizeDOI(doi)
	if !strings.HasPrefix(strings.ToLower(doi), prefix) {
		return ""
	}
	return strings.ToLower(stripArxivVersion(doi[len(prefix):]))
}

func stripArxivVersion(identifier string) string {
	if i := strings.LastIndex(strings.ToLower(identifier), "v"); i > 0 && i+1 < len(identifier) {
		version := identifier[i+1:]
		if strings.Trim(version, "0123456789") == "" {
			return identifier[:i]
		}
	}
	return identifier
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
