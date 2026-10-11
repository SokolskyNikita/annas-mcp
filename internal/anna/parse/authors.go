package parse

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Some imported records append affiliations to the author field, either as a
// numbered list ("A , B ;1 FAIR - Facebook AI Research ...", from HAL) or as
// a parenthesized block after the last author ("A;B(University of ...;...)").
var (
	numberedAffiliations = regexp.MustCompile(`\s*;\s*1\s+\S`)
	affiliationKeywords  = regexp.MustCompile(`(?i)\b(universit|institut|department|school|college|laborator|hospital|academy|research|faculty|centre|center)`)
)

// CleanAuthors removes affiliations appended to an author field and
// normalizes the spacing around its commas and semicolons.
func CleanAuthors(authors string) string {
	if loc := numberedAffiliations.FindStringIndex(authors); loc != nil {
		authors = authors[:loc[0]]
	}
	authors = trimAffiliationBlock(authors)
	var groups []string
	for _, group := range strings.Split(authors, ";") {
		var names []string
		for _, name := range strings.Split(group, ",") {
			if name = strings.Join(strings.Fields(name), " "); name != "" {
				names = append(names, name)
			}
		}
		if len(names) > 0 {
			groups = append(groups, strings.Join(names, ", "))
		}
	}
	return strings.Join(groups, "; ")
}

// trimAffiliationBlock drops the first parenthesized block that holds
// affiliations. Short notes such as "(ed.)" or "(Translator)" are kept.
func trimAffiliationBlock(authors string) string {
	open := strings.IndexAny(authors, "(（")
	if open < 0 {
		return authors
	}
	// Affiliations can nest parentheses ("Institute (Quzhou)") and may use
	// full-width ones, so find the matching close; an unclosed block runs to
	// the end of the field.
	block := authors[open:]
	depth := 0
	for i, r := range block {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			if depth--; depth == 0 {
				block = block[:i+utf8.RuneLen(r)]
			}
		}
		if depth == 0 {
			break
		}
	}
	if len(block) < 40 || !affiliationKeywords.MatchString(block) {
		return authors
	}
	return strings.TrimSpace(authors[:open]) + " " + strings.TrimSpace(authors[open+len(block):])
}
