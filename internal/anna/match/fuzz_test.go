package match

import (
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
)

func FuzzNormalizePersonName(f *testing.F) {
	for _, seed := range []string{"Łukasz Kaiser", "İsmail Kılıç", "Йошкар", "ガウス", "Ángel", "  Jr.  III "} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		once := normalizePersonName(name)
		if twice := normalizePersonName(once); twice != once {
			t.Fatalf("normalizePersonName is not stable: %q -> %q -> %q", name, once, twice)
		}
	})
}

// A record describing the work exactly as DOI.org does is that work.
func FuzzSameWorkAcceptsTheCitationItself(f *testing.F) {
	f.Add("10.1000/x", "Attention Is All You Need", "Ashish Vaswani;Noam Shazeer", "2017")
	f.Add("10.48550/arXiv.1412.6980", "Adam", "Diederik P. Kingma", "")
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
