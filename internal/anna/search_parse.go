package anna

import (
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

const (
	DefaultSearchTimeout   = 60 * time.Second
	DefaultDownloadTimeout = 30 * time.Minute
	BrowserUserAgent       = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
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
	return searchResults{cards: parse.SearchCards(doc), pageURL: pageURL}.books()
}

func (r searchResults) books() []*Book {
	var books []*Book
	for _, card := range r.cards {
		if card.Prefix == "md5" {
			books = append(books, bookFromCard(card, r.pageURL))
		}
	}
	return books
}

func bookFromCard(card parse.Card, pageURL string) *Book {
	return &Book{
		Title:          card.Title,
		Authors:        card.Authors,
		Publisher:      card.Publisher,
		Edition:        card.Edition,
		Language:       card.Meta.Language,
		Format:         card.Meta.Format,
		Size:           card.Meta.Size,
		Year:           card.Meta.Year,
		Content:        card.Meta.Content,
		Sources:        card.Meta.Sources,
		FastDownload:   card.Meta.FastDownload,
		PossibleIssues: card.PossibleIssues,
		Description:    card.Description,
		URL:            absoluteURL(pageURL, card.Href),
		Hash:           card.ID,
	}
}

func metadataFromCard(card parse.Card, pageURL string) *MetadataRecord {
	id, err := url.PathUnescape(card.ID)
	if err != nil {
		id = card.ID
	}
	return &MetadataRecord{
		ID:          card.Prefix + ":" + id,
		Title:       card.Title,
		Authors:     card.Authors,
		Publisher:   card.Publisher,
		Edition:     card.Edition,
		Language:    card.Meta.Language,
		Year:        card.Meta.Year,
		Description: card.Description,
		URL:         absoluteURL(pageURL, card.Href),
	}
}

// splitPage sorts the chosen cards into exact and partial matches.
func splitPage[T any](cards []parse.Card, keep func(parse.Card) bool, convert func(parse.Card) T) SearchPage[T] {
	page := SearchPage[T]{Results: []T{}}
	for _, card := range cards {
		if !keep(card) {
			continue
		}
		if card.Partial {
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
