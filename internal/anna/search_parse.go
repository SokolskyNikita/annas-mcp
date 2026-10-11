package anna

import (
	"net/url"
	"strings"
	"time"
	"unicode"

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
