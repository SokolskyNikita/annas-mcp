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
	if authors := CleanText(attr(doc.Find(fallbackCoverSelector).Eq(1), "data-content")); authors != "" {
		authorTexts = append(authorTexts, authors)
	}
	identity.Authors = splitAuthors(authorTexts)

	dois := make(map[string]struct{})
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
	return identity
}

type archiveCode struct{ key, value string }

// The codes panels are generated from the record's structured identifier
// fields. Each panel links to an exact search for its key/value pair, such as
// q="doi:10.1234%2Fexample"; similarly shaped links elsewhere on the page are
// deliberately ignored.
func archiveRecordCodes(doc *goquery.Document) []archiveCode {
	var codes []archiveCode
	doc.Find(codesSelector + " a[href^='/search']").Each(func(_ int, node *goquery.Selection) {
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

// recordTitle is the record's full title, from its fallback cover, or from
// the page's <title> when the cover has none.
func recordTitle(doc *goquery.Document) string {
	if title := CleanText(attr(doc.Find(fallbackCoverSelector).Eq(0), "data-content")); title != "" {
		return title
	}
	return titleFromArchiveDocument(doc)
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
	publisherLink := doc.Find(publisherSelector).First().Parent()
	publisher, edition := publisherAndEdition(publisherLink.Text(), publisherLink.AttrOr("href", ""))
	return RecordDetails{
		Title:     recordTitle(doc),
		Authors:   CleanAuthors(CleanText(attr(doc.Find(fallbackCoverSelector).Eq(1), "data-content"))),
		Publisher: publisher,
		Edition:   edition,
		Meta:      ParseCardMeta(ownText(doc.Find(metadataLineSelector).First())),
	}
}
