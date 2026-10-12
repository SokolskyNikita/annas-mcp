package parse

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Anna's search pages list up to 50 results per page and 100 pages.
const (
	ResultsPerPage = 50
	MaxResultPages = 100
)

// Search result cards share one layout for files and catalog records. Each
// is a child of a results list:
//
//	<div class="js-aarecord-list-outer">
//	  <div class="flex [opacity-40] …">
//	    <a href="/md5/<hash>" class="custom-a block …">cover</a>
//	    <a href="/md5/<hash>" class="js-vim-focus …">Title</a>
//	    <a href="/search?q=…"><span class="icon-[mdi--user-edit]"></span> Author</a>
//	    <a href="/search?q=<publisher>"><span class="icon-[mdi--company]"></span> Publisher, edition</a>
//	    <div>Description</div> <a class="js-aarecord-list-read-more">…</a>
//	    <div class="text-gray-800 …">English [en] · EPUB · 3.8MB · 2008 · 📘 Book (non-fiction) · 🚀/lgli/zlib<a>Save</a>…</div>
//	  </div>
//	</div>
//
// opacity-40 marks a file Anna's Archive warns might have issues. The list
// inside .js-partial-matches-show holds looser matches, which can ignore the
// page's filters.
type Card struct {
	// Prefix and ID name the record, as in /md5/<hash> or /isbndb/<isbn>.
	Prefix         string   `json:"prefix"`
	ID             string   `json:"id"`
	Href           string   `json:"href"`
	Title          string   `json:"title"`
	Authors        string   `json:"authors,omitempty"`
	Publisher      string   `json:"publisher,omitempty"`
	Edition        string   `json:"edition,omitempty"`
	Description    string   `json:"description,omitempty"`
	Meta           CardMeta `json:"meta"`
	PossibleIssues bool     `json:"possible_issues,omitempty"`
	Partial        bool     `json:"partial,omitempty"`
}

var (
	recordPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	cardLanguagePattern = regexp.MustCompile(`^(.+?)\s*\[[A-Za-z_-]+\]$`)
	cardFormatPattern   = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)
	cardSizePattern     = regexp.MustCompile(`(?i)^\d+(?:[.,]\d+)?\s*[KMGT]?B$`)
	cardYearPattern     = regexp.MustCompile(`^\d{1,4}$`)
	sourceNamePattern   = regexp.MustCompile(`^[a-z0-9_]+$`)
	resultsTotalPattern = regexp.MustCompile(`^Results ([\d,]+)\s*[-–]\s*([\d,]+) \(([\d,]+)(\+?) total\)$`)
)

// CardText is a search card's parts as the page has them, before any
// cleanup: what CardTexts extracts and ReadCard interprets.
type CardText struct {
	Href           string `json:"href"`
	Title          string `json:"title"`
	Authors        string `json:"authors,omitempty"`
	PublisherLine  string `json:"publisher_line,omitempty"`
	PublisherLink  string `json:"publisher_link,omitempty"`
	Description    string `json:"description,omitempty"`
	MetadataLine   string `json:"metadata_line,omitempty"`
	PossibleIssues bool   `json:"possible_issues,omitempty"`
	Partial        bool   `json:"partial,omitempty"`
}

// CardTexts extracts the cards of a search page, partial matches included,
// in page order. A card is a child of a results list with a title link; the
// list's scripts and ads are skipped.
func CardTexts(doc *goquery.Document) []CardText {
	if doc == nil {
		return nil
	}
	var texts []CardText
	doc.Find(resultsListSelector).Each(func(_ int, list *goquery.Selection) {
		partial := list.Closest(partialMatchesSelector).Length() > 0
		list.Children().Each(func(_ int, card *goquery.Selection) {
			link := card.Find(titleSelector).First()
			if link.Length() == 0 {
				return
			}
			publisher := card.Find(publisherSelector).First().Parent()
			texts = append(texts, CardText{
				Href:           link.AttrOr("href", ""),
				Title:          link.Text(),
				Authors:        card.Find(authorSelector).First().Parent().Text(),
				PublisherLine:  publisher.Text(),
				PublisherLink:  publisher.AttrOr("href", ""),
				Description:    card.Find(descriptionLinkSelector).First().Prev().Text(),
				MetadataLine:   ownText(card.Find(metadataLineSelector).First()),
				PossibleIssues: hasClass(card, possibleIssuesClass),
				Partial:        partial,
			})
		})
	})
	return texts
}

// ReadCard interprets a card's text. A card without a record link or a
// title is not read.
func ReadCard(text CardText) (Card, bool) {
	prefix, id, ok := RecordPath(text.Href)
	title := CleanText(text.Title)
	if !ok || title == "" {
		return Card{}, false
	}
	publisher, edition := publisherAndEdition(text.PublisherLine, text.PublisherLink)
	return Card{
		Prefix:         prefix,
		ID:             id,
		Href:           text.Href,
		Title:          title,
		Authors:        CleanAuthors(CleanText(text.Authors)),
		Publisher:      publisher,
		Edition:        edition,
		Description:    Snippet(CleanText(text.Description)),
		Meta:           ParseCardMeta(text.MetadataLine),
		PossibleIssues: text.PossibleIssues,
		Partial:        text.Partial,
	}, true
}

