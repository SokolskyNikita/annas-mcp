# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)

An MCP server that lets Claude, Cursor, Codex and other AI clients search [Anna's Archive](https://annas-archive.gl) for books and papers and download the file you pick. It also works as a command-line tool.

| Tool | What it does |
| --- | --- |
| `book_search` | Finds books, textbooks, manuals and standards by title, author or topic |
| `article_search` | Finds journal articles by keyword, or looks one up by DOI |
| `book_download` | Downloads a book using the hash from a search result |
| `article_download` | Downloads an article by DOI or hash |

Use it only for material you are entitled to obtain, under the laws and terms that apply to you.

## Setup

You need Node.js 18 or newer and an Anna's Archive [membership](https://annas-archive.gl/donate):

| To… | Membership |
| --- | --- |
| Search | **Lucky Librarian** or higher |
| Download | Any tier |

### 1. Get your credentials

- **Account cookie.** Sign in to Anna's Archive, open your browser's developer tools and go to Application (Chrome, Edge) or Storage (Firefox, Safari) → Cookies. Copy the value of `aa_account_id2`. It expires weekly; see [Renewing the cookie](#renewing-the-cookie).
- **API key.** Copy it from your Anna's Archive account page. See the [API FAQ](https://annas-archive.gl/faq#api). Only needed for downloads.

### 2. Add the server to your client

**Claude Code**

```bash
claude mcp add annas-mcp \
  --env ANNAS_ACCOUNT_COOKIE=your-cookie \
  --env ANNAS_SECRET_KEY=your-api-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  -- npx -y github:SokolskyNikita/annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y github:SokolskyNikita/annas-mcp`.

**Claude Desktop and Cursor.** Add this to Claude Desktop's config (Settings → Developer → Edit Config) or to Cursor's `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "github:SokolskyNikita/annas-mcp"],
      "env": {
        "ANNAS_ACCOUNT_COOKIE": "your-cookie",
        "ANNAS_SECRET_KEY": "your-api-key",
        "ANNAS_DOWNLOAD_PATH": "/absolute/path/to/downloads"
      }
    }
  }
}
```

**Codex.** Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.annas-mcp]
command = "npx"
args = ["-y", "github:SokolskyNikita/annas-mcp"]

[mcp_servers.annas-mcp.env]
ANNAS_ACCOUNT_COOKIE = "your-cookie"
ANNAS_SECRET_KEY = "your-api-key"
ANNAS_DOWNLOAD_PATH = "/absolute/path/to/downloads"
```

Pick a permanent `ANNAS_DOWNLOAD_PATH`. Folders under `/tmp` are cleared by the operating system.

### 3. Try it

Restart your client and ask something like *"Find the EPUB of Pride and Prejudice and download it."* The AI searches, picks a copy and returns the path of the saved file.

`npx` downloads the latest release on first run, verifies its checksum and caches it. Later runs start from the cache and pick up new releases automatically.

## Renewing the cookie

The `aa_account_id2` cookie expires every week. When searches start failing with `[UPSTREAM_BLOCKED]`:

1. Copy a fresh `aa_account_id2` value from your browser, as in [step 1](#1-get-your-credentials).
2. Replace `ANNAS_ACCOUNT_COOKIE` in your client's config.
3. Restart the MCP server from your client.

`ANNAS_ACCOUNT_COOKIE` accepts the bare value, `aa_account_id2=…` or a whole `Cookie` header copied from the Network tab. Only `aa_account_id2` is ever sent. Keep the cookie and API key private.

## Configuration

| Variable | Needed for | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | Everything | Your `aa_account_id2` cookie. |
| `ANNAS_SECRET_KEY` | Downloads | Your Anna's Archive API key. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute folder for downloaded files. Created if missing. |
| `ANNAS_BASE_URL` | Optional | Mirror to fall back on when automatic selection fails. Defaults to `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where `npx` caches release binaries. |

The server picks a working mirror by itself, and only among the official domains `annas-archive.gl`, `.pk` and `.gd`. Whatever host you set in `ANNAS_BASE_URL` receives your credentials, so only use one you trust.

The server and CLI also read a `.env` file from their working directory. Variables already set in the environment take precedence.

## Tools

Every tool accepts `timeout_seconds`, covering the whole operation including retries. Searches default to 60 seconds and downloads to 30 minutes.

### Searching

`book_search` and `article_search` take a `query` and return one page of results. Each result includes a `hash`, title, authors, format, size and language, and books also include a publisher. Optional fields:

| Field | Default | Meaning |
| --- | --- | --- |
| `limit` | 10 | Results to return from the page. Raise this before asking for another page. |
| `page` | 1 | Result page. |
| `language` | Any | Two-letter code such as `en`. |
| `content` | Any | `book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine` or `standards_document`. |

Give `article_search` a DOI (`10.1038/nature14539`, `doi:…` or `https://doi.org/…`) to get that one article instead of a page of results.

### Downloading

`book_download` takes the `hash` and a `title`. `article_download` takes either a `doi` or a `hash`, plus an optional `title`. Both return the saved file's `path` and size in `bytes`.

- **Filename.** `title` becomes the filename. Pass `format` (such as `epub` or `pdf`) to set the extension; this doesn't convert the file. Without it, the extension is detected from the download.
- **No overwrites.** An existing file is never overwritten: a name clash gets a short suffix.
- **Integrity.** Files are checked against the archive's MD5 hash, and incomplete or corrupt downloads are deleted.
- **DOI fallback.** When the fast download servers don't have a paper, `article_download` by DOI falls back to the PDF on Anna's SciDB page.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A variable is missing or invalid. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Usually the cookie has expired: [renew it](#renewing-the-cookie). Also check your membership tier. |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, format, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise `timeout_seconds`. |
| `[UPSTREAM]` | The archive or a download server failed. Check your daily download quota and API key, then retry. |

A failed download lists every server it tried and why each one failed. A server starting up cleanly doesn't prove your credentials work; only a search or download tests them.

If the server doesn't start at all, run `npx -y github:SokolskyNikita/annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which comes preinstalled on macOS, Windows 10 and later, and mainstream Linux distributions.

## Command line

Every tool is also a command. Set the same variables (or put them in a `.env` file), then:

```bash
npx -y github:SokolskyNikita/annas-mcp book-search "pride and prejudice" --language en
npx -y github:SokolskyNikita/annas-mcp article-search 10.1038/nature14539
npx -y github:SokolskyNikita/annas-mcp book-download <hash> "Pride and Prejudice.epub"
npx -y github:SokolskyNikita/annas-mcp article-download 10.1038/nature14539
```

Add `--json` for machine-readable output, `--timeout 10m` to change the timeout, and `--help` to any command for all of its options.

## Privacy

annas-mcp runs on your machine and has no telemetry. It sends your cookie and API key only to Anna's Archive, and downloads files from the servers Anna's Archive points it to. DOI lookups may contact doi.org. The `npx` launcher and the MCP Bundle contact GitHub to fetch and verify release binaries. Downloaded files stay in `ANNAS_DOWNLOAD_PATH`; nothing else is stored apart from the cached binary. Questions: [open an issue](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
