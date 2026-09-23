# annas-mcp

`annas-mcp` is a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server and command-line client for searching Anna's Archive for books and journal articles, then downloading a selected file. It exposes the same operations to Cursor, Claude, Codex, and other MCP clients as it does to a shell script.

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)

The archive's contents and access rules vary by work and account. Use the service only for material you are entitled to obtain and follow the applicable law and service terms.

## Membership and credentials

The supported setup for this MCP requires an active Anna's Archive membership and credentials supplied to the process:

| Operations | Minimum membership | Required configuration |
| --- | --- | --- |
| `book_search`, `article_search` (including DOI lookup), and other archive metadata requests | **Brilliant Bookworm** or higher | `ANNAS_ACCOUNT_COOKIE` |
| `book_download` and `article_download` (by hash or DOI) | **Lucky Librarian** or higher | `ANNAS_SECRET_KEY`, plus the account cookie and `ANNAS_DOWNLOAD_PATH` |

**Downloads will not work without at least Lucky Librarian status and a valid API key.** A cookie does not replace the key, and a key does not replace the cookie required for searches and lookups. Obtain the key from your Anna's Archive account; see the [membership options](https://annas-archive.gl/donate) and [API FAQ](https://annas-archive.gl/faq#api) for account details.

**The account cookie expires once a week. You must retrieve it manually every week**, update `ANNAS_ACCOUNT_COOKIE`, and restart the MCP server. This project does not renew cookies automatically.

### Retrieve or renew the cookie

1. Open Anna's Archive in your browser and sign in to the account with the required membership.
2. Open developer tools → Application/Storage → Cookies and copy the current `aa_account_id2` value. Alternatively, copy the `Cookie` header from an authenticated request in the Network tab.
3. Replace `ANNAS_ACCOUNT_COOKIE` in your MCP client's environment configuration or the `.env` file used by the CLI.
4. Restart the MCP server through your client so it reads the new value. Repeat this procedure weekly, or sooner if the session is rejected.

The variable accepts the raw value, `aa_account_id2=...`, or a full browser `Cookie` header. Only `aa_account_id2` is sent to the selected archive mirror. Keep the cookie and API key private; never commit them.

## Quick start

Install Node.js 18 or newer and a `tar` command that supports `.tar.xz` archives (`.zip` on Windows). Add this configuration to your MCP client, replacing the placeholders with the credentials described above. Cursor and Claude Desktop use this JSON structure.

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

The launcher starts the latest published release binary over stdio, verifies its SHA-256 checksum, and caches it locally. Later launches check for updates with a bounded request and can use a verified cached binary when offline. It does not run the repository's current source; use [Build from source](#build-from-source) to test unreleased changes.

