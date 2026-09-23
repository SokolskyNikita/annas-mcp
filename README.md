# annas-mcp

`annas-mcp` is a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server and command-line client for searching Anna's Archive for books and journal articles, then downloading a selected file. It exposes the same operations to Cursor, Claude, Codex, and other MCP clients as it does to a shell script.

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)

The archive's contents and access rules vary by work and account. Use the service only for material you are entitled to obtain and follow the applicable law and service terms.

## Quick start

The npm launcher requires Node.js 18 or newer and a `tar` command that supports `.tar.xz` archives (`.zip` on Windows). It downloads the release binary for the current OS and CPU, verifies the published SHA-256 checksum, stores it in a local cache, and starts it. A later launch checks for a newer release with a short bounded request; if the check or download fails, a valid cached binary is used.

Configure the environment in the MCP client's `env` block. Set an Anna's Archive account cookie when the upstream search or mirror requires a browser session, a configured download directory for downloads, and the account's API key for fast downloads. DOI article downloads can use the SciDB route without the fast-download key; hash-based fast downloads require `ANNAS_SECRET_KEY`. See [Anna's Archive's API FAQ](https://annas-archive.gl/faq#api) for the service's current account and API requirements.

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "github:SokolskyNikita/annas-mcp"],
      "env": {
        "ANNAS_ACCOUNT_COOKIE": "aa_account_id2-value",
        "ANNAS_SECRET_KEY": "your-api-key",
        "ANNAS_DOWNLOAD_PATH": "/absolute/path/to/downloads"
      }
    }
  }
}
```

The same launcher can be tried from a terminal:

```bash
npx -y github:SokolskyNikita/annas-mcp --help
npx -y github:SokolskyNikita/annas-mcp book-search "distributed systems" --language en
```

The launcher cache can be moved with `ANNAS_MCP_CACHE_DIR`. Keep the cache on a local, user-writable filesystem. A cached binary is selected only when its release metadata and target asset still match.

### Client configuration examples

Claude Code:

```bash
claude mcp add annas-mcp \
  --env ANNAS_ACCOUNT_COOKIE=aa_account_id2-value \
  --env ANNAS_SECRET_KEY=your-api-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  -- npx -y github:SokolskyNikita/annas-mcp
```

Codex, in `~/.codex/config.toml`:

```toml
[mcp_servers.annas-mcp]
command = "npx"
args = ["-y", "github:SokolskyNikita/annas-mcp"]

[mcp_servers.annas-mcp.env]
ANNAS_ACCOUNT_COOKIE = "aa_account_id2-value"
ANNAS_SECRET_KEY = "your-api-key"
ANNAS_DOWNLOAD_PATH = "/absolute/path/to/downloads"
```

Cursor and Claude Desktop use the JSON shape shown above. Never commit real cookies or API keys.

## Account cookie

`ANNAS_ACCOUNT_COOKIE` may contain the raw `aa_account_id2` value, a single `aa_account_id2=...` pair, or a full browser `Cookie` header. The server extracts and sends only `aa_account_id2` to the configured Anna's Archive mirror.

To copy it, open Anna's Archive in a browser, open developer tools, and either copy `aa_account_id2` from Application/Storage → Cookies or copy it from a request in the Network tab. A missing, stale, or otherwise rejected cookie can produce a 403. Refresh the browser value, update the client configuration, and restart the MCP process so it reads the new value.

## MCP tools

The server communicates over stdio. Search before downloading so the client can pass the returned MD5 hash and title to the download tool. Every successful download returns the absolute path and byte count:

```json
{"path":"/absolute/path/to/downloads/title.pdf","bytes":123456}
```

Successful tool calls include both structured JSON output for programs and a text representation for clients that display tool text. Schema-invalid arguments can be rejected by the MCP SDK at the protocol boundary before a handler runs; validly shaped calls that fail inside the service return an application error with `isError: true` and a stable code.

Every tool accepts an optional `timeout_seconds`. Search defaults to 60 seconds; downloads default to 1,800 seconds. `0` uses the command default, and the maximum is 86,400 seconds (24 hours). A timeout applies to the complete operation and can be increased for a slow mirror.

### `book_search`

Search books, textbooks, manuals, standards, or other book records by title, author, or topic.

```json
{
  "query": "Designing Data-Intensive Applications",
  "language": "en",
  "page": 1,
  "limit": 10,
  "timeout_seconds": 60
}
```

`content` is optional. Supported values are `book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine`, and `standards_document`.

Keyword searches return one page:

```json
{
  "page": 1,
  "language": "en",
  "matched": 2,
  "limit": 2,
  "results": [
    {
      "hash": "abc123def4567890abc123def4567890",
      "title": "Designing Data-Intensive Applications",
      "authors": "Martin Kleppmann",
      "publisher": "O'Reilly Media",
      "language": "English",
      "format": "EPUB",
      "size": "3.1MB",
      "description": "A short record excerpt.",
      "url": "https://annas-archive.gl/md5/abc123def4567890abc123def4567890"
    }
  ]
}
```

`matched` is the number of parsed records on the current upstream page. `limit` is the number returned after this tool's limit is applied. A missing `description` or `doi` field means the source page did not provide it.

### `book_download`

Download a book by the `hash` returned by `book_search`.

```json
{
  "hash": "abc123def4567890abc123def4567890",
  "title": "Designing Data-Intensive Applications",
  "format": "epub",
  "timeout_seconds": 1800
}
```

`title` is used to form the local filename. `format` is an optional filename extension; it selects the name and does not convert the file. If omitted, the extension is inferred from the download response. Downloads are limited to 8 GiB per file. The downloader verifies the expected MD5 when the hash is known, removes incomplete or invalid files, keeps an existing file instead of overwriting it, and adds a short hash or numeric suffix on a name collision.

### `article_search`

Search journal articles by keywords, or resolve one article by DOI. The query may be a bare DOI such as `10.48550/arXiv.1706.03762` or a DOI URL such as `https://doi.org/10.48550/arXiv.1706.03762`.

