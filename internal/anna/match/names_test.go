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
