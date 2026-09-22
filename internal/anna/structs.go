package anna

import "fmt"

type Book struct {
	Language  string `json:"language"`
	Format    string `json:"format"`
	Size      string `json:"size"`
	Title     string `json:"title"`
	Publisher string `json:"publisher"`
	Authors   string `json:"authors"`
	URL       string `json:"url"`
	Hash      string `json:"hash"`
}

type Paper struct {
	DOI         string `json:"doi"`
	Title       string `json:"title,omitempty"`
	Authors     string `json:"authors"`
	Journal     string `json:"journal"`
	Size        string `json:"size"`
	Hash        string `json:"hash,omitempty"`
	DownloadURL string `json:"download_url"`
	SciHubURL   string `json:"scihub_url,omitempty"`
	PageURL     string `json:"page_url"`
}

func (p *Paper) String() string {
	return fmt.Sprintf("DOI: %s\nTitle: %s\nAuthors: %s\nJournal: %s\nSize: %s\nHash: %s\nDownload URL: %s\nSci-Hub: %s\nPage: %s",
		p.DOI, p.Title, p.Authors, p.Journal, p.Size, p.Hash, p.DownloadURL, p.SciHubURL, p.PageURL)
}

type fastDownloadResponse struct {
	DownloadURL *string `json:"download_url"`
	Error       string  `json:"error"`
}

// SearchOptions narrows one page of Anna's Archive results.
type SearchOptions struct {
	Content  string
	Language string
	Page     int
}

// DownloadResult is the file written by a successful download.
type DownloadResult struct {
	Path  string
	Bytes int64
}

// ProgressFunc reports bytes written. total is 0 when the server omits Content-Length.
type ProgressFunc func(done, total int64)
