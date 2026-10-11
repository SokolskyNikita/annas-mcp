package modes

// SearchParams is shared by book_search, article_search and the CLI. List
// filters take comma-separated values, and a leading "-" excludes a value.
// Content describes the kind of work, not its file extension. Zero
// page/timeout values select the operation defaults.
type SearchParams struct {
	Query              string `json:"query,omitempty" jsonschema:"Words to look for in titles, authors, publishers and other metadata. For article_search, also a DOI, doi: identifier or https://doi.org/ URL. Optional when title, author, publisher or year is set"`
	Title              string `json:"title,omitempty" jsonschema:"Optional phrase the title must contain"`
	Author             string `json:"author,omitempty" jsonschema:"Optional phrase the author must contain"`
	Publisher          string `json:"publisher,omitempty" jsonschema:"Optional phrase the publisher must contain"`
	Year               string `json:"year,omitempty" jsonschema:"Optional publication year, for example 2008"`
	Format             string `json:"format,omitempty" jsonschema:"Optional file extensions, comma-separated, such as epub, pdf, mobi, azw3, djvu, fb2 or cbz. Prefix one with - to exclude it, as in -pdf"`
	Language           string `json:"language,omitempty" jsonschema:"Optional language codes, comma-separated, for example en or en,de. Prefix one with - to exclude it"`
	Content            string `json:"content,omitempty" jsonschema:"Optional content types, comma-separated: book_nonfiction, book_fiction, book_unknown, book_comic, magazine, standards_document, musical_score, audiobook or other. Prefix one with - to exclude it"`
	Source             string `json:"source,omitempty" jsonschema:"Optional source libraries, comma-separated, such as lgli (Libgen.li), lgrs (Libgen.rs), zlib, ia, scihub, nexusstc or upload. Prefix one with - to exclude it"`
	Sort               string `json:"sort,omitempty" jsonschema:"Optional order: relevance (default), newest or oldest (publication year), largest or smallest (file size), newest_added or oldest_added (date added to the archive), or random"`
	DownloadableOnly   bool   `json:"downloadable_only,omitempty" jsonschema:"Only return files the download tools can fetch: fast downloads, and for article_search also SciDB"`
	SearchDescriptions bool   `json:"search_descriptions,omitempty" jsonschema:"Also match query against descriptions and metadata comments"`
	Page               int    `json:"page,omitempty" jsonschema:"Result page starting at 1. Defaults to 1"`
	Limit              int    `json:"limit,omitempty" jsonschema:"Maximum results from this page. Defaults to 10; raise this before requesting another page"`
	TimeoutSeconds     int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// MetadataSearchParams are the metadata_search tool arguments.
type MetadataSearchParams struct {
	Query          string `json:"query,omitempty" jsonschema:"Words to look for, or an ISBN. Optional when title, author, publisher or year is set"`
	Title          string `json:"title,omitempty" jsonschema:"Optional phrase the title must contain"`
	Author         string `json:"author,omitempty" jsonschema:"Optional phrase the author must contain"`
	Publisher      string `json:"publisher,omitempty" jsonschema:"Optional phrase the publisher must contain"`
	Year           string `json:"year,omitempty" jsonschema:"Optional publication year, for example 2008"`
	Language       string `json:"language,omitempty" jsonschema:"Optional language codes, comma-separated, for example en or en,de. Prefix one with - to exclude it"`
	Source         string `json:"source,omitempty" jsonschema:"Optional catalogs, comma-separated, such as ol (Open Library), oclc (WorldCat), isbndb, goodreads or gbooks (Google Books). Prefix one with - to exclude it"`
	Sort           string `json:"sort,omitempty" jsonschema:"Optional order: relevance (default), newest or oldest (publication year), newest_added or oldest_added, or random"`
	Page           int    `json:"page,omitempty" jsonschema:"Result page starting at 1. Defaults to 1"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum results from this page. Defaults to 10; raise this before requesting another page"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// DetailsParams are the book_details tool arguments.
type DetailsParams struct {
	ID             string `json:"id" jsonschema:"A 32-character hash from book_search or article_search, or a record id such as isbndb:9780521879286 from metadata_search"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// AccountStatusParams are the account_status tool arguments.
type AccountStatusParams struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// BookDownloadParams are the book_download tool arguments.
type BookDownloadParams struct {
	Hash           string `json:"hash" jsonschema:"32-character MD5 hash from search"`
	Title          string `json:"title" jsonschema:"Book title used for the filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf or epub. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}

// ArticleDownloadParams are the article_download tool arguments. Exactly one
// of DOI or Hash is required.
type ArticleDownloadParams struct {
	DOI            string `json:"doi,omitempty" jsonschema:"DOI, doi: identifier or https://doi.org/ URL. Supply exactly one of doi or hash"`
	Hash           string `json:"hash,omitempty" jsonschema:"32-character MD5 hash from search. Supply exactly one of doi or hash"`
	Title          string `json:"title,omitempty" jsonschema:"Optional title override for the saved filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}
