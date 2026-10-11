package parse

import (
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Identity is a record's own description of its work, read from its record
// page: the title and authors Anna shows, and the DOI, arXiv id and year in
// its identifiers panel. DOIConflict marks a record listing several DOIs.
type Identity struct {
	DOI         string   `json:"doi"`
	DOIConflict bool     `json:"doi_conflict"`
	ArxivID     string   `json:"arxiv_id"`
	Title       string   `json:"title"`
	Authors     []string `json:"authors"`
	Year        string   `json:"year"`
}

// RecordIdentity reads the selected MD5 record's own metadata. In
// particular, it never examines the search result description, where DOI
// strings commonly refer to citations rather than to the result itself.
func RecordIdentity(doc *goquery.Document) Identity {
	var identity Identity
	if doc == nil {
		return identity
	}

	identity.Title = recordTitle(doc)

	var authorTexts []string
	doc.Find("[data-content][class*=amber-900]").Each(func(_ int, node *goquery.Selection) {
		if value := CleanText(attr(node, "data-content")); value != "" {
			authorTexts = append(authorTexts, value)
		}
	})
	if len(authorTexts) == 0 {
		doc.Find("meta[name=citation_author], meta[name='DC.Creator']").Each(func(_ int, node *goquery.Selection) {
			if value := strings.TrimSpace(node.AttrOr("content", "")); value != "" {
				authorTexts = append(authorTexts, value)
			}
		})
	}
	if len(authorTexts) == 0 {
		doc.Find(authorSelector).Each(func(_ int, node *goquery.Selection) {
			if value := strings.TrimSpace(node.Parent().Text()); value != "" {
				authorTexts = append(authorTexts, value)
			}
		})
	}
	identity.Authors = splitAuthors(authorTexts)

	dois := make(map[string]struct{})
	doc.Find("meta[name=citation_doi], meta[name='DC.Identifier.DOI'], meta[property='og:doi']").Each(func(_ int, node *goquery.Selection) {
		if candidate := NormalizeDOI(node.AttrOr("content", "")); candidate != "" && !placeholderDOI(candidate) {
			dois[strings.ToLower(candidate)] = struct{}{}
			if identity.DOI == "" {
				identity.DOI = candidate
			}
		}
	})
	for _, code := range archiveRecordCodes(doc) {
		switch strings.ToLower(code.key) {
		case "doi":
			candidate := NormalizeDOI(code.value)
			if candidate != "" && !placeholderDOI(candidate) {
				dois[strings.ToLower(candidate)] = struct{}{}
				if identity.DOI == "" {
					identity.DOI = candidate
				}
			}
		case "arxiv", "arxiv_id", "arxiv-id":
			if identity.ArxivID == "" {
				identity.ArxivID = NormalizeArxivID(code.value)
			}
		case "year", "publication_year", "publication-year":
			if identity.Year == "" {
				identity.Year = yearValue(code.value)
			}
		}
	}
	identity.DOIConflict = len(dois) > 1
	if identity.DOI != "" {
		identity.DOI = NormalizeDOI(identity.DOI)
	}
	if identity.Year == "" {
		identity.Year = archiveYear(doc)
	}
	return identity
}

type archiveCode struct{ key, value string }

// The codes panels are generated from the record's structured identifier
// fields. Each panel links to an exact search for its key/value pair, such as
// q="doi:10.1234%2Fexample"; similarly shaped links elsewhere on the page are
// deliberately ignored.
func archiveRecordCodes(doc *goquery.Document) []archiveCode {
	var codes []archiveCode
	doc.Find(".js-md5-codes-container a[href^='/search']").Each(func(_ int, node *goquery.Selection) {
		href := node.AttrOr("href", "")
		parsed, err := url.Parse(href)
		if err != nil {
			return
		}
		query := strings.Trim(strings.TrimSpace(parsed.Query().Get("q")), `"`)
		key, value, ok := strings.Cut(query, ":")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return
		}
		codes = append(codes, archiveCode{key: strings.TrimSpace(key), value: strings.TrimSpace(value)})
	})
	return codes
}

// placeholderDOI recognizes the 10.0000/<md5> codes Anna gives records
// without a DOI. No DOI registrant has the code 0000.
func placeholderDOI(doi string) bool {
	return strings.HasPrefix(doi, "10.0000/")
}

func archiveYear(doc *goquery.Document) string {
	var year string
	doc.Find(".js-md5-codes-container [id^='md5-codes-panel-']").EachWithBreak(func(_ int, panel *goquery.Selection) bool {
		text := strings.ToLower(strings.Join(strings.Fields(panel.Text()), " "))
		if !strings.Contains(text, "year:") {
			return true
		}
		panel.Find("span.bg-gray-200").EachWithBreak(func(_ int, value *goquery.Selection) bool {
			year = yearValue(value.Text())
			return year == ""
		})
		return year == ""
	})
	if year != "" {
		return year
	}
	doc.Find("meta[name=citation_publication_date], meta[name=citation_date], meta[name='DC.Date']").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		year = yearValue(node.AttrOr("content", ""))
		return year == ""
	})
	if year != "" {
		return year
	}
	doc.Find("time[datetime]").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		year = yearValue(node.AttrOr("datetime", ""))
		return year == ""
	})
	return year
}

func yearValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 && value[0] >= '1' && value[0] <= '2' {
		allDigits := true
		for _, digit := range value[:4] {
			if digit < '0' || digit > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return value[:4]
		}
	}
	return ""
}

func titleFromArchiveDocument(doc *goquery.Document) string {
	title := strings.TrimSpace(doc.Find("title").First().Text())
	for _, suffix := range []string{" - Anna's Archive", " - Anna’s Archive"} {
		if index := strings.Index(title, suffix); index > 0 {
			return strings.TrimSpace(title[:index])
		}
	}
	return ""
}

func attr(node *goquery.Selection, name string) string {
	value, _ := node.Attr(name)
	return value
}

// recordTitle is the record's full title. The current record page provides
// it in a data-content node; older title, meta and heading selectors remain
// as fallbacks.
func recordTitle(doc *goquery.Document) string {
	var title string
	doc.Find("[data-content][class*=violet-900]").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		title = CleanText(attr(node, "data-content"))
		return title == ""
	})
	if title == "" {
		title = strings.TrimSpace(doc.Find("meta[name=citation_title], meta[property='og:title']").First().AttrOr("content", ""))
	}
	if title == "" {
		title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if title == "" {
		title = titleFromArchiveDocument(doc)
	}
	return title
}

// RecordDetails is how a record page describes its file. The page's header
// has the same parts as a search card: the title, the authors, the
// "publisher, edition" line and the metadata line.
type RecordDetails struct {
	Title     string   `json:"title"`
	Authors   string   `json:"authors,omitempty"`
	Publisher string   `json:"publisher,omitempty"`
	Edition   string   `json:"edition,omitempty"`
	Meta      CardMeta `json:"meta"`
}

// ReadRecordDetails reads a record page's header the way SearchCards reads a
// card.
func ReadRecordDetails(doc *goquery.Document) RecordDetails {
	if doc == nil {
		return RecordDetails{}
	}
	publisher, edition := publisherAndEdition(doc.Find(publisherSelector).First().Parent())
	return RecordDetails{
		Title:     recordTitle(doc),
		Authors:   CleanAuthors(CleanText(doc.Find(authorSelector).First().Parent().Text())),
		Publisher: publisher,
		Edition:   edition,
		Meta:      ParseCardMeta(ownText(doc.Find("div.text-gray-800").First())),
	}
}