// SearchCards reads the result cards of a search page, partial matches
// included, in page order.
func SearchCards(doc *goquery.Document) []Card {
	var cards []Card
	for _, text := range CardTexts(doc) {
		if card, ok := ReadCard(text); ok {
			cards = append(cards, card)
		}
	}
	return cards
}

// publisherAndEdition splits the line "publisher, edition". Its link searches
// for the publisher alone, so the link's query says where the publisher ends;
// a record without a publisher links to an empty search. When the link and
// the line disagree, the whole line counts as the publisher.
func publisherAndEdition(line, link string) (publisher, edition string) {
	line = CleanText(line)
	if line == "" {
		return "", ""
	}
	query, ok := strings.CutPrefix(link, "/search?q=")
	if !ok {
		return line, ""
	}
	candidates := []string{query}
	if decoded, err := url.QueryUnescape(query); err == nil && decoded != query {
		candidates = append(candidates, decoded)
	}
	for _, candidate := range candidates {
		candidate = CleanText(candidate)
		if candidate == "" {
			return "", line
		}
		if line == candidate {
			return line, ""
		}
		if rest, ok := strings.CutPrefix(line, candidate+", "); ok {
			return candidate, rest
		}
	}
	return line, ""
}

// RecordPath splits a record link such as /md5/<hash> or /isbndb/<isbn>.
func RecordPath(href string) (prefix, id string, ok bool) {
	parsed, err := url.Parse(href)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") {
		return "", "", false
	}
	prefix, id, found := strings.Cut(strings.TrimPrefix(parsed.Path, "/"), "/")
	id = strings.TrimSuffix(id, "/")
	if !found || id == "" || !recordPrefixPattern.MatchString(prefix) {
		return "", "", false
	}
	return prefix, id, true
}

// ownText joins an element's direct text, leaving out the Save button and
// statistics scripts Anna appends to the metadata line.
func ownText(selection *goquery.Selection) string {
	var b strings.Builder
	selection.Contents().Each(func(_ int, node *goquery.Selection) {
		if goquery.NodeName(node) == "#text" {
			b.WriteString(node.Text())
		}
	})
	return b.String()
}

func hasClass(selection *goquery.Selection, class string) bool {
	classes, _ := selection.Attr("class")
	for _, name := range strings.Fields(classes) {
		if name == class {
			return true
		}
	}
	return false
}

// ResultsTotal reads Anna's count, "Results 1-50 (500+ total)". The client
// reads Anna's pages in English; see mirror.ParseBaseURL.
func ResultsTotal(doc *goquery.Document) (total int, lowerBound, found bool) {
	count := resultsCount(doc)
	return count.total, count.lowerBound, count.found
}

// resultsLine is Anna's "Results 1-50 (500+ total)": the page shows results
// from to to, of total.
type resultsLine struct {
	from, to, total   int
	lowerBound, found bool
}

func resultsCount(doc *goquery.Document) resultsLine {
	var line resultsLine
	if doc == nil {
		return line
	}
	// The count stands just before the first results list's wrapper.
	doc.Find(resultsListSelector).First().Parent().PrevAll().EachWithBreak(func(_ int, node *goquery.Selection) bool {
		match := resultsTotalPattern.FindStringSubmatch(strings.TrimSpace(node.Text()))
		if match == nil {
			return true
		}
		number := func(text string) (int, error) { return strconv.Atoi(strings.ReplaceAll(text, ",", "")) }
		from, err1 := number(match[1])
		to, err2 := number(match[2])
		total, err3 := number(match[3])
		if err1 != nil || err2 != nil || err3 != nil {
			return true
		}
		line = resultsLine{from: from, to: to, total: total, lowerBound: match[4] == "+", found: true}
		return false
	})
	return line
}

// ShowsResults reports whether a page with no parsed cards still shows
// results, by its count or by links to files. If so, Anna changed its markup,
// and returning no results would read as the work being absent.
func ShowsResults(doc *goquery.Document, pageNumber int) bool {
	if doc == nil {
		return false
	}
	if total, _, found := ResultsTotal(doc); found && total > (max(pageNumber, 1)-1)*ResultsPerPage {
		return true
	}
	return doc.Find("a[href^='/md5/']").Length() > 0
}

// FacetValues lists the values Anna offers for a filter on this page.
func FacetValues(doc *goquery.Document, name string) []string {
	var values []string
	seen := map[string]bool{}
	doc.Find("input[name='" + name + "']").Each(func(_ int, input *goquery.Selection) {
		value, _ := input.Attr("value")
		if value == "" || strings.HasPrefix(value, "anti__") || seen[value] {
			return
		}
		seen[value] = true
		values = append(values, value)
	})
	return values
}
