package match

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// sameAuthorSet compares authors by family name and first initial, so
// "Diederik P. Kingma" and "Jimmy Lei Ba" match "Diederik Kingma" and
// "Jimmy Ba". The lists must have the same length.
func sameAuthorSet(expected, actual []string) bool {
	if len(expected) == 0 || len(expected) != len(actual) {
		return false
	}
	want := make(map[string]int, len(expected))
	for _, name := range expected {
		key := personKey(name)
		if key == "" {
			return false
		}
		want[key]++
	}
	for _, name := range actual {
		key := personKey(name)
		if key == "" || want[key] == 0 {
			return false
		}
		want[key]--
	}
	for _, count := range want {
		if count != 0 {
			return false
		}
	}
	return true
}

// personKey is a name's family name and first initial, from a name written
// given name first. Generational suffixes are ignored.
func personKey(name string) string {
	words := strings.Fields(normalizePersonName(name))
	for len(words) > 1 {
		switch words[len(words)-1] {
		case "jr", "sr", "ii", "iii", "iv":
			words = words[:len(words)-1]
			continue
		}
		break
	}
	if len(words) == 0 {
		return ""
	}
	family := words[len(words)-1]
	if len(words) == 1 {
		return family
	}
	initial, _ := utf8.DecodeRuneInString(words[0])
	return family + " " + string(initial)
}

// normalizePersonName lowercases a name, folds the accents of Latin letters
// and keeps letters and digits, separated by single spaces.
func normalizePersonName(name string) string {
	var normalized strings.Builder
	space := true
	for _, r := range foldLatinAccents(strings.ToLower(name)) {
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

// latinLetters are the Latin letters that carry their difference in the
// letter itself rather than in a combining accent.
var latinLetters = map[rune]rune{'ł': 'l', 'ø': 'o', 'đ': 'd', 'ð': 'd', 'ı': 'i'}

// foldLatinAccents drops the accents of Latin letters, so the archive's
// ASCII transliteration and DOI.org's native spelling can agree: "Łukasz
// Kaiser" and "Lukasz Kaiser", "Gülşen" and "Gulsen". Marks on other scripts
// are kept: they tell letters apart there, as in Cyrillic "й" and "и".
// Letters such as "æ", "œ" and "þ" stay as they are.
func foldLatinAccents(text string) string {
	var folded strings.Builder
	var base rune
	for _, r := range norm.NFD.String(text) {
		if unicode.Is(unicode.Mn, r) {
			if unicode.Is(unicode.Latin, base) {
				continue
			}
		} else {
			base = r
			if plain, ok := latinLetters[r]; ok {
				r = plain
			}
		}
		folded.WriteRune(r)
	}
	return norm.NFC.String(folded.String())
}
