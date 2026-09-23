package modes

// SearchParams is shared by the CLI and MCP. Content describes the kind of work,
// not its file extension. Zero page/timeout values select the operation defaults.
type SearchParams struct {
	Query          string `json:"query" jsonschema:"Title, author, topic, or (for article_search) a DOI, doi: identifier, or https://doi.org/ URL"`
	Content        string `json:"content,omitempty" jsonschema:"Optional content filter: book_fiction, book_nonfiction, book_unknown, book_comic, magazine, or standards_document"`
	Language       string `json:"language,omitempty" jsonschema:"Optional two-letter ISO 639-1 language code, for example en"`
	Page           int    `json:"page,omitempty" jsonschema:"Result page starting at 1. Defaults to 1"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum results from this page. Defaults to 10; raise this before requesting another page"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

type BookDownloadParams struct {
	Hash           string `json:"hash" jsonschema:"32-character MD5 hash from search"`
	Title          string `json:"title" jsonschema:"Book title used for the filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf or epub. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}

type ArticleDownloadParams struct {
	DOI            string `json:"doi,omitempty" jsonschema:"DOI, doi: identifier, or https://doi.org/ URL. Supply exactly one of doi or hash"`
	Hash           string `json:"hash,omitempty" jsonschema:"32-character MD5 hash from search. Supply exactly one of doi or hash"`
	Title          string `json:"title,omitempty" jsonschema:"Optional title override for the saved filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}
