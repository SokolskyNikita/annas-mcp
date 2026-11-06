# How annas-mcp works

For setup and everyday use, see the [README](README.md).

## Shared Go client

The public `anna` package owns archive requests, validation, authentication, mirror selection, parsers, citation matching, statistics and verified downloads. The `papers` package coordinates OpenAlex and AA article operations. The MCP and CLI adapters use those shared APIs; environment defaults, tool envelopes and text rendering remain application responsibilities. See the [SDK contract](https://github.com/SokolskyNikita/annas-mcp/blob/main/anna/README.md). Parsers, matching and mirrors are private packages under `anna/internal/`, and their captured fixtures and maintenance tooling live with the SDK.

## Tools

Every tool accepts `timeout_seconds`, which covers the whole operation including retries. Downloads and structured full-text reads default to 30 minutes, and the metadata tools to 60 seconds.

### Research search, details and reading

`article_search` accepts keywords, a title/citation, DOI or OpenAlex work ID/URL. `provider=auto` selects OpenAlex when its key is configured for an ordinary research request; `provider=anna` or `provider=openalex` constrains the backend. AA-specific file filters retain their archive meaning. Exact identity lookup returns one work, while keyword search returns a bounded page. Search and details do not fetch file bodies.

For OpenAlex searches, use `work_type=dissertation` to include doctoral and master’s theses, including older repository deposits. A thesis can have an OpenAlex W ID and no DOI. Author, year or year range, language, exact author/institution ID, and repository ID help narrow results. A recorded institution can describe an author affiliation. Repository metadata alone does not establish a degree-awarding institution. A language label describes indexed metadata and may differ from the PDF.

Search hits include provider, identity, access evidence and directly usable `details_args` and `download_args`. Follow `next_search_args` to preserve the selected provider, query and paging state. An OpenAlex-hosted PDF, a reported direct open PDF, an archive fast download and a landing page are different access evidence. A result’s citation count describes the work; archive activity statistics describe an individual file hash.

`article_details` accepts a DOI, W ID/URL or archive MD5/page URL and exposes the selected work’s identifiers, abstract, authors, copies, licenses and versions. Location IDs identify copies and can be passed through to download. Use a strict version only when needed. The hosted PDF’s version can remain unknown even when a repository copy’s version is recorded. Citation relations on `article_search` find a bounded page of citing, referenced or related works.

`article_read` returns bounded passages of OpenAlex structured full text with work and section provenance and continuation arguments. Its first content fetch can consume OpenAlex allowance. Reusing the continuation reads from the cached artifact. If only an abstract or a public PDF link is available, the result makes that distinction explicit. Downloaded XML is structured text and can have extraction gaps.

### Archive searching

The archive `book_search` and `article_search` paths return one page of results for a `query`. Each result includes a `hash`, title, authors, year, format, size and source libraries. Books also include the language, publisher, edition and content type, and articles the journal. `fast_download` says whether Anna's fast download servers have the file, which the download tools need, and `possible_issues` marks files the archive warns might be broken.

Optional fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `title`, `author`, `publisher` | Any | A phrase that field must contain. With any of these or `year`, `query` can be left out. |
| `year` | Any | Publication year, such as `2008`. |
| `format` | Any | File extensions such as `epub`, `pdf`, `mobi`, `azw3` or `djvu`. |
| `language` | Any | Language codes such as `en`. |
| `content` | Any | `book_nonfiction`, `book_fiction`, `book_unknown`, `book_comic`, `magazine`, `standards_document`, `musical_score`, `audiobook` or `other`. |
| `source` | Any | Source libraries such as `lgli` (Libgen.li), `lgrs` (Libgen.rs), `zlib`, `ia`, `scihub`, `nexusstc` or `upload`. An unknown one is an error that lists the valid ones. |
| `sort` | Relevance | `newest` or `oldest` (publication year), `largest` or `smallest` (file size), `newest_added` or `oldest_added`, or `random`. |
| `downloadable_only` | `false` | Only files the download tools can fetch: fast downloads, plus SciDB for `article_search`. |
| `search_descriptions` | `false` | Also match `query` against descriptions and metadata comments. |
| `include_stats` | `false` | Add public activity counts to returned file hits when available. This adds requests and may add latency. |
| `limit` | 10 | Results to return from the page. Raise this before asking for another page. |
| `page` | 1 | Result page. Anna's Archive pages hold 50 results. |

`format`, `language`, `content` and `source` take comma-separated lists, and a leading `-` excludes a value: `"format": "epub,mobi"`, `"source": "-scihub"`.

Every result in `results` matches all filters. `total` counts all matches (it is a lower bound when `total_is_lower_bound` is set), and `more_pages` says whether another page exists. When a first page has fewer results than `limit`, Anna's Archive suggests looser matches; these come back separately as `partial_matches` and may ignore the filters.

Give `article_search` a DOI (`10.1038/nature14539`, `doi:…` or `https://doi.org/…`) to get that one article instead of a page of results.

With `include_stats: true`, returned book and article hits (including partial matches) can include a `stats` object with `downloads`, `great_quality`, `reports`, `lists` and `comments`, matching the counters in `book_details`. These are counts for that file's MD5 hash: recorded downloads are the main popularity signal, lists count list membership, quality counts positive quality votes, reports count problem reports, and comments count direct comments. Counts are per file, not combined across editions or formats; they may be cached upstream for up to a day. An unavailable lookup omits the whole `stats` field, while a confirmed zero is returned as `0`. DOI results include stats when a file hash is available. Catalog records from `metadata_search` do not have file stats. The option does not change search ordering or produce a combined popularity score.

### Catalogs and details

`metadata_search` searches catalog records from Open Library, WorldCat, ISBNdb, Goodreads, Google Books and other sources, including books that have no downloadable file. It takes the same `query`, `title`, `author`, `publisher`, `year`, `language`, `sort`, `limit` and `page` fields, and its `source` filter takes catalogs such as `ol`, `oclc`, `isbndb`, `goodreads` or `gbooks`. Each result has an `id` such as `isbndb:9780521879286`. To find files of an edition it lists, search `book_search` for the ISBN.

`book_details` takes a `hash` from either search tool or an `id` from `metadata_search` as its `id`, and returns the full record: ISBNs, DOI, OCLC, Open Library and other identifiers, the edition, the full description, subjects and Dewey/Library of Congress classes, alternative titles and authors, problems source libraries reported for the file (with a better copy's hash when the archive knows one), and download and quality counts.

### Archive downloads

Both download tools accept a bare MD5 or an Anna's Archive `/md5/...` page URL in `hash`; the server extracts the hash automatically.

On the AA route, `book_download` takes the `hash` and a `title`. `article_download` takes either a `doi` or a `hash`, plus an optional `title`. Both return the file's `path` and size in `bytes`; a fresh fast download also returns `downloads_left`, the account's remaining fast downloads. Local reuse omits `downloads_left` because it makes no fresh account request. `account_status` returns `member`, `downloads_left`, `downloads_limit`, `window_hours` and your active `memberships`; the [README](README.md#tools) explains the allowance.

Before downloading file bytes, the server checks completed regular files directly in the configured download folder against the full MD5 hash. A match is reused regardless of filename or extension, so the returned path can differ from the requested title. `article_download` by DOI resolves the DOI to a hash before this check. If there is no match, `title` becomes the filename. Pass `format` (such as `epub` or `pdf`) to set the extension; this doesn't convert the file, and without it the extension is detected from the download. An existing file is never overwritten: a name clash gets a short suffix.

Files are checked against the archive's MD5 hash, and incomplete or corrupt downloads are deleted. A download tries up to seven download servers in turn, and when the file is stored in more than one collection and every server fails, up to two more collections. Trying another server or collection doesn't use another fast download. When the fast download servers don't have a paper, or the account has no fast downloads left, `article_download` by DOI falls back to the PDF on Anna's SciDB page. When every server answers that it doesn't have the file, a download by hash fails with `NOT_FOUND`, and for a paper the error names its DOI so the download can be retried with `article_download`.

### Coordinated article retrieval

A caller that knows the DOI or W ID and wants a file can call `article_download` immediately. A caller starting from a title should search and select the identity first. Supply exactly one of `doi`, `openalex_id` or `hash`. With OpenAlex configured, an ordinary DOI request tries a verified reusable local copy, an OpenAlex-hosted PDF, eligible direct open PDFs, and then the existing AA DOI downloader if archive access is configured. AA still resolves its own file hash and signed URL internally. OpenAlex cannot infer the AA download URL.

An explicit archive hash selects the exact file. A W ID selects the scholarly work, including a no-DOI dissertation. An explicit provider, location, version or artifact constrains eligible retrieval routes. A no-DOI thesis cannot silently fall back to a similar AA title. Hosted PDFs use the `pdf` artifact; explicit `tei_xml` requests obtain structured full text. A PDF is saved with a PDF extension.

Download results retain `path` and `bytes` and add source and reuse evidence. Paths are absolute on the server host. OpenAlex files have a recorded local digest for later reuse. The cache is revalidated against local file bytes before returning a reused file. AA downloads retain their source MD5 checks. All providers use temporary files and unique final names. Failed or cancelled transfers do not publish a partial final file.

One deadline covers resolution, eligible fallback and file transfer. Provider failures preserve their cause and any known work/landing link. An invalid key and exhausted budget require a different next step from a temporary throttle or unavailable upstream. Local I/O errors and cancellation stop the operation. `account_status` can inspect either provider; an ordinary search or download does not need a status preflight.

### Credentials and account allowance

Read `OPENALEX_API_KEY` from the process environment for OpenAlex and `ANNAS_SECRET_KEY` or the configured archive session for AA. OpenAlex-only operations work independently of archive membership. Keep the OpenAlex token on the official API/content hosts and fetch direct repository/publisher PDFs without provider credentials. Search is metered according to the OpenAlex account plan, and hosted file or structured-text requests use content allowance. Report provider units as supplied by its usage API.

## Choosing a mirror

The server picks among `ANNAS_BASE_URL` and the official mirrors `annas-archive.gl`, `.pk` and `.gd`, or uses only `ANNAS_BASE_URL` when `ANNAS_AUTO_BASE_URL` is `false`.

It contacts the mirrors only while a tool call is running, never in the background. The first call checks them all at once on Anna's uptime endpoint and uses the first that answers. After that it keeps using that mirror for as long as it works, without checking it again.

A mirror that fails is set aside: when it can't be reached, doesn't answer within 30 seconds, answers with a server error or returns a page that isn't Anna's Archive. It is set aside for 15 seconds, then twice as long with each further failure in a row, up to 5 minutes. A call that hits a failing mirror is retried once on another. A `403` counts against a mirror only when another mirror then works; when two refuse, the problem is the account, and the error says so.

When every mirror is set aside, calls fail at once with what each mirror last did and when to try again. Until one works again, the mirrors are checked at most once every 30 seconds.

Which mirrors worked or failed recently is saved as `mirrors.json` in the [cache folder](#the-cache-folder). A new run, a CLI command or another MCP client uses a mirror that worked in the last 10 minutes without checking it, and skips one that just failed. The file holds no credentials, and deleting it is safe.

## The cache folder

On first run `npx` downloads the latest release and caches it once its checksum matches. Later runs start from the cache and pick up new releases automatically.

On macOS and Linux with Node 22.15 or later, the launcher then replaces its own Node process with the binary, so only the server keeps running. `npx` itself still waits beside it; the [Homebrew install](README.md#homebrew-lighter-on-memory) avoids that too. Set `ANNAS_MCP_LAUNCHER_SPAWN=1` to have the launcher start the binary as a child instead, as it does on Windows and older Node versions.

The cache folder is `ANNAS_MCP_CACHE_DIR` if you set it, otherwise `~/.cache/annas-mcp` on macOS and Linux (`$XDG_CACHE_HOME/annas-mcp` if that is set) and `%LOCALAPPDATA%\annas-mcp` on Windows. The server saves its [session](#the-saved-session) and [mirror health](#choosing-a-mirror) there too.

## The saved session

The server saves its session as `session.json` in the [cache folder](#the-cache-folder), so restarts and CLI commands don't sign in again. Only you can read the file. It holds the session cookie plus SHA-256 fingerprints of your secret key and of any `ANNAS_ACCOUNT_COOKIE` the session replaced. Deleting it is safe; the server signs in again.

Once an `ANNAS_ACCOUNT_COOKIE` expires and the server has signed in with the secret key instead, it skips that cookie until you set a different one. `ANNAS_ACCOUNT_COOKIE` accepts the bare value, `aa_account_id2=…` or a whole `Cookie` header copied from the Network tab. Only `aa_account_id2` is ever sent.
