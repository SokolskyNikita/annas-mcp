package match

import (
	"strings"
	"unicode"
	"unicode/utf8"
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

func normalizePersonName(name string) string {
	var normalized strings.Builder
	space := true
	for _, r := range strings.ToLower(name) {
		r = foldNameRune(r)
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

func foldNameRune(r rune) rune {
	switch r {
	case 'ł':
		return 'l'
	case 'ø':
		return 'o'
	case 'đ', 'ð':
		return 'd'
	}
	// Common precomposed Latin accents are compared without diacritics so the
	// archive's ASCII transliteration and CSL's native spelling can still agree.
	if strings.ContainsRune("àáâãäåāăąǎǟǡǻ", r) {
		return 'a'
	}
	if strings.ContainsRune("èéêëēĕėęě", r) {
		return 'e'
	}
	if strings.ContainsRune("ìíîïĩīĭįǐ", r) {
		return 'i'
	}
	if strings.ContainsRune("òóôõöōŏőǒ", r) {
		return 'o'
	}
	if strings.ContainsRune("ùúûüũūŭůűųǔ", r) {
		return 'u'
	}
	if r == 'ý' || r == 'ÿ' || r == 'ŷ' {
		return 'y'
	}
	if r == 'ć' || r == 'ĉ' || r == 'ċ' || r == 'č' {
		return 'c'
	}
	if r == 'ś' || r == 'ŝ' || r == 'ş' || r == 'š' {
		return 's'
	}
	if r == 'ń' || r == 'ņ' || r == 'ň' || r == 'ñ' {
		return 'n'
	}
	if r == 'ź' || r == 'ż' || r == 'ž' {
		return 'z'
	}
	if r == 'ř' {
		return 'r'
	}
	if r == 'ď' {
		return 'd'
	}
	if r == 'ť' {
		return 't'
	}
	return r
}
