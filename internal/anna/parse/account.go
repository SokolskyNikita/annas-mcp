package parse

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	downloadsUsedPattern = regexp.MustCompile(`^\s*(\d+)\s*/\s*(\d+)\s*$`)
	firstNumberPattern   = regexp.MustCompile(`\d+`)
)

// AccountPage is what the account page shows: whether the session is signed
// in, the fast downloads used of the allowance over a window of WindowHours,
// and the active memberships.
type AccountPage struct {
	SignedIn    bool
	Used, Limit int
	WindowHours int
	Memberships []string
}

// ParseAccountPage reads the account page, which shows active memberships
// and "Fast downloads used (last 18 hours): 4 / 125".
func ParseAccountPage(doc *goquery.Document) AccountPage {
	var page AccountPage
	if doc == nil {
		return page
	}
	page.SignedIn = doc.Find("a[href='/account/secret_key']").Length() > 0
	doc.Find("a[href='/account/downloaded']").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		line := link.Parent()
		match := downloadsUsedPattern.FindStringSubmatch(line.Find("strong").First().Text())
		if match == nil {
			return true
		}
		page.Used, _ = strconv.Atoi(match[1])
		page.Limit, _ = strconv.Atoi(match[2])
		if hours := firstNumberPattern.FindString(ownText(line)); hours != "" {
			page.WindowHours, _ = strconv.Atoi(hours)
		}
		return false
	})
	// Expired memberships stay listed with line-through.
	doc.Find("a[href^='/donate?tier=']").Each(func(_ int, link *goquery.Selection) {
		line := link.Parent()
		tier := strings.Join(strings.Fields(line.Find("strong").First().Text()), " ")
		if tier == "" || hasClass(line, "line-through") {
			return
		}
		text := strings.Join(strings.Fields(line.Clone().Find("a").Remove().End().Text()), " ")
		if i := strings.Index(text, tier); i >= 0 {
			text = text[i:]
		}
		page.Memberships = append(page.Memberships, text)
	})
	return page
}
