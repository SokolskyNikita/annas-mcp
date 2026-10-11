package anna

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
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
// journal is the old article default and maps to the journals index, as does
// journal_article outside the metadata index: that tab holds only articles.
func ResolveSearch(options SearchOptions, defaultContent string) SearchOptions {
	options.Index = strings.ToLower(strings.TrimSpace(options.Index))
	if options.Page < 1 {
		options.Page = 1
	}
	content := make([]string, 0, len(options.Content))
	for _, value := range options.Content {
		switch value = strings.ToLower(strings.TrimSpace(value)); value {
		case "", "book_any":
		case "journal", "journal_article":
			if options.Index == "" {
				options.Index = "journals"
			}
			if value == "journal_article" && options.Index != "journals" {
				content = append(content, value)
			}
		default:
			content = append(content, value)
		}
	}
	options.Content = content
	if len(options.Content) == 0 && options.Index == "" {
		switch defaultContent {
		case "journal", "journals":
			options.Index = "journals"
		case "", "book_any":
		default:
			options.Content = []string{defaultContent}
		}
	}
	return options
}

// parseBooks returns every file card on a search page, partial matches included.
func parseBooks(doc *goquery.Document, pageURL string) []*Book {
	return searchResults{cards: parseSearchCards(doc), pageURL: pageURL}.books()
}

func (r searchResults) books() []*Book {
	var books []*Book
	for _, card := range r.cards {
		if card.prefix == "md5" {
			books = append(books, bookFromCard(card, r.pageURL))
		}
	}
	return books
}

func bookFromCard(card searchCard, pageURL string) *Book {
	return &Book{
		Title:          card.title,
		Authors:        card.authors,
		Publisher:      card.publisher,
		Edition:        card.edition,
		Language:       card.meta.language,
		Format:         card.meta.format,
		Size:           card.meta.size,
		Year:           card.meta.year,
		Content:        card.meta.content,
		Sources:        card.meta.sources,
		FastDownload:   card.meta.fastDownload,
		PossibleIssues: card.possibleIssues,
		Description:    card.description,
		URL:            absoluteURL(pageURL, card.href),
		Hash:           card.id,
	}
}

func metadataFromCard(card searchCard, pageURL string) *MetadataRecord {
	id, err := url.PathUnescape(card.id)
	if err != nil {
		id = card.id
	}
	return &MetadataRecord{
		ID:          card.prefix + ":" + id,
		Title:       card.title,
		Authors:     card.authors,
		Publisher:   card.publisher,
		Edition:     card.edition,
		Language:    card.meta.language,
		Year:        card.meta.year,
		Description: card.description,
		URL:         absoluteURL(pageURL, card.href),
	}
}

// splitPage sorts the chosen cards into exact and partial matches.
func splitPage[T any](cards []searchCard, keep func(searchCard) bool, convert func(searchCard) T) SearchPage[T] {
	page := SearchPage[T]{Results: []T{}}
	for _, card := range cards {
		if !keep(card) {
			continue
		}
		if card.partial {
			page.Partial = append(page.Partial, convert(card))
		} else {
			page.Results = append(page.Results, convert(card))
		}
	}
	return page
}

func paperFromBook(book *Book, doi string) *Paper {
	if book == nil {
		return nil
	}
	if doi == "" {
		doi = book.DOI
	}
	paper := &Paper{
		DOI:            doi,
		Title:          book.Title,
		Authors:        book.Authors,
		Journal:        joinNonEmpty(", ", book.Publisher, book.Edition),
		Year:           book.Year,
		Format:         book.Format,
		Size:           book.Size,
		Sources:        book.Sources,
		FastDownload:   book.FastDownload,
		PossibleIssues: book.PossibleIssues,
		Hash:           book.Hash,
		Description:    book.Description,
		PageURL:        book.URL,
	}
	return paper
}

func joinNonEmpty(separator string, values ...string) string {
	var kept []string
	for _, value := range values {
		if value != "" {
			kept = append(kept, value)
		}
	}
	return strings.Join(kept, separator)
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

// Some imported records append affiliations to the author field, either as a
// numbered list ("A , B ;1 FAIR - Facebook AI Research ...", from HAL) or as
// a parenthesized block after the last author ("A;B(University of ...;...)").
var (
	numberedAffiliations = regexp.MustCompile(`\s*;\s*1\s+\S`)
	affiliationKeywords  = regexp.MustCompile(`(?i)\b(universit|institut|department|school|college|laborator|hospital|academy|research|faculty|centre|center)`)
)

func cleanAuthors(authors string) string {
	if loc := numberedAffiliations.FindStringIndex(authors); loc != nil {
		authors = authors[:loc[0]]
	}
	authors = trimAffiliationBlock(authors)
	var groups []string
	for _, group := range strings.Split(authors, ";") {
		var names []string
		for _, name := range strings.Split(group, ",") {
			if name = strings.Join(strings.Fields(name), " "); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			groups = append(groups, strings.Join(names, ", "))
		}
	}
	return strings.Join(groups, "; ")
}

// trimAffiliationBlock drops the first parenthesized block that holds
// affiliations. Short notes such as "(ed.)" or "(Translator)" are kept.
func trimAffiliationBlock(authors string) string {
	open := strings.IndexAny(authors, "(（")
	if open < 0 {
		return authors
	}
	// Affiliations can nest parentheses ("Institute (Quzhou)") and may use
	// full-width ones, so find the matching close; an unclosed block runs to
	// the end of the field.
	block := authors[open:]
	depth := 0
	for i, r := range block {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			if depth--; depth == 0 {
				block = block[:i+utf8.RuneLen(r)]
			}
		}
		if depth == 0 {
			break
		}
	}
	if len(block) < 40 || !affiliationKeywords.MatchString(block) {
		return authors
	}
	return strings.TrimSpace(authors[:open]) + " " + strings.TrimSpace(authors[open+len(block):])
}

// Some records' text was HTML-escaped before Anna escaped it for its page,
// so it reads "Smith &amp; Jones" after the page itself is decoded.
// Only complete entities are decoded: legacy forms without a semicolon,
// such as "&para", are ordinary text here.
var escapedEntity = regexp.MustCompile(`&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

func decodeEntities(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	return escapedEntity.ReplaceAllStringFunc(text, html.UnescapeString)
}

// Records imported from publishers can carry formatting markup in their
// text, such as "<i>AlphaFold</i>2" or Crossref's <jats:p>. Only known
// formatting tags are removed, so text such as "Vector<T>" is kept.
var (
	inlineMarkup = regexp.MustCompile(`(?i)</?(?:a|b|i|em|strong|sub|sup|u|span|small|sc|tt|code|(?:jats|mml):[a-z-]+)(?:\s[^<>]*)?/?>`)
	blockMarkup  = regexp.MustCompile(`(?i)</?(?:p|br|div|li|ul|ol|h[1-6])(?:\s[^<>]*)?/?>`)
)

// cleanText decodes doubly escaped entities, removes formatting markup and
// collapses whitespace.
func cleanText(text string) string {
	text = decodeEntities(text)
	if strings.Contains(text, "<") {
		text = blockMarkup.ReplaceAllString(inlineMarkup.ReplaceAllString(text, ""), " ")
	}
	return strings.Join(strings.Fields(text), " ")
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
