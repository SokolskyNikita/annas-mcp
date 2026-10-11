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

// Search result cards share one layout for files and catalog records:
//
//	<div class="flex [opacity-40] …">
//	  <a href="/md5/<hash>" class="custom-a block …">cover</a>
//	  <div class="max-w-full …">
//	    <a href="/md5/<hash>">Title</a> <a href="/search?q=…"><span class="icon-[mdi--user-edit]"></span> Author</a>
//	    <a href="/search?q=<publisher>"><span class="icon-[mdi--company]"></span> Publisher, edition</a>
//	    <div class="… text-gray-600 …">Description</div>
//	    <div class="text-gray-800 …">English [en] · EPUB · 3.8MB · 2008 · 📘 Book (non-fiction) · 🚀/lgli/zlib<a>Save</a>…</div>
//	  </div>
//	</div>
//
// opacity-40 marks a file Anna's Archive warns might have issues. Cards
// inside .js-partial-matches-show are looser matches, which can ignore the
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
	resultsTotalPattern = regexp.MustCompile(`^\D*\d+\s*[-–]\s*\d+\D*?(\d+)(\+?)`)
)

// SearchCards reads the result cards of a search page, partial matches
// included, in page order. Cards without a record link or title are skipped.
func SearchCards(doc *goquery.Document) []Card {
	if doc == nil {
		return nil
	}
	var cards []Card
	doc.Find("a.custom-a.block[href]").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		prefix, id, ok := RecordPath(href)
		if !ok {
			return
		}
		container := link.Parent()
		info := container.Find("div.max-w-full").First()
		if info.Length() == 0 {
			return
		}
		title := ""
		info.Find("a[href]").EachWithBreak(func(_ int, anchor *goquery.Selection) bool {
			if target, _ := anchor.Attr("href"); target == href {
				title = CleanText(anchor.Text())
				return false
			}
			return true
		})
		if title == "" {
			return
		}
		publisher, edition := publisherAndEdition(info.Find(publisherSelector).Parent())
		cards = append(cards, Card{
			Prefix:         prefix,
			ID:             id,
			Href:           href,
			Title:          title,
			Authors:        CleanAuthors(CleanText(info.Find(authorSelector).Parent().Text())),
			Publisher:      publisher,
			Edition:        edition,
			Description:    Snippet(CleanText(info.Find(descriptionSelector).First().Text())),
			Meta:           ParseCardMeta(ownText(info.Find("div.text-gray-800").First())),
			PossibleIssues: hasClass(container, "opacity-40"),
			Partial:        link.Closest(".js-partial-matches-show").Length() > 0,
		})
	})
	return cards
}

// publisherAndEdition splits the line "publisher, edition". Its link searches
// for the publisher alone, so the link's query says where the publisher ends;
// a record without a publisher links to an empty search. When the link and
// the line disagree, the whole line counts as the publisher.
func publisherAndEdition(link *goquery.Selection) (publisher, edition string) {
	line := CleanText(link.Text())
	if line == "" {
		return "", ""
	}
	query, ok := strings.CutPrefix(link.AttrOr("href", ""), "/search?q=")
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

// ResultsTotal reads "Results 1-50 (500+ total)". The words are
// translated, but the numbers keep their order.
func ResultsTotal(doc *goquery.Document) (total int, lowerBound, found bool) {
	if doc == nil {
		return 0, false, false
	}
	doc.Find("div.uppercase").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		match := resultsTotalPattern.FindStringSubmatch(strings.TrimSpace(node.Text()))
		if match == nil {
			return true
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			return true
		}
		total, lowerBound, found = value, match[2] == "+", true
		return false
	})
	return total, lowerBound, found
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
