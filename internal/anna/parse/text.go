package parse

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Some records' text was HTML-escaped before Anna escaped it for its page,
// so it reads "Smith &amp; Jones" after the page itself is decoded.
// Only complete entities are decoded: legacy forms without a semicolon,
// such as "&para", are ordinary text here.
var escapedEntity = regexp.MustCompile(`&(?:#[0-9]{1,7}|#[xX][0-9a-fA-F]{1,6}|[A-Za-z][A-Za-z0-9]{1,31});`)

func decodeEntities(text string) string {
	if !strings.Contains(text, "&") {
		return text
	}
	return escapedEntity.ReplaceAllStringFunc(text, html.UnescapeString)
}

// Records imported from publishers can carry formatting markup in their
// text, such as "<i>AlphaFold</i>2" or Crossref's <jats:p>. Only known
// formatting tags are removed, so text such as "Vector<T>" is kept.
var (
	inlineMarkup = regexp.MustCompile(`(?i)</?(?:a|b|i|em|strong|sub|sup|u|span|small|sc|tt|code|(?:jats|mml):[a-z-]+)(?:\s[^<>]*)?/?>`)
	blockMarkup  = regexp.MustCompile(`(?i)</?(?:p|br|div|li|ul|ol|h[1-6])(?:\s[^<>]*)?/?>`)
)

// CleanText decodes doubly escaped entities, removes formatting markup and
// collapses whitespace.
func CleanText(text string) string {
	text = decodeEntities(text)
	if strings.Contains(text, "<") {
		text = blockMarkup.ReplaceAllString(inlineMarkup.ReplaceAllString(text, ""), " ")
	}
	return strings.Join(strings.Fields(text), " ")
}

// Snippet collapses whitespace and shortens text to at most 480 bytes,
// cutting at a word where it can.
func Snippet(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	const limit = 480
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndex(cut, " "); i > 320 {
		cut = cut[:i]
	}
	return cut
}
