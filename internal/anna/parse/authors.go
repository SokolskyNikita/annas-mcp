package parse

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Some imported records append affiliations to the author field: as a
// numbered list ("A , B ;1 FAIR - Facebook AI Research ...", from HAL), after
// a label ("A; B Affiliation: Zoology Department, ..."), as a parenthesized
// block after the last author ("A;B(University of ...;...)"), or after each
// name ("Hamel, R.. Alzheimer Centre Limburg, ...;Köhler, S.. ...").
var (
	numberedAffiliations = regexp.MustCompile(`\s*;\s*1\s+\S`)
	affiliationLabel     = regexp.MustCompile(`(?i)\s*\baffiliations?:`)
	affiliationKeywords  = regexp.MustCompile(`(?i)\b(affiliation|universit|institut|department|division|school|college|laborator|hospital|clinic|academy|research|faculty|centre|center|gmbh)`)
	nameThenAffiliation  = regexp.MustCompile(`^([^,;.()]+, [^.;()]{1,40}?)\.\.? (.+)$`)
)

// CleanAuthors removes affiliations appended to an author field and
// normalizes the spacing around its commas and semicolons. The spacing is
// normalized first, so cleaning a cleaned field changes nothing.
func CleanAuthors(authors string) string {
	authors = spaceSeparators(authors)
	if loc := numberedAffiliations.FindStringIndex(authors); loc != nil {
		authors = authors[:loc[0]]
	}
	if loc := affiliationLabel.FindStringIndex(authors); loc != nil {
		authors = authors[:loc[0]]
	}
	groups := strings.Split(trimAffiliationBlock(authors), ";")
	for i, group := range groups {
		groups[i] = trimNameAffiliation(strings.TrimSpace(group))
	}
	return spaceSeparators(strings.Join(groups, ";"))
}

func spaceSeparators(authors string) string {
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

// trimAffiliationBlock drops everything from the first parenthesized block
// that holds affiliations or an e-mail address: such blocks come after the
// names they belong to. Short notes such as "(ed.)" or "(Translator)" are
// kept. Full-width parentheses come from Chinese catalogs, which use them
// only for affiliations, so short ones go too.
func trimAffiliationBlock(authors string) string {
	for offset := 0; ; {
		open := strings.IndexAny(authors[offset:], "(（")
		if open < 0 {
			return authors
		}
		open += offset
		block := enclosed(authors[open:])
		fullWidth := strings.HasPrefix(block, "（")
		if strings.Contains(block, "@") || affiliationKeywords.MatchString(block) && (len(block) >= 40 || fullWidth) {
			return strings.TrimSpace(authors[:open])
		}
		offset = open + len(block)
	}
}

// enclosed returns the parenthesized block text starts with, up to its
// matching close. Affiliations can nest parentheses ("Institute (Quzhou)")
// and may use full-width ones; an unclosed block runs to the end.
func enclosed(text string) string {
	depth := 0
	for i, r := range text {
		switch r {
		case '(', '（':
			depth++
		case ')', '）':
			depth--
		}
		if depth == 0 {
			return text[:i+utf8.RuneLen(r)]
		}
	}
	return text
}

// trimNameAffiliation turns "Family, Given.. Affiliation, ..." into
// "Family, Given.", keeping the period of a final initial.
func trimNameAffiliation(group string) string {
	match := nameThenAffiliation.FindStringSubmatch(group)
	if match == nil || !affiliationKeywords.MatchString(match[2]) {
		return group
	}
	name := match[1]
	if fields := strings.Fields(name); utf8.RuneCountInString(fields[len(fields)-1]) == 1 {
		name += "."
	}
	return name
}

func splitAuthors(values []string) []string {
	var authors []string
	for _, value := range values {
		// Anna displays its current full author field as a comma-separated list
		// in one data-content node. Older templates expose one linked author per
		// node. Records from some libraries list "Family, Given (role)" names
		// separated by semicolons, where the comma belongs to the name.
		value = CleanAuthors(value)
		separators := func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' }
		if strings.Contains(value, ";") {
			separators = func(r rune) bool { return r == ';' || r == '\n' || r == '\r' }
		}
		for _, part := range strings.FieldsFunc(value, separators) {
			if name := givenFirst(part); name != "" {
				authors = append(authors, name)
			}
		}
	}
	return authors
}

// givenFirst turns "Family, Given (author)" into "Given Family".
func givenFirst(name string) string {
	name = strings.TrimSpace(roleSuffix.ReplaceAllString(name, ""))
	if family, given, ok := strings.Cut(name, ","); ok && !strings.Contains(given, ",") {
		name = strings.TrimSpace(given) + " " + strings.TrimSpace(family)
	}
	return strings.TrimSpace(name)
}

var roleSuffix = regexp.MustCompile(`\s*\((?:author|editor|translator|ed\.|eds\.)\)\s*$`)
