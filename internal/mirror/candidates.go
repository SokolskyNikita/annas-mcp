package mirror

import (
	"sort"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Candidate is a mirror listed in the current SLUM directory.
//
// SLUM's current page publishes the availability status in the directory, so
// the old heartbeat endpoint and its monitor IDs are intentionally not part of
// this model.
type Candidate struct {
	BaseURL string
	Status  string
}

// ParseStatusPageHTML extracts mirrors from the current SLUM directory. The
// status page is HTML, so use the DOM rather than regexes over generated
// markup. A missing or empty Anna card is reported as no candidates and lets
// Resolve use its configured fallback.
func ParseStatusPageHTML(html string) ([]Candidate, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var card *goquery.Selection
	doc.Find(".site-card").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		title := strings.ToLower(strings.TrimSpace(selection.Find(".site-card-title").First().Text()))
		if title == "annas" || title == "anna's archive" || title == "anna’s archive" {
			card = selection
			return false
		}
		return true
	})
	if card == nil {
		return nil, nil
	}

	candidates := make([]Candidate, 0, maxMirrorCandidates)
	seen := make(map[string]struct{})
	card.Find(".domain-item-dense").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if len(candidates) >= maxMirrorCandidates {
			return false
		}
		href, ok := item.Find("a[href]").First().Attr("href")
		if !ok {
			return true
		}
		baseURL, parseErr := ParseCandidateURL(href)
		if parseErr != nil {
			return true
		}
		if _, exists := seen[baseURL]; exists {
			return true
		}
		seen[baseURL] = struct{}{}
		candidates = append(candidates, Candidate{
			BaseURL: baseURL,
			Status:  statusFromDirectoryItem(item),
		})
		return true
	})

	return candidates, nil
}

func rankCandidates(candidates []Candidate) []Candidate {
	filtered := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		switch strings.ToLower(strings.TrimSpace(candidate.Status)) {
		case "down", "timeout":
			continue
		default:
			filtered = append(filtered, candidate)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		leftRank := statusRank(filtered[i].Status)
		rightRank := statusRank(filtered[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return filtered[i].BaseURL < filtered[j].BaseURL
	})
	return filtered
}

func statusFromDirectoryItem(item *goquery.Selection) string {
	badge := item.Find(".status-badge").First()
	if badge.Length() == 0 {
		return ""
	}
	if classes, ok := badge.Attr("class"); ok {
		for _, class := range strings.Fields(strings.ToLower(classes)) {
			switch class {
			case "up", "protected", "degraded", "down", "timeout", "unknown":
				return class
			}
		}
	}
	text := strings.ToLower(strings.TrimSpace(badge.Text()))
	for _, status := range []string{"protected", "degraded", "timeout", "down", "up", "unknown"} {
		if strings.Contains(text, status) {
			return status
		}
	}
	return ""
}

func statusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "up":
		return 0
	case "protected":
		return 1
	case "degraded":
		return 2
	case "unknown", "":
		return 3
	default:
		return 4
	}
}