For keywords:

```json
{
  "query": "attention mechanisms",
  "language": "en",
  "page": 1,
  "limit": 10
}
```

Keyword results have the same pagination fields as `book_search`, usually include `"index":"journals"`, and use `journal` and `page_url` in place of a book's `publisher` and `url`.

For a DOI:

```json
{"query":"https://doi.org/10.48550/arXiv.1706.03762"}
```

The result is one article object rather than a page envelope. Its fields can include `doi`, `title`, `authors`, `journal`, `format`, `size`, `hash`, `description`, `download_url`, and `page_url`. The `hash` is the value to pass to `article_download` when it is present.

### `article_download`

Download an article by exactly one of `doi` or a `hash` from `article_search`.

```json
{"doi":"10.48550/arXiv.1706.03762","format":"pdf"}
```

When a hash is already known, use the direct path and provide a title if one is available:

```json
{
  "hash": "abc123def4567890abc123def4567890",
  "title": "Attention Is All You Need",
  "format": "pdf"
}
```

The DOI path resolves the record, tries the configured fast-download service when the record has a hash, and can fall back to the article's SciDB download route. DOI-only downloads can therefore work without `ANNAS_SECRET_KEY`; a hash-only article download requires that key. The same path and byte-count result applies. `format` remains a filename extension request; it never converts EPUB, PDF, or another source format.

## Search and download workflow for AI clients

1. Call `book_search` or `article_search` with the user's exact title, citation, DOI, or topic.
2. Inspect `title`, `authors`, `format`, `description`, `doi`, and `hash`; ask the user when several records are plausible.
3. Pass the selected record's `hash` and `title` to the corresponding download tool. Use a DOI for `article_download` when no hash is available.
4. If the desired record is not on the first page, raise `limit` first and then advance `page`. An empty page means that no records were parsed from that upstream page; narrow or revise the query when necessary.
5. Return the tool's `path` to the user. The returned file has the requested extension in its name, subject to the response's detected type when `format` is omitted.

## Pagination and limits

MCP search tools return at most 10 hits by default. Set `limit` to a larger positive value when the desired record is not among the first results, then use `page` to inspect later upstream pages. `page` starts at 1. `language` is a lowercase ISO 639 language code such as `en`.

Use current content filters; legacy values such as `book_any` are not useful search filters.

## Errors

The CLI exits nonzero and prints a coded message. For MCP calls, schema-invalid arguments may be rejected by the SDK before the handler runs. Validly shaped calls that fail inside the service return `isError: true`, and their text starts with a stable machine-readable code.

| Code | Meaning and next action |
| --- | --- |
| `[CONFIG]` | A required environment variable is missing or invalid. Check `ANNAS_ACCOUNT_COOKIE`, `ANNAS_SECRET_KEY`, `ANNAS_DOWNLOAD_PATH`, and mirror settings. |
| `[INVALID_ARGUMENT]` | An input is malformed or out of range. Correct the DOI, hash, extension, page, limit, or timeout and retry. |
| `[NOT_FOUND]` | A DOI or search did not resolve to a usable record. Try a title search, a larger limit, or another page. |
| `[UPSTREAM_BLOCKED]` | Anna's Archive or its mirror refused the request, commonly because the account cookie is missing or rejected. Refresh the cookie and check configured access. |
| `[REQUEST_TIMEOUT]` | The request was cancelled or exceeded its timeout. Retry or increase `timeout_seconds`. |
| `[UPSTREAM]` | The archive, mirror, API, or download failed for another reason. Retry, select a different configured mirror, or inspect the message. |

## Configuration

The CLI loads a `.env` file from its process working directory. For an MCP client, prefer its explicit `env` configuration because the working directory is usually where the client was launched, not where this repository or binary is stored.

