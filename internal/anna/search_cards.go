package anna

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// Anna's search pages list up to 50 results per page and 100 pages.
const (
	resultsPerPage = 50
	maxResultPages = 100
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
type searchCard struct {
	prefix, id                         string
	href                               string
	title, authors, publisher, edition string
	description                        string
	meta                               cardMeta
	possibleIssues                     bool
	partial                            bool
}

type cardMeta struct {
	language, format, size, year, content string
	sources                               []string
	fastDownload                          bool
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

// The content label is translated, but its emoji is not.
var contentEmoji = map[rune]string{
	'📗': "book_unknown", '📘': "book_nonfiction", '📕': "book_fiction",
	'📄': "journal_article", '📝': "standards_document", '📰': "magazine",
	'💬': "book_comic", '🎶': "musical_score", '🎧': "audiobook", '🤨': "other",
}

func parseSearchCards(doc *goquery.Document) []searchCard {
	if doc == nil {
		return nil
	}
	var cards []searchCard
	doc.Find("a.custom-a.block[href]").Each(func(_ int, link *goquery.Selection) {
		href, _ := link.Attr("href")
		prefix, id, ok := recordPath(href)
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
				title = strings.TrimSpace(anchor.Text())
				return false
			}
			return true
		})
		if title == "" {
			return
		}
		publisher, edition := publisherAndEdition(info.Find(publisherSelector).Parent())
		cards = append(cards, searchCard{
			prefix:         prefix,
			id:             id,
			href:           href,
			title:          title,
			authors:        cleanAuthors(info.Find(authorSelector).Parent().Text()),
			publisher:      publisher,
			edition:        edition,
			description:    snippet(info.Find(descriptionSelector).First().Text()),
			meta:           parseCardMeta(ownText(info.Find("div.text-gray-800").First())),
			possibleIssues: hasClass(container, "opacity-40"),
			partial:        link.Closest(".js-partial-matches-show").Length() > 0,
		})
	})
	return cards
}

// publisherAndEdition splits the line "publisher, edition". Its link searches
// for the publisher alone, so the link's query says where the publisher ends;
// a record without a publisher links to an empty search. When the link and
// the line disagree, the whole line counts as the publisher.
func publisherAndEdition(link *goquery.Selection) (publisher, edition string) {
	line := strings.Join(strings.Fields(link.Text()), " ")
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
		candidate = strings.Join(strings.Fields(candidate), " ")
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

// recordPath splits a record link such as /md5/<hash> or /isbndb/<isbn>.
func recordPath(href string) (prefix, id string, ok bool) {
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

// parseCardMeta reads the metadata line. Its parts are optional but always
// in this order: languages, extension, size, year, content type, sources
// (prefixed by 🧬 for SciDB and 🚀 for fast downloads) and a record id.
func parseCardMeta(line string) cardMeta {
	const (
		stageLanguage = iota
		stageFormat
		stageSize
		stageYear
		stageContent
		stageSources
		stageDone
	)
	var meta cardMeta
	stage := stageLanguage
	for _, part := range strings.Split(line, "·") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "✅"))
		if part == "" {
			continue
		}
		if stage <= stageLanguage {
			// A record can list up to three languages, most likely first.
			if match := cardLanguagePattern.FindStringSubmatch(part); match != nil {
				if meta.language != "" {
					meta.language += ", "
				}
				meta.language += match[1]
				continue
			}
		}
		if stage <= stageFormat && cardFormatPattern.MatchString(part) && strings.IndexFunc(part, unicode.IsLetter) >= 0 {
			meta.format, stage = part, stageSize
			continue
		}
		if stage <= stageSize && cardSizePattern.MatchString(part) {
			meta.size, stage = part, stageYear
			continue
		}
		if stage <= stageYear && cardYearPattern.MatchString(part) {
			meta.year, stage = part, stageContent
			continue
		}
		if stage <= stageContent {
			first, _ := utf8.DecodeRuneInString(part)
			if content, ok := contentEmoji[first]; ok {
				meta.content, stage = content, stageSources
				continue
			}
		}
		// Sources follow the content type, which every record has; a lone
		// lowercase word earlier on, such as a translated label, is not one.
		if stage == stageSources || (stage < stageSources && strings.Contains(part, "/")) {
			if sources, fast, ok := parseSources(part); ok {
				meta.sources, meta.fastDownload, stage = sources, fast, stageDone
				continue
			}
		}
	}
	return meta
}

func parseSources(part string) (sources []string, fastDownload, ok bool) {
	for _, token := range strings.Split(part, "/") {
		switch token = strings.TrimSpace(token); {
		case token == "🚀":
			fastDownload = true
		case token == "🧬":
		case sourceNamePattern.MatchString(token):
			sources = append(sources, token)
		default:
			return nil, false, false
		}
	}
	return sources, fastDownload, len(sources) > 0 || fastDownload
}

// parseResultsTotal reads "Results 1-50 (500+ total)". The words are
// translated, but the numbers keep their order.
func parseResultsTotal(doc *goquery.Document) (total int, lowerBound, found bool) {
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

// showsResults reports whether a page with no parsed cards still shows
// results, by its count or by links to files. Then Anna changed its markup,
// and returning no results would read as the work being absent.
func showsResults(doc *goquery.Document, pageNumber int) bool {
	if doc == nil {
		return false
	}
	if total, _, found := parseResultsTotal(doc); found && total > (max(pageNumber, 1)-1)*resultsPerPage {
		return true
	}
	return doc.Find("a[href^='/md5/']").Length() > 0
}

// pageTotals fills in totals for a parsed page. Without Anna's count, a
// full page is taken to mean that more results may follow.
func pageTotals[T any](page *SearchPage[T], doc *goquery.Document, pageNumber int) {
	if pageNumber < 1 {
		pageNumber = 1
	}
	total, lowerBound, found := parseResultsTotal(doc)
	if !found {
		total = (pageNumber-1)*resultsPerPage + len(page.Results)
		lowerBound = len(page.Results) >= resultsPerPage
	}
	page.Total, page.TotalIsLowerBound = total, lowerBound
	page.MorePages = pageNumber < maxResultPages && (total > pageNumber*resultsPerPage || lowerBound)
}

// facetValues lists the values Anna offers for a filter on this page.
func facetValues(doc *goquery.Document, name string) []string {
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

// checkFacetFilter rejects requested values the page does not offer. Anna
// ignores an unknown value and returns no exact matches, which would read
// as the work being absent. Pages without the facet are not checked.
func checkFacetFilter(doc *goquery.Document, name, label string, requested []string) error {
	if len(requested) == 0 || doc == nil {
		return nil
	}
	offered := facetValues(doc, name)
	if len(offered) == 0 {
		return nil
	}
	known := make(map[string]bool, len(offered))
	for _, value := range offered {
		known[value] = true
	}
	for _, value := range requested {
		value = strings.TrimPrefix(value, "-")
		if !known[value] {
			sort.Strings(offered)
			return apperr.New(apperr.InvalidArgument, fmt.Sprintf("unknown %s %q for this search; use one of: %s", label, value, strings.Join(offered, ", ")))
		}
	}
	return nil
}
