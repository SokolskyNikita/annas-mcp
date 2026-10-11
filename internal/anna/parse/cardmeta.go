package parse

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// CardMeta is a card's metadata line. Language joins up to three language
// names, most likely first; Content is a content type such as
// book_nonfiction; FastDownload marks files on Anna's fast download servers.
type CardMeta struct {
	Language     string   `json:"language,omitempty"`
	Format       string   `json:"format,omitempty"`
	Size         string   `json:"size,omitempty"`
	Year         string   `json:"year,omitempty"`
	Content      string   `json:"content,omitempty"`
	Sources      []string `json:"sources,omitempty"`
	FastDownload bool     `json:"fast_download,omitempty"`
	// Unparsed holds parts of the line that were not recognized, apart from
	// a leading label such as "Metadata" and the record id after the
	// sources.
	Unparsed []string `json:"unparsed,omitempty"`
}

// The content label is translated, but its emoji is not.
var contentEmoji = map[rune]string{
	'📗': "book_unknown", '📘': "book_nonfiction", '📕': "book_fiction",
	'📄': "journal_article", '📝': "standards_document", '📰': "magazine",
	'💬': "book_comic", '🎶': "musical_score", '🎧': "audiobook", '🤨': "other",
}

// ParseCardMeta reads the metadata line. Its parts are optional but always
// in this order: languages, extension, size, year, content type, sources
// (prefixed by 🧬 for SciDB and 🚀 for fast downloads) and a record id.
func ParseCardMeta(line string) CardMeta {
	const (
		stageLanguage = iota
		stageFormat
		stageSize
		stageYear
		stageContent
		stageSources
		stageDone
	)
	var meta CardMeta
	stage := stageLanguage
	index, recordID := -1, false
	for _, part := range strings.Split(line, "·") {
		part = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "✅"))
		if part == "" {
			continue
		}
		index++
		if stage <= stageLanguage {
			// A record can list up to three languages, most likely first.
			if match := cardLanguagePattern.FindStringSubmatch(part); match != nil {
				if meta.Language != "" {
					meta.Language += ", "
				}
				meta.Language += match[1]
				continue
			}
		}
		if stage <= stageFormat && cardFormatPattern.MatchString(part) && strings.IndexFunc(part, unicode.IsLetter) >= 0 {
			meta.Format, stage = part, stageSize
			continue
		}
		if stage <= stageSize && cardSizePattern.MatchString(part) {
			meta.Size, stage = part, stageYear
			continue
		}
		if stage <= stageYear && cardYearPattern.MatchString(part) {
			meta.Year, stage = part, stageContent
			continue
		}
		if stage <= stageContent {
			first, _ := utf8.DecodeRuneInString(part)
			if content, ok := contentEmoji[first]; ok {
				meta.Content, stage = content, stageSources
				continue
			}
		}
		// Sources follow the content type, which every record has; a lone
		// lowercase word earlier on, such as a translated label, is not one.
		if stage == stageSources || (stage < stageSources && strings.Contains(part, "/")) {
			if sources, fast, ok := parseSources(part); ok {
				meta.Sources, meta.FastDownload, stage = sources, fast, stageDone
				continue
			}
		}
		switch {
		case index == 0 && stage == stageLanguage:
			// A label such as "Metadata" leads catalog records' lines.
		case stage == stageDone && !recordID:
			recordID = true
		default:
			meta.Unparsed = append(meta.Unparsed, part)
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
