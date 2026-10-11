package anna

import (
	"strconv"
	"strings"
)

// Book is one search hit. Its JSON field names are part of the MCP output.
// FastDownload reports whether Anna's fast download servers have the file,
// which book_download needs; PossibleIssues mirrors the archive's warning
// that the file may be broken or mislabeled.
type Book struct {
	Title          string       `json:"title"`
	Authors        string       `json:"authors"`
	Publisher      string       `json:"publisher"`
	Edition        string       `json:"edition,omitempty"`
	Language       string       `json:"language"`
	Format         string       `json:"format"`
	Size           string       `json:"size"`
	Year           string       `json:"year,omitempty"`
	Content        string       `json:"content,omitempty"`
	Sources        []string     `json:"sources,omitempty"`
	FastDownload   bool         `json:"fast_download"`
	PossibleIssues bool         `json:"possible_issues,omitempty"`
	Hash           string       `json:"hash"`
	URL            string       `json:"url"`
	DOI            string       `json:"doi,omitempty"`
	Description    string       `json:"description,omitempty"`
	Stats          *RecordStats `json:"stats,omitempty"`
}

// Paper is a journal article found by DOI or keyword search. DownloadURL is
// set when Anna's SciDB page embeds the PDF directly. It is a signed link
// issued to the member session, so it is used internally and never output.
// FastDownload is only known for search hits, so it is omitted when false.
// Journal is the record's publisher and edition line, which for articles
// usually names the journal, volume and issue.
type Paper struct {
	DOI            string       `json:"doi,omitempty"`
	Title          string       `json:"title,omitempty"`
	Authors        string       `json:"authors"`
	Journal        string       `json:"journal"`
	Year           string       `json:"year,omitempty"`
	Format         string       `json:"format,omitempty"`
	Size           string       `json:"size"`
	Sources        []string     `json:"sources,omitempty"`
	FastDownload   bool         `json:"fast_download,omitempty"`
	PossibleIssues bool         `json:"possible_issues,omitempty"`
	Hash           string       `json:"hash,omitempty"`
	Description    string       `json:"description,omitempty"`
	DownloadURL    string       `json:"-"`
	PageURL        string       `json:"page_url"`
	Stats          *RecordStats `json:"stats,omitempty"`
}

// MetadataRecord is a catalog entry from a source such as Open Library,
// WorldCat or ISBNdb. It describes a book but has no file of its own.
type MetadataRecord struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Authors     string `json:"authors"`
	Publisher   string `json:"publisher"`
	Edition     string `json:"edition,omitempty"`
	Language    string `json:"language"`
	Year        string `json:"year,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url"`
}

// SearchOptions narrows one page of Anna's Archive results. Index is an
// Anna search tab such as "journals" or "meta". The list filters take
// upstream values; a leading "-" excludes a value instead of requiring it.
// book_any and journal are legacy Content values and are not sent.
type SearchOptions struct {
	Index        string
	Page         int
	Sort         string
	Content      []string
	Language     []string
	Format       []string
	Source       []string
	Access       []string
	Fields       []SearchField
	Descriptions bool
}

// SearchField limits one metadata field to a phrase. Name is an Anna field
// such as title, author, publisher or year.
type SearchField struct {
	Name  string
	Value string
}

// SearchPage is one page of results. Results honor every filter. Partial
// holds the looser matches Anna's Archive adds to a short first page; they
// may ignore the filters. Total counts all matches, and is only a lower
// bound when TotalIsLowerBound is set.
type SearchPage[T any] struct {
	Results           []T
	Partial           []T
	Total             int
	TotalIsLowerBound bool
	MorePages         bool
}

// DownloadResult is the file returned by a successful download request.
// DownloadsLeft is included when a fresh fast download reports the account's
// allowance, and omitted when an existing local file is reused.
type DownloadResult struct {
	Path          string `json:"path"`
	Bytes         int64  `json:"bytes"`
	DownloadsLeft *int   `json:"downloads_left,omitempty"`
}

// ProgressFunc reports bytes written. total is 0 when the server omits Content-Length.
type ProgressFunc func(done, total int64)

// String renders the CLI's plain-text view of a search hit.
func (b *Book) String() string {
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
func (p *Paper) String() string {
	pairs := []string{
		"DOI", p.DOI, "Title", p.Title, "Authors", p.Authors, "Journal", p.Journal,
		"Year", p.Year, "Size", p.Size, "Possible issues", yesOrEmpty(p.PossibleIssues),
		"Hash", p.Hash, "Page", p.PageURL,
	}
	return labeled(appendStats(pairs, p.Stats)...)
}

// appendStats keeps confirmed zero counts visible and unknown stats absent.
func appendStats(pairs []string, stats *RecordStats) []string {
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
func (r *MetadataRecord) String() string {
	return labeled(
		"ID", r.ID, "Title", r.Title, "Authors", r.Authors, "Publisher", r.Publisher, "Edition", r.Edition,
		"Language", r.Language, "Year", r.Year, "URL", r.URL,
	)
}

// String renders the CLI's plain-text view of a record's details.
func (d *Details) String() string {
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
func (s *AccountStatus) String() string {
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
