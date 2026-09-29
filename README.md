# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)

`annas-mcp` searches [Anna's Archive](https://annas-archive.gl) for books and journal articles and downloads the file you pick. It runs as a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server for Claude, Cursor, Codex and other MCP clients. It also runs as a command-line tool. Both modes share the same validation and download code.

| MCP tool | CLI command | What it does |
| --- | --- | --- |
| [`book_search`](#book_search) | `book-search` | Finds books, textbooks, manuals and standards by title, author or topic |
| [`article_search`](#article_search) | `article-search` | Finds journal articles by keyword or resolves one by DOI |
| [`book_download`](#book_download) | `book-download` | Downloads a book by the hash from a search result |
| [`article_download`](#article_download) | `article-download` | Downloads an article by DOI or hash |

Use it only for material you are entitled to obtain, under the laws and service terms that apply to you.

## Quick start

You need:

- An Anna's Archive membership, its account cookie (renewed weekly) and an API key. Searches require **Lucky Librarian** or higher; downloads work with any tier. See [Membership and credentials](#membership-and-credentials).
- Node.js 18 or newer.
- On macOS and Linux, a `tar` that handles `.tar.xz` archives. On Windows the launcher extracts the `.zip` release with the system's built-in `tar.exe`, even when started from Git Bash.

Add the server to your MCP client and replace the placeholders with your credentials. Cursor (`~/.cursor/mcp.json`, or `.cursor/mcp.json` per project) and Claude Desktop (Settings → Developer → Edit Config) use this JSON:

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

The launcher fetches the latest published release binary, verifies its SHA-256 checksum, caches it and runs it over stdio. Later launches check for updates with a bounded request and fall back to the verified cached binary when offline. Releases ship through GitHub rather than the npm registry, which is why the package is addressed as `github:SokolskyNikita/annas-mcp`. The launcher never runs the repository's current source. To test unreleased changes, [build from source](#build-from-source).

To use the CLI instead, set the same variables and run `npx -y github:SokolskyNikita/annas-mcp --help`. See [CLI](#cli).

### Client configuration examples

Claude Code:

```bash
claude mcp add annas-mcp \
  --env ANNAS_ACCOUNT_COOKIE=aa_account_id2-value \
  --env ANNAS_SECRET_KEY=your-api-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  -- npx -y github:SokolskyNikita/annas-mcp
```

Keep the server name before the `--env` flags. `--env` takes multiple values, so a name placed after it is parsed as another variable. On native Windows (not WSL), wrap the command as `-- cmd /c npx -y github:SokolskyNikita/annas-mcp`.

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

## Membership and credentials

Every archive request needs the account cookie. Downloads also need an API key.

| Operations | Membership | Required variables |
| --- | --- | --- |
| `book_search`, `article_search` (including DOI lookup) and other metadata requests | **Lucky Librarian** or higher, authenticated by the cookie | `ANNAS_ACCOUNT_COOKIE` |
| `book_download` and `article_download` (by hash or DOI) | Any [membership tier](https://annas-archive.gl/donate), for the fast-download JSON API | `ANNAS_ACCOUNT_COOKIE`, `ANNAS_SECRET_KEY` and `ANNAS_DOWNLOAD_PATH` |

Neither credential substitutes for the other. The cookie authenticates archive requests; the key unlocks downloads. Get the key from your Anna's Archive account. The [API FAQ](https://annas-archive.gl/faq#api) has details.

Searches and DOI lookups read the site's HTML pages with the cookie, so they need **Lucky Librarian** or higher even though downloads work with any tier. An API key alone does not make searches work.

> [!IMPORTANT]
> The account cookie expires every week. You must fetch a new one by hand, update `ANNAS_ACCOUNT_COOKIE` and restart the MCP server. This project does not renew cookies.

### Retrieve or renew the cookie

1. Sign in to Anna's Archive in your browser with the account that holds the membership.
2. Open developer tools, go to Application (Chrome, Edge) or Storage (Firefox, Safari) → Cookies and copy the `aa_account_id2` value. You can also copy the `Cookie` header from an authenticated request in the Network tab.
3. Paste it into `ANNAS_ACCOUNT_COOKIE` in your MCP client's environment configuration or in the `.env` file the CLI reads.
4. Restart the MCP server from your client so it picks up the new value.

Repeat weekly, or sooner if the archive rejects the session.

The variable accepts the raw value, `aa_account_id2=...` or a full browser `Cookie` header. Only `aa_account_id2` is sent to the selected mirror. Keep the cookie and API key private and never commit them.

## Configuration

Set variables in your MCP client's `env` block. The CLI and server also load `.env` from the process working directory, with existing environment variables taking precedence. MCP clients may launch the process from any directory, so the client configuration is the reliable place.

| Variable | Required for | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | All archive requests, downloads included | `aa_account_id2` cookie. [Renew it weekly](#retrieve-or-renew-the-cookie). |
| `ANNAS_SECRET_KEY` | Downloads | API key from an account meeting the [download requirement](#membership-and-credentials). |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory for downloaded files. Created if missing. |
| `ANNAS_BASE_URL` | Optional | Hostname or HTTPS base URL of the fallback mirror. Defaults to `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | Optional | Automatic mirror discovery is on by default. Set to `false` to use `ANNAS_BASE_URL` only. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Local, user-writable directory where the npm launcher caches release binaries and metadata. |

### Mirror selection

Discovery runs on the first archive request with a 15-second budget. It probes only the verified official mirrors `annas-archive.gl`, `annas-archive.pk` and `annas-archive.gd`. Other domains listed in the status directory never receive your credentials.

A successful pick is cached for 10 minutes and a fallback for 30 seconds. A mirror failure invalidates the cached choice. When discovery finds nothing usable, requests go to `ANNAS_BASE_URL`, or `annas-archive.gl` if that is unset.

Whatever host you put in `ANNAS_BASE_URL` receives your credentials. Only configure one you trust.

## MCP tools

Successful calls return structured JSON plus text content. Failures follow the [error contract](#errors-and-troubleshooting).

Every tool accepts `timeout_seconds`, which covers the whole operation including retries. Searches default to 60 seconds and downloads to 1,800 (30 minutes). `0` means the default. The maximum is 86,400 (24 hours).

Every title, author, publisher, DOI and hash in the examples below is a **fictional placeholder**. Substitute real queries and identifiers.

### Shared search options

`book_search` and keyword `article_search` accept these optional fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `language` | Any | Two-letter ISO 639-1 code, such as `en`. |
| `page` | `1` | Upstream result page, starting at 1. |
| `limit` | `10` | Maximum records returned from that page. Records past the limit are skipped rather than carried to the next page, so raise `limit` before paging. |
| `content` | No filter | `book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine` or `standards_document`. Legacy values such as `book_any` do not filter anything. |

### `book_search`

Searches books, textbooks, manuals, standards and other book records by title, author or topic.

```json
{
  "query": "The Moon Goblin's Guide to Teapot Astronomy",
  "language": "en",
  "page": 1,
  "limit": 10,
  "timeout_seconds": 60
}
```

A keyword search returns one page:

```json
{
  "page": 1,
  "language": "en",
  "matched": 1,
  "limit": 1,
  "results": [
    {
      "hash": "abc123def4567890abc123def4567890",
      "title": "The Moon Goblin's Guide to Teapot Astronomy",
      "authors": "Professor Puddlewhisk",
      "publisher": "Imaginary Moon Press",
      "language": "English",
      "format": "EPUB",
      "size": "3.1MB",
      "description": "A fictional handbook for charting constellations with enchanted teapots.",
      "url": "https://annas-archive.gl/md5/abc123def4567890abc123def4567890"
    }
  ]
}
```

`matched` counts the records parsed from the upstream page. The response's `limit` counts the records actually returned in `results`, which can be fewer than requested. A missing `description` or `doi` field means the source page did not provide one.

### `article_search`

Searches journal articles by keyword or resolves a single article from a bare DOI or DOI URL. The examples use the fictional paper "Teleporting Teapots with Moonbeam Networks".

Keyword search:

```json
{"query":"moonbeam networks","language":"en","limit":10}
```

Keyword results use the same pagination envelope as `book_search`. They usually include `"index":"journals"` and carry `journal` and `page_url` in place of a book's `publisher` and `url`.

DOI lookup:

```json
{"query":"https://doi.org/10.0000/fictional.moonbeam-teapots"}
```

A DOI lookup returns one article object instead of a page envelope. Its fields can include `doi`, `title`, `authors`, `journal`, `format`, `size`, `hash`, `description` and `page_url`. The signed SciDB PDF link is never returned; `article_download` resolves it again when needed. When `hash` is present, pass it to `article_download`.

DOI suffix punctuation is preserved exactly, including parentheses and trailing periods. When copying a DOI out of prose, strip any punctuation that belongs to the surrounding sentence.

### `book_download`

Downloads a book by the `hash` from `book_search`.

```json
{
  "hash": "abc123def4567890abc123def4567890",
  "title": "The Moon Goblin's Guide to Teapot Astronomy",
  "format": "epub",
  "timeout_seconds": 1800
}
```

### `article_download`

Downloads an article by exactly one of `doi` or `hash` (from `article_search`).

```json
{"doi":"10.0000/fictional.moonbeam-teapots","format":"pdf"}
```

With a known hash, use the direct path and include a title when you have one:

```json
{
  "hash": "abc123def4567890abc123def4567890",
  "title": "Teleporting Teapots with Moonbeam Networks",
  "format": "pdf"
}
```

### Shared download behavior

Both download tools return the absolute local path and byte count:

```json
{"path":"/absolute/path/to/downloads/teapot-astronomy.epub","bytes":123456}
```

`title` sets the filename. `format` sets the extension without converting the file; if omitted, the extension is inferred from the response's filename, its content type, or the download URL, in that order, and falls back to `.bin`. Files are capped at 8 GiB. The downloader verifies the expected MD5 when known and deletes incomplete or invalid files. Existing files are never overwritten: a name collision gets a short hash or numeric suffix.

If the fast API fails for a DOI download, the client falls back to the PDF embedded in that DOI's SciDB article page. It extracts the PDF.js viewer's file URL, checks the PDF signature against the page's MD5 and saves the file with a `.pdf` extension when `format` is omitted. The fallback only applies to a page with a single PDF and a matching archive hash. A generic SciDB search form does not qualify. The account cookie stays on the archive origin, so external PDF hosts never receive it.

## Workflow for AI clients

1. Search with the user's title, citation, DOI or topic. Compare title, authors, language, format and description. Ask the user when several records are plausible.
2. Download the chosen record with its `hash` and `title`. For `article_download`, use the DOI when no hash is available. Never reuse identifiers from the fictional examples.
3. Give the user the `path` from the successful tool result. Report errors as they are. Never claim a download succeeded without a successful result.

An empty search page means no records were parsed from that upstream page. Adjust the query or the [search options](#shared-search-options). For access failures, check [membership and credentials](#membership-and-credentials) and ask the user to renew the cookie when needed. Retrying does not renew an expired cookie.

## Errors and troubleshooting

The CLI exits nonzero with a coded message. MCP service failures return `isError: true` with the code at the start of the text. Schema-invalid arguments may be rejected at the protocol boundary instead.

Failed downloads list every fast server attempted, separate API requests from file transfers and include the SciDB fallback when it ran. HTTP failures show the server hostname, status code and a short explanation. Raw response bodies and signed download URLs are left out. A 404 on a file transfer means the download server did not provide the file, not that the archive's metadata record is missing.

A clean startup, `tools/list` or `--help` does not prove upstream access. Membership and credentials are only exercised by a search or download.

| Code | Meaning and next action |
| --- | --- |
| `[CONFIG]` | A required environment variable is missing or invalid. See [Configuration](#configuration). |
| `[INVALID_ARGUMENT]` | An input is malformed or out of range. Fix the DOI, hash, extension, page, limit or timeout and retry. |
| `[NOT_FOUND]` | A DOI or search did not resolve to a usable record, or the selected copy has no fast download. Try a title search, a larger limit, another page or another copy of the file. |
| `[UPSTREAM_BLOCKED]` | Check the membership and credentials, then [fetch a fresh cookie](#retrieve-or-renew-the-cookie) and restart the server. |
| `[REQUEST_TIMEOUT]` | The request was cancelled or exceeded its timeout. Retry or raise `timeout_seconds`. |
| `[UPSTREAM]` | Read the message, then check the account's download quota, the API key and mirror availability before retrying. |

## CLI

The CLI shares the MCP tools' validation and download code. Search commands print the whole upstream page by default; `--limit` only trims what the CLI prints. `--json` emits machine-readable output for scripts and agents.

The examples assume an `annas-mcp` binary on your `PATH`, such as one installed with `go install` (see [Build from source](#build-from-source)). Otherwise, replace `annas-mcp` with `npx -y github:SokolskyNikita/annas-mcp`.

```bash
# Search
annas-mcp book-search "teapot astronomy" --language en --page 1 --limit 20
annas-mcp article-search "moonbeam networks" --json
annas-mcp article-search "10.0000/fictional.moonbeam-teapots" --json

# Download by a search result hash
annas-mcp book-download abc123def4567890abc123def4567890 "teapot-astronomy.epub"
annas-mcp article-download --hash abc123def4567890abc123def4567890 \
  --title "Teleporting Teapots with Moonbeam Networks" --format pdf

# Download an article by DOI
annas-mcp article-download "10.0000/fictional.moonbeam-teapots" --format pdf

# Start the stdio server explicitly
annas-mcp mcp
```

`--timeout 30s` or `--timeout 10m` overrides an operation's default (positive, up to 24h). Search flags match the [shared search options](#shared-search-options). Run any subcommand with `--help` for its full argument list.

## Build from source

Requires Go 1.26 or newer. The MCP server uses `github.com/modelcontextprotocol/go-sdk` v1.8.0.

```bash
git clone https://github.com/SokolskyNikita/annas-mcp.git
cd annas-mcp
go build -o ./annas-mcp ./cmd/annas-mcp
```

This builds the current checkout. To use it from an MCP client, point the client at the binary's absolute path, pass the `mcp` argument and supply the same [configuration](#configuration):

```toml
[mcp_servers.annas-mcp]
command = "/absolute/path/to/annas-mcp"
args = ["mcp"]
```

`go run ./cmd/annas-mcp mcp` starts the checkout directly and `go install ./cmd/annas-mcp` installs it locally. To install the latest tagged version instead, run `go install github.com/SokolskyNikita/annas-mcp/cmd/annas-mcp@latest`.

## Development

### Project layout

- `cmd/annas-mcp` is the executable entry point.
- `internal/modes` holds the shared MCP and CLI service, validation and output handling.
- `internal/anna` handles archive search, article lookup and downloads.
- `internal/mirror` discovers and probes archive mirrors. The archive client caches the selection.
- `internal/env`, `internal/apperr` and `internal/version` hold configuration, stable application errors and the embedded release version.
- `lib/launcher.js`, `lib/archive.js` and `lib/target.js` split the npm launcher into release fetching, safe archive extraction and platform/checksum selection.
- `scripts` contains launcher tests, health checks and immutable release-tag tooling.

### Checks

Run these before opening a pull request:

```bash
gofmt -w cmd internal
go vet ./...
npm run check:version              # embedded and npm versions must agree
npm test                           # version, launcher and stdio integration checks
npm run coverage                   # Go race tests; enforces 80% total coverage
```

None of them need archive credentials. They run against fixtures and a local mock release server. Go fixtures live in each package's `testdata` directory and launcher fixtures in `scripts/testdata`. Coverage is written to `coverage.out`.

`scripts/healthcheck.sh` runs local Go tests and CLI startup checks from any working directory. Add `--live` (or set `ANNAS_MCP_HEALTHCHECK_LIVE=1`) for real book and article searches, without downloads. `ANNAS_MCP_HEALTHCHECK_TIMEOUT` sets the per-check timeout in seconds.

### Optional live MCP smoke test

To exercise all four MCP tools against the real upstream services, build the checkout and opt in:

```bash
go build -o ./annas-mcp ./cmd/annas-mcp
node scripts/smoke-mcp.mjs ./annas-mcp
```

The smoke test reads the configured environment and the repository's `.env`, so the [download access requirements](#membership-and-credentials) apply. It performs real searches and downloads (covering both DOI and hash article paths) and consumes download quota. It verifies byte counts and checksums. The downloaded files and a `report.json` (server version, timings, paths, sizes and hashes) stay in the temporary directory it reports.

To test SciDB independently of the fast API:

```bash
ANNAS_MCP_TEST_SCIDB_LIVE=1 go test ./internal/anna -run '^TestLiveSciDBFallback$' -count=1 -v
```

This opt-in check uses `ANNAS_ACCOUNT_COOKIE` (or the repository's `.env`) against the official `.gl` mirror. It disables fast API requests locally, downloads a small article through the SciDB viewer link and verifies its PDF signature, MD5 and credential handling. Temporary download files are removed when the test finishes.

### CI and releases

Pull requests and pushes to `main` run Go race tests, the coverage gate, vet, shell checks and Node integration tests on Linux, macOS and Windows. CI also checks formatting, version consistency and workflow syntax (with actionlint) and builds a GoReleaser snapshot of all eight release targets. Live archive tests stay opt-in because they need membership credentials and download quota.

To publish a release:

1. Update `internal/version/version.txt` (`vMAJOR.MINOR.PATCH`) and `package.json` (the same version without the `v`). Record the changes in [CHANGELOG.md](CHANGELOG.md).
2. Run the checks above, `goreleaser check -f .goreleaser` and the live MCP smoke test.
3. Commit and push to `main`, then wait for CI to pass.
4. Run `scripts/manage-tag.sh add`. It requires a clean checkout matching the pushed `origin/main` commit, creates an annotated tag and rejects existing tags.

The tag workflow repeats the platform tests, then publishes binaries and SHA-256 checksums to GitHub Releases. It then installs the published binary through the npm launcher on Linux, macOS and Windows, verifies its version and checks the MCP handshake and all four registered tools. To repeat that verification locally, run `node scripts/verify-release.mjs v0.0.16` with the published tag you want to check. No archive credentials are needed.

Release tags are immutable. Never move or recreate one: the npm launcher caches binaries by release identity and checksum. GitHub Releases is the only distribution channel; nothing is published to the npm registry.

## Project lineage

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive search and downloads as an MCP server. This repository is a complete rewrite and shares no code with it. Contributions that improve search correctness, download integrity, mirror resilience, MCP schemas or cross-platform launcher behavior are welcome.

## License

[MIT](LICENSE)
