package anna

import "strings"

// Book is one search hit. Its JSON field names are part of the MCP output.
type Book struct {
	Title       string `json:"title"`
	Authors     string `json:"authors"`
	Publisher   string `json:"publisher"`
	Language    string `json:"language"`
	Format      string `json:"format"`
	Size        string `json:"size"`
	Hash        string `json:"hash"`
	URL         string `json:"url"`
	DOI         string `json:"doi,omitempty"`
	Description string `json:"description,omitempty"`
}

// Paper is a journal article found by DOI or keyword search. DownloadURL is
// set when Anna's SciDB page embeds the PDF directly.
type Paper struct {
	DOI         string `json:"doi,omitempty"`
	Title       string `json:"title,omitempty"`
	Authors     string `json:"authors"`
	Journal     string `json:"journal"`
	Format      string `json:"format,omitempty"`
	Size        string `json:"size"`
	Hash        string `json:"hash,omitempty"`
	Description string `json:"description,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
	PageURL     string `json:"page_url"`
}

// fastDownloadResponse is the body of /dyn/api/fast_download.json. A failed
// request sets Error and leaves DownloadURL null.
type fastDownloadResponse struct {
	DownloadURL *string `json:"download_url"`
	Error       string  `json:"error"`
}

// SearchOptions narrows one page of Anna's Archive results.
// Index is an Anna search tab such as "journals". Content is a file-type filter
// such as "book_nonfiction". book_any and journal are legacy values and are not sent.
type SearchOptions struct {
	Content  string
	Language string
	Index    string
	Page     int
}

// DownloadResult is the file written by a successful download.
type DownloadResult struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// ProgressFunc reports bytes written. total is 0 when the server omits Content-Length.
type ProgressFunc func(done, total int64)

// String renders the CLI's plain-text view of a search hit.
func (b *Book) String() string {
	return labeled(
		"Title", b.Title, "Authors", b.Authors, "Publisher", b.Publisher,
		"Language", b.Language, "Format", b.Format, "Size", b.Size,
		"URL", b.URL, "Hash", b.Hash,
	)
}

// String renders the CLI's plain-text view of an article.
func (p *Paper) String() string {
	return labeled(
		"DOI", p.DOI, "Title", p.Title, "Authors", p.Authors, "Journal", p.Journal,
		"Size", p.Size, "Hash", p.Hash, "Download URL", p.DownloadURL, "Page", p.PageURL,
	)
}

// labeled formats label/value pairs as "Label: value" lines.
func labeled(pairs ...string) string {
	lines := make([]string, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		lines = append(lines, pairs[i]+": "+pairs[i+1])
	}
	return strings.Join(lines, "\n")
}
