package match

import (
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

func TestAuthorNamesPreservesNonLatinNames(t *testing.T) {
	t.Parallel()
	if sameAuthorSet([]string{"李 王"}, []string{"山 田"}) || sameAuthorSet([]string{"Иван Петров"}, []string{"Анна Сергеева"}) {
		t.Fatal("distinct non-Latin authors collapsed to the same identity")
	}
}

func TestAuthorNamesPreservesDistinctLetters(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"Ægir Smith", "Agir Smith"}, {"Œdipe Smith", "Odipe Smith"}, {"Þor Smith", "Tor Smith"}} {
		metadata := parse.Citation{Title: "Shared title", Authors: []string{pair[0]}}
		record := parse.Identity{Title: "Shared title", Authors: []string{pair[1]}}
		if SameWork("10.1000/example", metadata, record) {
			t.Errorf("accepted different authors %q and %q", pair[0], pair[1])
		}
		record.Authors = []string{strings.ToLower(pair[0])}
		if !SameWork("10.1000/example", metadata, record) {
			t.Errorf("rejected the same author %q", pair[0])
		}
	}
}

func TestPersonKey(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"Diederik P. Kingma":     "kingma d",
		"Martin Luther King Jr.": "king m",
		"John Smith III":         "smith j",
		"Plato":                  "plato",
		"Jr":                     "jr",
		"  ":                     "",
		"Łukasz Kaiser":          "kaiser l",
	} {
		if got := personKey(name); got != want {
			t.Errorf("personKey(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNormalizePersonNameFoldsLatinAccents(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"Łø Đð Àé Íò Ùý Ćś Ńź Řď Ť": "lo dd ae io uy cs nz rd t",
		"Gülşen Özdemir-Ağaoğlu":    "gulsen ozdemir agaoglu",
		"Ştefan Ţurcanu":            "stefan turcanu",
		"İsmail Kılıç":              "ismail kilic",
		"Ægir Œdipe Þor":            "ægir œdipe þor",
		"Йошкар Иванов":             "йошкар иванов",
		"ガウス 李 Ли":                  "ガウス 李 ли",
		"A\u0301ngel":               "angel",
	} {
		if got := normalizePersonName(name); got != want {
			t.Errorf("normalizePersonName(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSameAuthorSet(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		expected, actual []string
		want             bool
	}{
		{[]string{"Ada Lovelace", "Charles Babbage"}, []string{"Charles Babbage", "Ada Lovelace"}, true},
		{[]string{"Ada Lovelace", "Ada Lovelace"}, []string{"Ada Lovelace", "Alan Lovelace"}, true},
		{[]string{"Ada Lovelace", "Ada Lovelace"}, []string{"Ada Lovelace", "Bob Lovelace"}, false},
		{[]string{"Ada Lovelace"}, []string{"Ada Lovelace", "Charles Babbage"}, false},
		{nil, nil, false},
		{[]string{"..."}, []string{"Ada Lovelace"}, false},
		{[]string{"Ada Lovelace"}, []string{"..."}, false},
	} {
		if got := sameAuthorSet(tc.expected, tc.actual); got != tc.want {
			t.Errorf("sameAuthorSet(%q, %q) = %v, want %v", tc.expected, tc.actual, got, tc.want)
		}
	}
}
