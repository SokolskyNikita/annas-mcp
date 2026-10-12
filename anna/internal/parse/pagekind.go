package parse

import (
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// IsArchivePage reports whether doc is one of Anna's Archive's own pages,
// rather than a browser check or another site's answer.
func IsArchivePage(doc *goquery.Document) bool {
	if doc == nil {
		return false
	}
	title := strings.ToLower(doc.Find("title").First().Text())
	return strings.Contains(title, "anna's archive") || strings.Contains(title, "anna’s archive") || doc.Find("form[action='/search'] input[name='q']").Length() > 0
}
