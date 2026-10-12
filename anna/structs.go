package anna

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