For terminal use, configure the same environment variables and run `npx -y github:SokolskyNikita/annas-mcp --help`. See [CLI](#cli) for commands.

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

## Configuration

The CLI and stdio server load `.env` from the process working directory; existing environment variables take precedence. Prefer your MCP client's explicit environment configuration so setup does not depend on where it launches the process.

| Variable | Required for | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | Searches, DOI lookups, and archive access | `aa_account_id2` cookie; follow the [weekly renewal procedure](#retrieve-or-renew-the-cookie). |
| `ANNAS_SECRET_KEY` | Downloads | API key from the account meeting the [download membership requirement](#membership-and-credentials). |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory for downloaded files. It is created when needed. |
| `ANNAS_BASE_URL` | Optional mirror fallback | Anna's Archive hostname or HTTPS base URL used as the fallback mirror. |
| `ANNAS_AUTO_BASE_URL` | Optional mirror discovery | Automatic mirror selection is enabled by default. Set to `false` to use `ANNAS_BASE_URL` only. |
| `ANNAS_MCP_CACHE_DIR` | Optional npm launcher cache | Local, user-writable directory for release binaries and metadata. |

Mirror discovery starts on the first archive request, with a 15-second budget. Successful selections are cached for 10 minutes; fallbacks for 30 seconds. A failed mirror is invalidated. Discovery falls back to `ANNAS_BASE_URL`, or `annas-archive.gl` when unset. Set `ANNAS_AUTO_BASE_URL=false` to use a fixed mirror.

## MCP tools

Successful calls return both structured JSON and text content. Failures follow the [error contract](#errors-and-troubleshooting).

All titles, authors, publishers, DOIs, and hashes in the examples are **fictional placeholders**. Replace them with real queries and identifiers before use.

Every tool accepts `timeout_seconds`: searches default to 60 seconds and downloads to 1,800 seconds. `0` uses the default; the maximum is 86,400 seconds (24 hours). The timeout covers the complete operation, including retries.

### Shared search options

`book_search` and keyword `article_search` accept these optional fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `language` | Any language | Two-letter ISO 639-1 code, such as `en`. |
| `page` | `1` | Upstream result page, starting at 1. |
| `limit` | `10` | Maximum records returned from that page. Increase this before requesting another page. |
| `content` | No content filter | `book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine`, or `standards_document`. |

Use current content filters; legacy values such as `book_any` are not useful filters.

### `book_search`

Search books, textbooks, manuals, standards, or other book records by title, author, or topic.

```json
{
  "query": "The Moon Goblin's Guide to Teapot Astronomy",
  "language": "en",
  "page": 1,
  "limit": 10,
  "timeout_seconds": 60
}
```

Keyword searches return one page:

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

`matched` is the number of parsed records on the current upstream page. `limit` is the number returned after this tool's limit is applied. A missing `description` or `doi` field means the source page did not provide it.

### `article_search`

Search journal articles by keywords, or resolve one article by a bare DOI or DOI URL. The DOI example refers to the fictional paper "Teleporting Teapots with Moonbeam Networks".

For keywords:

```json
{"query":"moonbeam networks","language":"en","limit":10}
```

Keyword results have the same pagination fields as `book_search`, usually include `"index":"journals"`, and use `journal` and `page_url` in place of a book's `publisher` and `url`.

For a DOI:

```json
{"query":"https://doi.org/10.0000/fictional.moonbeam-teapots"}
```

The result is one article object rather than a page envelope. Its fields can include `doi`, `title`, `authors`, `journal`, `format`, `size`, `hash`, `description`, `download_url`, and `page_url`. The `hash` is the value to pass to `article_download` when it is present.

### `book_download`

Download a book by the `hash` returned by `book_search`.

```json
{
  "hash": "abc123def4567890abc123def4567890",
  "title": "The Moon Goblin's Guide to Teapot Astronomy",
  "format": "epub",
  "timeout_seconds": 1800
}
```

### `article_download`

Download an article by exactly one of `doi` or a `hash` from `article_search`.

```json
{"doi":"10.0000/fictional.moonbeam-teapots","format":"pdf"}
```

When a hash is already known, use the direct path and provide a title if one is available:

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

`title` controls the filename. `format` sets its extension without converting the file; when omitted, the extension is inferred from the response. Files are limited to 8 GiB. The downloader verifies the expected MD5 when known, removes incomplete or invalid files, and preserves existing files by adding a short hash or numeric suffix on name collisions.

## Workflow for AI clients

1. Search with the user's title, citation, DOI, or topic. Compare title, authors, language, format, and description; ask the user when several records are plausible.
2. Download the selected record using its `hash` and `title`. Use a DOI for `article_download` when a hash is unavailable. Never invent identifiers from the fictional examples.
3. Return the successful tool result's `path` to the user. Report errors accurately; do not claim a download succeeded without a successful tool result.

An empty search page means no records were parsed from that upstream page. Adjust the query or use the [search options](#shared-search-options). For access failures, check [membership and credentials](#membership-and-credentials) and request manual cookie renewal when needed; repeated retries do not renew an expired cookie.

## Errors and troubleshooting

The CLI exits nonzero with a coded message. MCP service failures return `isError: true` with a code at the start of the text. Schema-invalid arguments may instead be rejected at the protocol boundary.

A successful startup, `tools/list`, or `--help` does not verify upstream access. Membership and credentials are exercised when you search or download.

| Code | Meaning and next action |
| --- | --- |
| `[CONFIG]` | Correct missing or invalid environment variables; see [Configuration](#configuration). |
| `[INVALID_ARGUMENT]` | An input is malformed or out of range. Correct the DOI, hash, extension, page, limit, or timeout and retry. |
| `[NOT_FOUND]` | A DOI or search did not resolve to a usable record. Try a title search, a larger limit, or another page. |
| `[UPSTREAM_BLOCKED]` | Check the required membership and credentials, then [retrieve a fresh cookie](#retrieve-or-renew-the-cookie) and restart the server. |
| `[REQUEST_TIMEOUT]` | The request was cancelled or exceeded its timeout. Retry or increase `timeout_seconds`. |
| `[UPSTREAM]` | Inspect the message, account download quota, API key, and mirror availability before retrying. |

## CLI

The CLI uses the same validation and download code as the MCP tools. Search commands print the whole upstream page by default; `--limit` only limits what the CLI prints. `--json` emits machine-readable JSON for scripts and agents.

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

Use `--timeout 30s` or `--timeout 10m` to override an operation's default (positive, up to 24h). Search flags match the [shared search options](#shared-search-options). Run a subcommand's `--help` for its full argument list.

## Build from source

Building from source requires Go 1.26 or newer. The MCP server uses `github.com/modelcontextprotocol/go-sdk` v1.8.0.

```bash
git clone https://github.com/SokolskyNikita/annas-mcp.git
cd annas-mcp
go build -o ./annas-mcp ./cmd/annas-mcp
```

This builds the current checkout. To use it in an MCP client, select the built binary by absolute path and supply the same [configuration](#configuration):

```toml
[mcp_servers.annas-mcp]
command = "/absolute/path/to/annas-mcp"
args = ["mcp"]
```

For development, `go run ./cmd/annas-mcp mcp` starts the checkout directly, and `go install ./cmd/annas-mcp` installs it locally. To install the latest tagged version instead, use `go install github.com/SokolskyNikita/annas-mcp/cmd/annas-mcp@latest`.

The main packages are arranged as follows:

- `cmd/annas-mcp` is the executable entry point.
- `internal/modes` contains the shared MCP and CLI service, validation, and output handling.
- `internal/anna` handles archive search, article lookup, and downloads.
- `internal/mirror` discovers and probes archive mirrors; the archive client caches the selection.
- `internal/env`, `internal/apperr`, and `internal/version` hold configuration, stable application errors, and the embedded release version.
- `lib/launcher.js`, `lib/archive.js`, and `lib/target.js` split npm release fetching, safe archive extraction, and platform/checksum selection.
- `scripts` contains launcher tests, health checks, and immutable release-tag tooling.

## Development

Run the checks used by the project before opening a pull request:

```bash
gofmt -w cmd internal
go vet ./...
npm test                           # launcher and stdio integration checks
npm run coverage                   # Go race tests; enforces 80% total coverage
```

Go fixtures live in each package's `testdata` directory; launcher fixtures live in `scripts/testdata`. These checks use fixtures and a local mock release server, without archive credentials. Coverage is written to `coverage.out`.

`scripts/healthcheck.sh` runs local Go tests and CLI startup checks from any working directory. Add `--live` (or `ANNAS_MCP_HEALTHCHECK_LIVE=1`) for real book/article searches, without downloads. `ANNAS_MCP_HEALTHCHECK_TIMEOUT` sets the per-check timeout in seconds.

### Optional live MCP smoke test

Build the checkout and opt in when you want to exercise all four MCP tools against the real upstream services:

```bash
go build -o ./annas-mcp ./cmd/annas-mcp
node scripts/smoke-mcp.mjs ./annas-mcp
```

The smoke test uses the configured environment and the repository's `.env`, so the [download access requirements](#membership-and-credentials) apply. It performs real searches and downloads, including DOI and hash article paths, and consumes download quota. It verifies byte counts and checksums, then leaves the downloaded files and `report.json` (server version, timings, paths, sizes, hashes) in the reported temporary directory.

The release workflow validates pull requests and publishes only an immutable `vMAJOR.MINOR.PATCH` tag. Keep the embedded version, npm package version, and release tag synchronized. Before a release, run the full test suite, verify the GoReleaser configuration, update both version files, commit the change, and run `scripts/manage-tag.sh add` to create and push the next tag (for example `v0.0.10`). Do not move or recreate an existing release tag: the npm launcher caches binaries by release identity and checksum.

## Project lineage

This is [SokolskyNikita's fork](https://github.com/SokolskyNikita/annas-mcp) of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp). Contributions that improve search correctness, download integrity, mirror resilience, MCP schemas, and cross-platform launcher behavior are welcome.
