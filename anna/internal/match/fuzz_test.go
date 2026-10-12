package match

import (
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// Seeds for both are in testdata/fuzz.

func FuzzNormalizePersonName(f *testing.F) {
	f.Fuzz(func(t *testing.T, name string) {
		once := normalizePersonName(name)
		if twice := normalizePersonName(once); twice != once {
			t.Fatalf("normalizePersonName is not stable: %q -> %q -> %q", name, once, twice)
		}
	})
}

// A record describing the work exactly as DOI.org does is that work.
func FuzzSameWorkAcceptsTheCitationItself(f *testing.F) {
	f.Fuzz(func(t *testing.T, doi, title, authorList, year string) {
		authors := strings.Split(authorList, ";")
		citation := parse.Citation{DOI: doi, Title: title, Authors: authors, Year: year}
		if NormalizeTitle(title) == "" {
			return
		}
		for _, author := range authors {
			if personKey(author) == "" {
				return
			}
		}
		record := parse.Identity{Title: title, Authors: authors, Year: year}
		if !SameWork(doi, citation, record) {
			t.Fatalf("a record identical to the citation was rejected: %+v", citation)
		}
	})
}
