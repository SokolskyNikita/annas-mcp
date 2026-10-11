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

	// The current record page provides the full title and author list in
	// data-content nodes. Keep the older title/meta/link selectors as fallbacks
	// for mirrors serving the preceding template.
	doc.Find("[data-content][class*=violet-900]").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		identity.Title = CleanText(attr(node, "data-content"))
		return identity.Title == ""
	})
	if identity.Title == "" {
		identity.Title = strings.TrimSpace(doc.Find("meta[name=citation_title], meta[property='og:title']").First().AttrOr("content", ""))
	}
	if identity.Title == "" {
		identity.Title = strings.TrimSpace(doc.Find("h1").First().Text())
	}
	if identity.Title == "" {
		identity.Title = titleFromArchiveDocument(doc)
	}

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

// RecordDetails is what DOI lookup reports of a SciDB paper, read from its
// record page.
type RecordDetails struct {
	Title   string `json:"title"`
	Authors string `json:"authors"`
	Journal string `json:"journal"`
	Size    string `json:"size"`
}

// ReadRecordDetails reads a SciDB paper's title, journal, authors and size
// from its record page.
func ReadRecordDetails(detail *goquery.Document) RecordDetails {
	var details RecordDetails
	if title := strings.TrimSpace(detail.Find("title").First().Text()); title != "" {
		if idx := strings.Index(title, " - Anna"); idx > 0 {
			details.Title = strings.TrimSpace(title[:idx])
		}
	}
	if description, ok := detail.Find("meta[name=description]").Attr("content"); ok {
		parts := strings.Split(description, "\n\n")
		switch {
		case len(parts) >= 3:
			details.Journal = strings.TrimSpace(parts[2])
		case len(parts) >= 2:
			details.Journal = strings.TrimSpace(parts[1])
		default:
			details.Journal = strings.TrimSpace(description)
		}
	}
	detail.Find(authorSelector).EachWithBreak(func(_ int, element *goquery.Selection) bool {
		details.Authors = CleanAuthors(element.Parent().Text())
		return details.Authors == ""
	})
	detail.Find("div.text-gray-500").EachWithBreak(func(_ int, element *goquery.Selection) bool {
		text := strings.TrimSpace(element.Text())
		if strings.Contains(text, "MB") || strings.Contains(text, "KB") {
			details.Size = text
			return false
		}
		return true
	})
	return details
}
