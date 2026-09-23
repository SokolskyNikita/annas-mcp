package modes

type BookSearchParams struct {
	Query          string `json:"query" mcp:"Search query for books (title, author, or topic)"`
	Content        string `json:"content,omitempty" mcp:"Optional content filter: book_fiction, book_nonfiction, magazine, or standards_document"`
	Language       string `json:"language,omitempty" mcp:"Optional ISO 639-1 language code, for example en"`
	Page           int    `json:"page,omitempty" mcp:"Result page, starting at 1. Raise it when the current page is not enough"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" mcp:"Optional HTTP timeout in seconds. Defaults to 60"`
}

type BookDownloadParams struct {
	BookHash       string `json:"hash" mcp:"MD5 hash of the book to download"`
	Title          string `json:"title" mcp:"Book title, used for the filename"`
	Format         string `json:"format,omitempty" mcp:"Optional file extension, for example pdf or epub. Detected from the download when omitted"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" mcp:"Optional HTTP timeout in seconds. Defaults to 1800"`
}

type ArticleSearchParams struct {
	Query          string `json:"query" mcp:"DOI (for example 10.1038/nature12345) or search keywords. Keywords search journal articles"`
	Content        string `json:"content,omitempty" mcp:"Optional file-type filter. Keywords use the journals index unless this is set"`
	Language       string `json:"language,omitempty" mcp:"Optional ISO 639-1 language code, for example en"`
	Page           int    `json:"page,omitempty" mcp:"Result page, starting at 1"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" mcp:"Optional HTTP timeout in seconds. Defaults to 60"`
}

type ArticleDownloadParams struct {
	DOI            string `json:"doi,omitempty" mcp:"DOI of the article to download. Optional when hash is set"`
	Hash           string `json:"hash,omitempty" mcp:"MD5 hash from article_search. Optional when doi is set"`
	Title          string `json:"title,omitempty" mcp:"Optional title, used for the filename when downloading by hash"`
	Format         string `json:"format,omitempty" mcp:"Optional file extension, for example pdf. Detected from the download when omitted"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" mcp:"Optional HTTP timeout in seconds. Defaults to 1800"`
}
