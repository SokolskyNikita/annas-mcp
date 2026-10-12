package modes

import (
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

// String renders the CLI's plain-text view of a search hit.
func renderBook(b *anna.Book) string {
	pairs := []string{
		"Title", b.Title, "Authors", b.Authors, "Publisher", b.Publisher, "Edition", b.Edition,
		"Language", b.Language, "Format", b.Format, "Size", b.Size,
		"Year", b.Year, "Content", b.Content, "Sources", strings.Join(b.Sources, ", "),
		"Fast download", yesNo(b.FastDownload), "Possible issues", yesOrEmpty(b.PossibleIssues),
		"URL", b.URL, "Hash", b.Hash,
	}
	return labeled(appendStats(pairs, b.Stats)...)
}

// String renders the CLI's plain-text view of an article.
func renderPaper(p *anna.Paper) string {
	pairs := []string{
		"DOI", p.DOI, "Title", p.Title, "Authors", p.Authors, "Journal", p.Journal,
		"Year", p.Year, "Size", p.Size, "Possible issues", yesOrEmpty(p.PossibleIssues),
		"Hash", p.Hash, "Page", p.PageURL,
	}
	return labeled(appendStats(pairs, p.Stats)...)
}

// appendStats keeps confirmed zero counts visible and unknown stats absent.
func appendStats(pairs []string, stats *anna.RecordStats) []string {
	if stats == nil {
		return pairs
	}
	return append(pairs,
		"Downloads", strconv.Itoa(stats.Downloads), "Lists", strconv.Itoa(stats.Lists),
		"Great quality votes", strconv.Itoa(stats.GreatQuality), "Issue reports", strconv.Itoa(stats.Reports),
		"Comments", strconv.Itoa(stats.Comments),
	)
}

// String renders the CLI's plain-text view of a catalog record.
func renderMetadataRecord(r *anna.MetadataRecord) string {
	return labeled(
		"ID", r.ID, "Title", r.Title, "Authors", r.Authors, "Publisher", r.Publisher, "Edition", r.Edition,
		"Language", r.Language, "Year", r.Year, "URL", r.URL,
	)
}

// String renders the CLI's plain-text view of a record's details.
func renderDetails(d *anna.Details) string {
	pairs := []string{
		"ID", d.ID, "Title", d.Title, "Authors", d.Authors, "Publisher", d.Publisher,
		"Edition", d.Edition, "Year", d.Year, "Languages", strings.Join(d.Languages, ", "),
		"Content", d.Content, "Format", d.Format,
	}
	if d.SizeBytes > 0 {
		pairs = append(pairs, "Size", strconv.FormatInt(d.SizeBytes, 10)+" bytes")
	}
	for _, identifier := range detailIdentifiers {
		pairs = append(pairs, identifier.label, strings.Join(d.Identifiers[identifier.key], ", "))
	}
	for _, key := range detailClassifications {
		pairs = append(pairs, strings.ToUpper(key), strings.Join(d.Classifications[key], ", "))
	}
	pairs = append(pairs, "Subjects", strings.Join(d.Subjects, "; "), "Sources", strings.Join(d.Sources, ", "))
	if strings.HasPrefix(d.ID, "md5:") {
		pairs = append(pairs, "Fast download", yesNo(d.FastDownload))
	}
	for _, problem := range d.Problems {
		pairs = append(pairs, "Problem", problem.Description)
	}
	if d.Stats != nil {
		pairs = append(pairs, "Downloads", strconv.Itoa(d.Stats.Downloads), "Great quality votes", strconv.Itoa(d.Stats.GreatQuality), "Issue reports", strconv.Itoa(d.Stats.Reports))
	}
	pairs = append(pairs, "Recently downloaded", yesOrEmpty(d.RecentlyDownloaded), "URL", d.URL, "Description", d.Description)
	return labeled(pairs...)
}

// String renders the CLI's plain-text view of the account's membership.
func renderAccountStatus(s *anna.AccountStatus) string {
	pairs := []string{"Member", yesNo(s.Member), "Fast downloads left", strconv.Itoa(s.DownloadsLeft)}
	if s.DownloadsLimit > 0 {
		limit := strconv.Itoa(s.DownloadsLimit)
		if s.WindowHours > 0 {
			limit += " per " + strconv.Itoa(s.WindowHours) + " hours"
		}
		pairs = append(pairs, "Fast download limit", limit)
	}
	for _, membership := range s.Memberships {
		pairs = append(pairs, "Membership", membership)
	}
	return labeled(pairs...)
}

// labeled formats label/value pairs as "Label: value" lines, skipping empty values.
func labeled(pairs ...string) string {
	lines := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			lines = append(lines, pairs[i]+": "+pairs[i+1])
		}
	}
	return strings.Join(lines, "\n")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func yesOrEmpty(value bool) string {
	if value {
		return "yes"
	}
	return ""
}

var detailIdentifiers = []struct{ key, label string }{
	{"isbn13", "ISBN-13"}, {"isbn10", "ISBN-10"}, {"doi", "DOI"}, {"issn", "ISSN"}, {"iso", "ISO"},
	{"oclc", "OCLC"}, {"lccn", "LCCN"}, {"ol", "Open Library"}, {"goodreads", "Goodreads"},
	{"gbooks", "Google Books"}, {"librarything", "LibraryThing"}, {"ocaid", "Internet Archive"},
	{"asin", "ASIN"}, {"pmid", "PubMed"}, {"arxiv", "arXiv"},
}

var detailClassifications = []string{"ddc", "lcc", "udc"}

func renderRecord(value any) string {
	switch v := value.(type) {
	case *anna.Book:
		return renderBook(v)
	case *anna.Paper:
		return renderPaper(v)
	case *anna.MetadataRecord:
		return renderMetadataRecord(v)
	case *anna.Details:
		return renderDetails(v)
	case *anna.AccountStatus:
		return renderAccountStatus(v)
	default:
		return ""
	}
}
