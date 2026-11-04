package modes

// SearchParams describes the common book and legacy archive search filters. List
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
	IncludeStats       bool   `json:"include_stats,omitempty" jsonschema:"Include public per-file community counts (downloads, quality votes, reports, lists and comments). Adds a statistics lookup for returned file hashes"`
	Page               int    `json:"page,omitempty" jsonschema:"Result page starting at 1. Defaults to 1"`
	Limit              int    `json:"limit,omitempty" jsonschema:"Maximum results from this page. Defaults to 10; raise this before requesting another page"`
	TimeoutSeconds     int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// ArticleSearchParams adds scholarly discovery controls without changing book search.
type ArticleSearchParams struct {
	SearchParams
	Provider      string `json:"provider,omitempty" jsonschema:"Search provider: auto (default), openalex or anna. Auto uses OpenAlex when configured; archive-specific file filters select Anna's Archive"`
	WorkType      string `json:"work_type,omitempty" jsonschema:"Scholarly work types, comma-separated, such as article, review, preprint, conference-paper or dissertation. Use dissertation for doctoral and master's theses, including works without a DOI; any includes all work types"`
	AuthorID      string `json:"author_id,omitempty" jsonschema:"Exact OpenAlex author ID or ORCID to disambiguate an author"`
	InstitutionID string `json:"institution_id,omitempty" jsonschema:"OpenAlex institution ID for author affiliation. This does not establish which institution awarded a thesis"`
	RepositoryID  string `json:"repository_id,omitempty" jsonschema:"OpenAlex source ID for a repository, returned by article_details"`
	Relation      string `json:"relation,omitempty" jsonschema:"Optional citation relationship: citing (works citing work_id), references (works referenced by work_id), or related. Requires work_id"`
	WorkID        string `json:"work_id,omitempty" jsonschema:"DOI or OpenAlex W ID used as the seed for relation"`
	Cursor        string `json:"cursor,omitempty" jsonschema:"Opaque OpenAlex continuation cursor. Replay next_search_args from the previous page, retaining its provider and filters"`
}

// ArticleDetailsParams selects a scholarly work or archive file for inspection.
type ArticleDetailsParams struct {
	ID             string `json:"id" jsonschema:"DOI, OpenAlex W ID or work URL, or Anna's Archive MD5 or file-page URL"`
	Provider       string `json:"provider,omitempty" jsonschema:"auto (default), openalex or anna. Explicit choices constrain the provider"`
	LocationsPage  int    `json:"locations_page,omitempty" jsonschema:"Page of available copies, starting at 1. Details include replayable download arguments for each supported copy"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// ArticleReadParams requests bounded structured full text for a selected work.
type ArticleReadParams struct {
	ID             string `json:"id" jsonschema:"DOI or OpenAlex W ID or work URL. Required on the first request; retain the ID when replaying next_args"`
	Continuation   string `json:"continuation,omitempty" jsonschema:"Opaque continuation returned by article_read. Replay next_args verbatim to read the next section without downloading the content again"`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum characters in this passage. Defaults to 8000; maximum 16000"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including content retrieval. Defaults to 1800; maximum 86400"`
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
	Provider       string `json:"provider,omitempty" jsonschema:"Optional provider: anna or openalex. Defaults to Anna when configured, otherwise OpenAlex. Reports allowance in the provider's own units; no status check is needed before downloading"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 60; maximum 86400"`
}

// BookDownloadParams are the book_download tool arguments.
type BookDownloadParams struct {
	Hash           string `json:"hash" jsonschema:"32-character MD5 hash or Anna's Archive /md5/ page URL"`
	Title          string `json:"title" jsonschema:"Book title used for the filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf or epub. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}

// ArticleDownloadParams are the article_download tool arguments. Exactly one
// of DOI, OpenAlexID or Hash is required.
type ArticleDownloadParams struct {
	OpenAlexID     string `json:"openalex_id,omitempty" jsonschema:"OpenAlex W ID or work URL, including a dissertation without a DOI. Supply exactly one of doi, openalex_id or hash"`
	Provider       string `json:"provider,omitempty" jsonschema:"auto (default), openalex or anna. Auto can use a hosted PDF, open repository or publisher PDF, then Anna's Archive. An explicit provider constrains fallback"`
	LocationID     string `json:"location_id,omitempty" jsonschema:"Exact location ID from article_details. Copy it unchanged to request only that copy"`
	Version        string `json:"version,omitempty" jsonschema:"Optional strict version: published, accepted or submitted. Unknown-version hosted PDFs cannot satisfy an explicit version"`
	Artifact       string `json:"artifact,omitempty" jsonschema:"pdf (default) or tei_xml for structured OpenAlex full text. Hosted content requests consume the OpenAlex allowance. File extensions do not convert content"`
	DOI            string `json:"doi,omitempty" jsonschema:"DOI, doi: identifier or https://doi.org/ URL. Supply exactly one of doi, openalex_id or hash"`
	Hash           string `json:"hash,omitempty" jsonschema:"32-character MD5 hash or Anna's Archive /md5/ page URL. Supply exactly one of doi, openalex_id or hash"`
	Title          string `json:"title,omitempty" jsonschema:"Optional title override for the saved filename"`
	Format         string `json:"format,omitempty" jsonschema:"Optional filename extension such as pdf. Inferred when omitted; does not convert the file"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty" jsonschema:"Total operation timeout in seconds, including retries. Defaults to 1800; maximum 86400"`
}
