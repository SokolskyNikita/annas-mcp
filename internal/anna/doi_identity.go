package anna

import (
	"net/url"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

type doiMetadata struct {
	DOI     string
	Title   string
	Authors []string
	Year    string
}

type archiveIdentity struct {
	DOI         string
	DOIConflict bool
	ArxivID     string
	Title       string
	Authors     []string
	Year        string
}

// parseArchiveIdentity reads the selected MD5 record's own metadata. In
// particular, it never examines the search result description, where DOI
// strings commonly refer to citations rather than to the result itself.
func parseArchiveIdentity(doc *goquery.Document) archiveIdentity {
	var identity archiveIdentity
	if doc == nil {
		return identity
	}

	// The current record page provides the full title and author list in
	// data-content nodes. Keep the older title/meta/link selectors as fallbacks
	// for mirrors serving the preceding template.
	doc.Find("[data-content][class*=violet-900]").EachWithBreak(func(_ int, node *goquery.Selection) bool {
		identity.Title = strings.TrimSpace(attr(node, "data-content"))
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
		if value := strings.TrimSpace(attr(node, "data-content")); value != "" {
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
	identity.Authors = splitArchiveAuthors(authorTexts)

	dois := make(map[string]struct{})
	doc.Find("meta[name=citation_doi], meta[name='DC.Identifier.DOI'], meta[property='og:doi']").Each(func(_ int, node *goquery.Selection) {
		if candidate := NormalizeDOI(node.AttrOr("content", "")); candidate != "" {
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
			if candidate != "" {
				dois[strings.ToLower(candidate)] = struct{}{}
				if identity.DOI == "" {
					identity.DOI = candidate
				}
			}
		case "arxiv", "arxiv_id", "arxiv-id":
			if identity.ArxivID == "" {
				identity.ArxivID = normalizeArxivID(code.value)
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
// fields. Their search links encode the key/value pair (for example
// q=doi%3A10.1234%2Fexample); similarly shaped links elsewhere on the page are
// deliberately ignored.
func archiveRecordCodes(doc *goquery.Document) []archiveCode {
	var codes []archiveCode
	doc.Find(".js-md5-codes-container a[href^='/search']").Each(func(_ int, node *goquery.Selection) {
		href := node.AttrOr("href", "")
		parsed, err := url.Parse(href)
		if err != nil {
			return
		}
		query := parsed.Query().Get("q")
		key, value, ok := strings.Cut(query, ":")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return
		}
		codes = append(codes, archiveCode{key: strings.TrimSpace(key), value: strings.TrimSpace(value)})
	})
	return codes
}

func splitArchiveAuthors(values []string) []string {
	var authors []string
	for _, value := range values {
		// Anna displays its current full author field as a comma-separated list
		// in one data-content node. Older templates expose one linked author per
		// node. Semicolons/newlines are also used by some mirrors.
		parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' })
		for _, part := range parts {
			if name := strings.TrimSpace(part); name != "" {
				authors = append(authors, name)
			}
		}
	}
	return authors
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

func verifyDOIIdentity(doi string, metadata doiMetadata, record archiveIdentity) bool {
	if record.DOIConflict {
		return false
	}
	if record.DOI != "" && !strings.EqualFold(NormalizeDOI(record.DOI), doi) {
		return false
	}
	wantArxiv := arxivID(doi)
	if record.ArxivID != "" && wantArxiv != "" && normalizeArxivID(record.ArxivID) != wantArxiv {
		return false
	}

	ownIdentifierMatches := record.DOI != "" && strings.EqualFold(NormalizeDOI(record.DOI), doi) ||
		wantArxiv != "" && record.ArxivID == wantArxiv
	if ownIdentifierMatches {
		if metadata.Title != "" && record.Title != "" && normalizeTitle(metadata.Title) != normalizeTitle(record.Title) {
			return false
		}
		if len(metadata.Authors) > 0 && len(record.Authors) > 0 && !sameAuthorSet(metadata.Authors, record.Authors) {
			return false
		}
		if metadata.Year != "" && record.Year != "" && metadata.Year != record.Year {
			return false
		}
		return true
	}
	// Some genuine Anna records do not expose a DOI code. In that case require
	// the CSL title and every author to agree; a title by itself is never enough.
	return metadata.Title != "" && len(metadata.Authors) > 0 &&
		normalizeTitle(metadata.Title) != "" && normalizeTitle(metadata.Title) == normalizeTitle(record.Title) &&
		len(record.Authors) > 0 && sameAuthorSet(metadata.Authors, record.Authors) &&
		(metadata.Year == "" || record.Year == "" || metadata.Year == record.Year)
}

func sameAuthorSet(expected, actual []string) bool {
	if len(expected) == 0 || len(expected) != len(actual) {
		return false
	}
	want := make(map[string]int, len(expected))
	for _, name := range expected {
		normalized := normalizePersonName(name)
		if normalized == "" {
			return false
		}
		want[normalized]++
	}
	for _, name := range actual {
		normalized := normalizePersonName(name)
		if normalized == "" || want[normalized] == 0 {
			return false
		}
		want[normalized]--
	}
	for _, count := range want {
		if count != 0 {
			return false
		}
	}
	return true
}

func normalizePersonName(name string) string {
	var normalized strings.Builder
	space := true
	for _, r := range strings.ToLower(name) {
		r = foldNameRune(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			normalized.WriteRune(r)
			space = false
		} else if !space {
			normalized.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(normalized.String())
}

func foldNameRune(r rune) rune {
	switch r {
	case 'ł':
		return 'l'
	case 'ø':
		return 'o'
	case 'đ', 'ð':
		return 'd'
	}
	// Common precomposed Latin accents are compared without diacritics so the
	// archive's ASCII transliteration and CSL's native spelling can still agree.
	if strings.ContainsRune("àáâãäåāăąǎǟǡǻ", r) {
		return 'a'
	}
	if strings.ContainsRune("èéêëēĕėęě", r) {
		return 'e'
	}
	if strings.ContainsRune("ìíîïĩīĭįǐ", r) {
		return 'i'
	}
	if strings.ContainsRune("òóôõöōŏőǒ", r) {
		return 'o'
	}
	if strings.ContainsRune("ùúûüũūŭůűųǔ", r) {
		return 'u'
	}
	if r == 'ý' || r == 'ÿ' || r == 'ŷ' {
		return 'y'
	}
	if r == 'ć' || r == 'ĉ' || r == 'ċ' || r == 'č' {
		return 'c'
	}
	if r == 'ś' || r == 'ŝ' || r == 'ş' || r == 'š' {
		return 's'
	}
	if r == 'ń' || r == 'ņ' || r == 'ň' || r == 'ñ' {
		return 'n'
	}
	if r == 'ź' || r == 'ż' || r == 'ž' {
		return 'z'
	}
	if r == 'ř' {
		return 'r'
	}
	if r == 'ď' {
		return 'd'
	}
	if r == 'ť' {
		return 't'
	}
	return r
}

func normalizeArxivID(value string) string {
	value = strings.TrimSpace(value)
	if extracted := arxivID(value); extracted != "" {
		return extracted
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{"arxiv:", "arxiv."} {
		if strings.HasPrefix(lower, prefix) {
			return strings.ToLower(stripArxivVersion(value[len(prefix):]))
		}
	}
	return strings.ToLower(stripArxivVersion(value))
}
