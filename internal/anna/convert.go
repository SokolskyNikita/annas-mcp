package anna

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

// Conversions from what package parse reads to the results the tools return.

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
		Stats:          book.Stats,
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

// addRecordDetails fills in a SciDB paper's details from its record page,
// read like a search card: the journal is the "publisher, edition" line.
func addRecordDetails(paper *Paper, details parse.RecordDetails) {
	paper.Title, paper.Authors = details.Title, details.Authors
	paper.Journal = joinNonEmpty(", ", details.Publisher, details.Edition)
	paper.Year, paper.Size = details.Meta.Year, details.Meta.Size
	paper.Sources, paper.FastDownload = details.Meta.Sources, details.Meta.FastDownload
}
