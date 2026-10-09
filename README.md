# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)
[![npm](https://img.shields.io/npm/v/annas-mcp)](https://www.npmjs.com/package/annas-mcp)

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

### 1. Get your secret key

Copy the secret key you log in with from your Anna's Archive [account page](https://annas-archive.gl/account/). It is also your API key ([API FAQ](https://annas-archive.gl/faq#api)). The server signs in with it by itself, so you don't need to copy cookies from your browser.

### 2. Add the server to your client

Pick a permanent `ANNAS_DOWNLOAD_PATH`. Folders under `/tmp` are cleared by the operating system.

#### Claude Code

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-secret-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  -- npx -y annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y annas-mcp`.

#### Claude Desktop, one click

Download [annas-mcp.mcpb](https://github.com/SokolskyNikita/annas-mcp/releases/latest/download/annas-mcp.mcpb) and open it, or drag it onto Settings → Extensions. Claude Desktop asks for your secret key and download folder.

#### Claude Desktop or Cursor, by hand

Add this to Claude Desktop's config (Settings → Developer → Edit Config) or to Cursor's `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "annas-mcp"],
      "env": {
        "ANNAS_SECRET_KEY": "your-secret-key",
        "ANNAS_DOWNLOAD_PATH": "/absolute/path/to/downloads"
      }
    }
  }
}
```

#### Codex

Add this to `~/.codex/config.toml`:

```toml
[mcp_servers.annas-mcp]
command = "npx"
args = ["-y", "annas-mcp"]

[mcp_servers.annas-mcp.env]
ANNAS_SECRET_KEY = "your-secret-key"
ANNAS_DOWNLOAD_PATH = "/absolute/path/to/downloads"
```

### 3. Try it

Restart your client and ask something like *"Find the EPUB of Pride and Prejudice and download it."* The AI searches, picks a copy and returns the path of the saved file.

On first run `npx` downloads the latest release and caches it once its checksum matches. Later runs start from the cache and pick up new releases automatically.

## Signing in

Anna's Archive only lets signed-in members search. The server signs in with your secret key before its first request, and again whenever the archive ends the session, which happens about once a week. Nothing needs renewing by hand.

The session is saved as `session.json` in the server's cache folder, so restarts and CLI commands don't sign in again. It's the folder `npx` caches the server in: `ANNAS_MCP_CACHE_DIR` if you set it, otherwise `~/.cache/annas-mcp` on macOS and Linux (`$XDG_CACHE_HOME/annas-mcp` if that is set) and `%LOCALAPPDATA%\annas-mcp` on Windows. Only you can read the file. It holds the session cookie plus SHA-256 fingerprints of your secret key and of any `ANNAS_ACCOUNT_COOKIE` the session replaced. Deleting it is safe; the server just signs in again.

If signing in fails, the error says what each mirror answered and how to give the server a browser session instead:

1. Sign in to Anna's Archive in your browser, open its developer tools and go to Application (Chrome, Edge) or Storage (Firefox, Safari) → Cookies.
2. Copy the value of `aa_account_id2` into `ANNAS_ACCOUNT_COOKIE` and restart the MCP server from your client.

The server uses that cookie first and still signs in with the secret key once it expires. From then on it remembers that this cookie expired and skips it until you set a different one. `ANNAS_ACCOUNT_COOKIE` accepts the bare value, `aa_account_id2=…` or a whole `Cookie` header copied from the Network tab. Only `aa_account_id2` is ever sent. Keep the secret key and cookie private.

## Configuration

| Variable | Needed for | Description |
| --- | --- | --- |
| `ANNAS_SECRET_KEY` | Everything | Your Anna's Archive secret key. The server signs in with it and uses it for downloads. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute folder for downloaded files. Created if missing. |
| `ANNAS_ACCOUNT_COOKIE` | Optional | An `aa_account_id2` cookie to use before signing in. See [Signing in](#signing-in). |
| `ANNAS_BASE_URL` | Optional | Mirror to fall back on when automatic selection fails. Defaults to `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where the server saves its session and `npx` caches release binaries. |

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

`title` becomes the filename. Pass `format` (such as `epub` or `pdf`) to set the extension; this doesn't convert the file, and without it the extension is detected from the download. An existing file is never overwritten: a name clash gets a short suffix.

Files are checked against the archive's MD5 hash, and incomplete or corrupt downloads are deleted. When the fast download servers don't have a paper, `article_download` by DOI falls back to the PDF on Anna's SciDB page.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A variable is missing or invalid, or Anna's Archive rejected the secret key. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Check that your membership allows searching. If signing in itself failed, see [Signing in](#signing-in). |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, format, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise `timeout_seconds`. |
| `[UPSTREAM]` | The archive or a download server failed. Check your daily download quota and secret key, then retry. |

A failed download lists every server it tried and why each one failed. The server only checks your credentials when you search or download, not when it starts.

If the server doesn't start at all, run `npx -y annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which comes preinstalled on macOS, mainstream Linux distributions and Windows 10 or later.

## Command line

Every tool is also a command. Set the same variables (or put them in a `.env` file), then:

```bash
npx -y annas-mcp book-search "pride and prejudice" --language en
npx -y annas-mcp article-search 10.1038/nature14539
npx -y annas-mcp book-download <hash> "Pride and Prejudice.epub"
npx -y annas-mcp article-download 10.1038/nature14539
```

Add `--json` for machine-readable output, `--timeout 10m` to change the timeout and `--help` to any command for all of its options.

## Privacy

annas-mcp runs on your machine and has no telemetry. It sends your secret key (and cookie, if you set one) only to Anna's Archive, and downloads files from the servers Anna's Archive points it to. DOI lookups may contact doi.org. The `npx` launcher and the MCP Bundle contact GitHub to fetch and verify release binaries. Downloaded files stay in `ANNAS_DOWNLOAD_PATH`; nothing else is stored apart from the cached binary and the [saved session](#signing-in). Questions: [open an issue](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
