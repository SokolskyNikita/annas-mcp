// Package match decides whether a record in Anna's Archive is the work a
// DOI names, comparing what the record page shows with DOI.org's citation
// data. Both sides are read by package parse.
package match

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// SameWork reports whether an archive record is the work a DOI names, as
// DOI.org describes it. A record's own DOI or arXiv id decides when it has
// one, as long as its title, authors and year don't contradict DOI.org's;
// otherwise the title and every author must agree.
func SameWork(doi string, metadata parse.Citation, record parse.Identity) bool {
	if record.DOIConflict {
		return false
	}
	if record.DOI != "" && !strings.EqualFold(parse.NormalizeDOI(record.DOI), doi) {
		return false
	}
	wantArxiv := parse.ArxivID(doi)
	if record.ArxivID != "" && wantArxiv != "" && parse.NormalizeArxivID(record.ArxivID) != wantArxiv {
		return false
	}

	ownIdentifierMatches := record.DOI != "" && strings.EqualFold(parse.NormalizeDOI(record.DOI), doi) ||
		wantArxiv != "" && record.ArxivID == wantArxiv
	if ownIdentifierMatches {
		if metadata.Title != "" && record.Title != "" && NormalizeTitle(metadata.Title) != NormalizeTitle(record.Title) {
			return false
		}
		if len(metadata.Authors) > 0 && len(record.Authors) > 0 && !sameAuthorSet(metadata.Authors, record.Authors) {
			return false
		}
		if metadata.Year != "" && record.Year != "" && !sameYear(doi, metadata.Year, record.Year) {
			return false
		}
		return true
	}
	// Some genuine Anna records do not expose a DOI code. In that case require
	// the CSL title and every author to agree; a title by itself is never enough.
	return metadata.Title != "" && len(metadata.Authors) > 0 &&
		NormalizeTitle(metadata.Title) != "" && NormalizeTitle(metadata.Title) == NormalizeTitle(record.Title) &&
		len(record.Authors) > 0 && sameAuthorSet(metadata.Authors, record.Authors) &&
		(metadata.Year == "" || record.Year == "" || sameYear(doi, metadata.Year, record.Year))
}

// sameYear compares publication years. An arXiv DOI is dated by the
// preprint, and the journal or conference version the archive holds is often
// published the following year.
func sameYear(doi, want, got string) bool {
	if want == got {
		return true
	}
	wantYear, err1 := strconv.Atoi(want)
	gotYear, err2 := strconv.Atoi(got)
	return err1 == nil && err2 == nil && parse.ArxivID(doi) != "" && gotYear == wantYear+1
}

// NormalizeTitle lowercases a title and reduces it to letters and digits
// separated by single spaces, for comparing titles.
func NormalizeTitle(title string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}
