package anna

import "fmt"

type Book struct {
	Language    string `json:"language"`
	Format      string `json:"format"`
	Size        string `json:"size"`
	Title       string `json:"title"`
	Publisher   string `json:"publisher"`
	Authors     string `json:"authors"`
	Description string `json:"description,omitempty"`
	DOI         string `json:"doi,omitempty"`
	URL         string `json:"url"`
	Hash        string `json:"hash"`
}

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

func (p *Paper) String() string {
	return fmt.Sprintf("DOI: %s\nTitle: %s\nAuthors: %s\nJournal: %s\nSize: %s\nHash: %s\nDownload URL: %s\nPage: %s",
		p.DOI, p.Title, p.Authors, p.Journal, p.Size, p.Hash, p.DownloadURL, p.PageURL)
}

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

func (b *Book) String() string {
	return fmt.Sprintf("Title: %s\nAuthors: %s\nPublisher: %s\nLanguage: %s\nFormat: %s\nSize: %s\nURL: %s\nHash: %s",
		b.Title, b.Authors, b.Publisher, b.Language, b.Format, b.Size, b.URL, b.Hash)
}
