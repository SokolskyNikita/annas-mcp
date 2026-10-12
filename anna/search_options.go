package anna

import (
	"strings"
)

const (
	browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// ResolveSearch applies legacy content values and the default for a search.
// book_any is not a current Anna filter and makes later pages empty, so it is dropped.
// journal is the old article default and maps to the journals index, as does
// journal_article outside the metadata index: that tab holds only articles.
func ResolveSearch(options SearchOptions, defaultContent string) SearchOptions {
	options.Index = strings.ToLower(strings.TrimSpace(options.Index))
	if options.Page < 1 {
		options.Page = 1
	}
	content := make([]string, 0, len(options.Content))
	for _, value := range options.Content {
		switch value = strings.ToLower(strings.TrimSpace(value)); value {
		case "", "book_any":
		case "journal", "journal_article":
			if options.Index == "" {
				options.Index = "journals"
			}
			if value == "journal_article" && options.Index != "journals" {
				content = append(content, value)
			}
		default:
			content = append(content, value)
		}
	}
	options.Content = content
	if len(options.Content) == 0 && options.Index == "" {
		switch defaultContent {
		case "journal", "journals":
			options.Index = "journals"
		case "", "book_any":
		default:
			options.Content = []string{defaultContent}
		}
	}
	return options
}