| Variable | Required for | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | Search and mirror requests when required upstream | Browser cookie input. Only the `aa_account_id2` value is used. It is an operational access credential, not a startup prerequisite. |
| `ANNAS_SECRET_KEY` | Fast downloads | API key accepted by the configured Anna's Archive account. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory for downloaded files. It is created when needed. |
| `ANNAS_BASE_URL` | Optional mirror fallback | Anna's Archive hostname or HTTPS base URL used as the fallback mirror. |
| `ANNAS_AUTO_BASE_URL` | Optional mirror discovery | Automatic mirror selection is enabled by default. Set to `false` to use `ANNAS_BASE_URL` only. |
| `ANNAS_MCP_CACHE_DIR` | Optional npm launcher cache | Directory for downloaded release binaries and metadata. |

Automatic mirror selection is lazy: the first archive request performs status-page discovery and probing within a 15-second budget rather than delaying MCP startup. A successful selection is cached for 10 minutes; a fallback selection after discovery failure is cached for 30 seconds. If the selected mirror later fails, it is invalidated so the next request can rediscover. When discovery cannot select a mirror, the server uses explicitly configured `ANNAS_BASE_URL`, or `annas-archive.gl` when no base URL is configured. Set `ANNAS_AUTO_BASE_URL=false` when a fixed mirror is required.

## CLI

The CLI uses the same validation and download code as the MCP tools. Search commands print the whole upstream page by default; `--limit` only limits what the CLI prints. `--json` emits machine-readable JSON for scripts and agents.

```bash
# Search
annas-mcp book-search "distributed systems" --language en --page 1 --limit 20
annas-mcp article-search "attention mechanisms" --json
annas-mcp article-search "10.1038/nature12373" --json

# Download by a search result hash
annas-mcp book-download abc123def4567890abc123def4567890 "my-book.epub"
annas-mcp article-download --hash abc123def4567890abc123def4567890 \
  --title "Attention Is All You Need" --format pdf

# Download an article by DOI
annas-mcp article-download "10.1038/nature12373" --format pdf

# Start the stdio server explicitly
annas-mcp mcp
```

Useful shared flags include `--timeout` (for example `30s` or `10m`, up to 24h), `--page`, `--language`, `--content`, and `--limit`. An omitted timeout uses the command default; an explicit `--timeout` must be positive. Run `annas-mcp --help` or a subcommand's `--help` for the current command-specific details.

## Build from source

Building from source requires Go 1.26 or newer. The MCP server uses `github.com/modelcontextprotocol/go-sdk` v1.8.0.

```bash
git clone https://github.com/SokolskyNikita/annas-mcp.git
cd annas-mcp
go run ./cmd/annas-mcp mcp
go build -o ./annas-mcp ./cmd/annas-mcp
go install ./cmd/annas-mcp
```

`go run` and the installed local binary use the current checkout. `go install github.com/SokolskyNikita/annas-mcp/cmd/annas-mcp@latest` installs the latest published release, which can predate untagged changes in this checkout; use it after a release is published when you want the released version.

The source command reads `.env` from the current working directory. Configure the same environment variables shown above before calling search or download commands. To point an MCP client at this checkout, build the binary and use an absolute path:

```toml
[mcp_servers.annas-mcp]
command = "/absolute/path/to/annas-mcp"
args = ["mcp"]
```

The `npx -y github:SokolskyNikita/annas-mcp` command fetches this repository’s launcher, which starts the latest published GitHub release binary. It does not run uncommitted source changes from a local checkout; use `go run`, `go build`, or the local `go install` command above while developing.

The main packages are arranged as follows:

- `cmd/annas-mcp` is the executable entry point.
- `internal/modes` contains the shared MCP and CLI service, validation, and output handling.
- `internal/anna` handles archive search, article lookup, and downloads.
- `internal/mirror` discovers and probes archive mirrors; the archive client caches the selection.
- `internal/env`, `internal/apperr`, and `internal/version` hold configuration, stable application errors, and the embedded release version.
- `scripts` contains launcher tests, health checks, and immutable release-tag tooling.

## Development

Run the checks used by the project before opening a pull request:

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
node scripts/test-npx-launcher.mjs
scripts/healthcheck.sh              # local build and CLI checks
scripts/healthcheck.sh --live       # optional upstream checks
```

The healthcheck is local by default. `--live` (or `ANNAS_MCP_HEALTHCHECK_LIVE=1`) enables archive requests; `ANNAS_MCP_HEALTHCHECK_TIMEOUT` sets the per-check timeout in seconds.

The release workflow validates pull requests and publishes only an immutable `vMAJOR.MINOR.PATCH` tag. Keep the embedded version, npm package version, and release tag synchronized. Before a release, run the full test suite, verify the GoReleaser configuration, update both version files, commit the change, and run `scripts/manage-tag.sh add` to create and push the next tag (for example `v0.0.10`). Do not move or recreate an existing release tag: the npm launcher caches binaries by release identity and checksum.

## Project lineage

This is [SokolskyNikita's fork](https://github.com/SokolskyNikita/annas-mcp) of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp). Contributions that improve search correctness, download integrity, mirror resilience, MCP schemas, and cross-platform launcher behavior are welcome.
