package parse

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Kinds of problems the page checks report.
const (
	ProblemUnparsedMeta  = "unrecognized parts of metadata lines"
	ProblemDroppedCard   = "result links without a readable card"
	ProblemIssueMismatch = "cards whose grey-out and issues line disagree"
	ProblemPartialCount  = "partial matches that differ from the page's count"
	ProblemResultCount   = "results that differ from the page's count"
)

// issuesLine is the line Anna adds to the cards it greys out.
const issuesLine = "This file might have issues"

var firstNumber = regexp.MustCompile(`\d+`)

// CheckSearchPage compares what SearchCards read with what the page itself
// shows: each result link should give a card, the numbers of results and
// partial matches should be the ones Anna prints, a greyed-out card should
// carry the issues line, and every part of each metadata line should be
// recognized.
func CheckSearchPage(doc *goquery.Document, cards []Card) Report {
	var report Report
	if doc == nil {
		return report
	}
	read := make(map[string]bool, len(cards))
	exact, partial := 0, 0
	for _, card := range cards {
		read[card.Href] = true
		if card.Partial {
			partial++
		} else {
			exact++
		}
		for _, part := range card.Meta.Unparsed {
			report.Add(ProblemUnparsedMeta, part)
		}
	}
	doc.Find(resultsListSelector).Children().Each(func(_ int, card *goquery.Selection) {
		link := card.Find(titleSelector).First()
		if link.Length() > 0 && strings.Contains(card.Text(), issuesLine) != hasClass(card, possibleIssuesClass) {
			report.Add(ProblemIssueMismatch, link.AttrOr("href", ""))
		}
	})
	doc.Find(resultsListSelector + " a[href]").Each(func(_ int, link *goquery.Selection) {
		href := link.AttrOr("href", "")
		if _, _, ok := RecordPath(href); ok && !read[href] {
			read[href] = true
			report.Add(ProblemDroppedCard, href)
		}
	})
	if line := resultsCount(doc); line.found {
		if shown := max(line.to-line.from+1, 0); shown != exact {
			report.Add(ProblemResultCount, fmt.Sprintf("read %d, the page shows %d", exact, shown))
		}
	}
	shown := 0
	if button := doc.Find(partialButtonSelector).First(); button.Length() > 0 {
		shown, _ = strconv.Atoi(firstNumber.FindString(button.Text()))
	}
	if shown != partial {
		report.Add(ProblemPartialCount, fmt.Sprintf("read %d, the page shows %d", partial, shown))
	}
	return report
}

// CheckRecordDetails reports the parts of a record page's metadata line
// that were not recognized.
func CheckRecordDetails(details RecordDetails) Report {
	var report Report
	for _, part := range details.Meta.Unparsed {
		report.Add(ProblemUnparsedMeta, part)
	}
	return report
}
