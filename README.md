# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)
[![npm](https://img.shields.io/npm/v/annas-mcp)](https://www.npmjs.com/package/annas-mcp)
[![annas-mcp MCP server](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp/badges/score.svg)](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp)

An MCP server that lets Claude, Cursor, Codex and other AI clients search [Anna's Archive](https://annas-archive.gl) for books and papers and download the file you pick. It also works as a command-line tool.

It signs in with your membership's secret key and renews the session by itself, so you never copy cookies from a browser. If you're coming from iosifache/annas-mcp, [switching](#switching-from-iosifacheannas-mcp) is a one-line change.

| Tool | What it does |
| --- | --- |
| `book_search` | Finds books, textbooks, manuals and standards by title, author or topic, filtered by format, language, source or year |
| `article_search` | Finds journal articles by keyword, author or year, or looks one up by DOI |
| `metadata_search` | Searches library catalogs such as Open Library, WorldCat and ISBNdb for editions and ISBNs |
| `book_details` | Shows a result's ISBNs and other identifiers, edition, description and reported file problems |
| `account_status` | Shows your membership and how many fast downloads are left |
| `book_download` | Downloads a book using the hash from a search result |
| `article_download` | Downloads an article by DOI or hash |

Use it only for material you are entitled to obtain, under the laws and terms that apply to you.

## Setup

You need Node.js 18 or newer and an Anna's Archive [membership](https://annas-archive.gl/donate). Any tier can download; searching needs **Lucky Librarian** or higher.

### 1. Get your secret key

Copy the secret key you log in with from your Anna's Archive [account page](https://annas-archive.gl/account/). It is also your API key ([API FAQ](https://annas-archive.gl/faq#api)).

### 2. Add the server to your client

#### Claude Code

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-secret-key \
  -- npx -y annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y annas-mcp`.

#### Claude Desktop, one click

Download [annas-mcp.mcpb](https://github.com/SokolskyNikita/annas-mcp/releases/latest/download/annas-mcp.mcpb) and open it, or drag it onto Settings → Extensions. Claude Desktop asks for your secret key and lets you pick a different download folder.

#### Claude Desktop or Cursor, by hand

Add this to Claude Desktop's config (Settings → Developer → Edit Config) or to Cursor's `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "annas-mcp"],
      "env": {
        "ANNAS_SECRET_KEY": "your-secret-key"
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
```

#### Docker

Build the image straight from this repository:

```bash
docker build -t annas-mcp https://github.com/SokolskyNikita/annas-mcp.git
```

Then have your client run it, mounting your download folder at `/downloads`:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "docker",
      "args": ["run", "-i", "--rm", "-e", "ANNAS_SECRET_KEY", "-v", "/absolute/path/to/downloads:/downloads", "annas-mcp"],
      "env": { "ANNAS_SECRET_KEY": "your-secret-key" }
    }
  }
}
```

On Linux, add `"--user", "1000:1000"` with your own user and group IDs before `"annas-mcp"`, so the container can write to the folder.

### 3. Try it

Restart your client and ask something like *"Find the EPUB of Pride and Prejudice and download it."* The AI searches, picks a copy and tells you where it saved the file: `annas-archive` in your home folder unless you [chose another](#choosing-the-download-folder).

## Switching from iosifache/annas-mcp

This server keeps the same four tools, adds three more and reads the same `ANNAS_SECRET_KEY`, `ANNAS_DOWNLOAD_PATH` and `ANNAS_BASE_URL` variables. In your client's config, replace the path to the old binary with `npx`:

```diff
-      "command": "/path/to/annas-mcp",
-      "args": ["mcp"],
+      "command": "npx",
+      "args": ["-y", "annas-mcp"],
```

What changes:

- Searches run as your signed-in member account, so they aren't turned away with `403` by the archive's browser check. Searching needs Lucky Librarian or higher.
- The server picks a working official mirror by itself. Set `ANNAS_AUTO_BASE_URL=false` to use only `ANNAS_BASE_URL`.
- Downloads are checked against the archive's MD5 hash.
- `npx` picks up new releases without a manual download.

## Signing in

Anna's Archive only lets signed-in members search. The server signs in with your secret key before its first request and again whenever the archive ends the session, about once a week. The [session is saved](HOW-IT-WORKS.md#the-saved-session) between runs.

If signing in fails, the error says what each mirror answered and how to give the server a browser session instead:

1. Sign in to Anna's Archive in your browser, open its developer tools and go to Application (Chrome, Edge) or Storage (Firefox, Safari) → Cookies.
2. Copy the value of `aa_account_id2` into `ANNAS_ACCOUNT_COOKIE` and restart the MCP server from your client.

The server uses that cookie until it expires, then signs in with the secret key again. Keep the secret key and cookie private.

## Configuration

| Variable | Needed for | Description |
| --- | --- | --- |
| `ANNAS_SECRET_KEY` | Everything | Your Anna's Archive secret key. The server signs in with it and uses it for downloads. |
| `ANNAS_DOWNLOAD_PATH` | Optional | Folder for downloaded files. Defaults to `annas-archive` in your home folder. See [Choosing the download folder](#choosing-the-download-folder). |
| `ANNAS_ACCOUNT_COOKIE` | Optional | An `aa_account_id2` cookie to use before signing in. See [Signing in](#signing-in). |
| `ANNAS_BASE_URL` | Optional | Mirror to try first; the official mirrors stand in for it while it fails. Defaults to `annas-archive.gl`. Language versions such as `es.annas-archive.gl` are refused, since the server reads the archive in English. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where the server saves its session and `npx` caches release binaries. See [The cache folder](HOW-IT-WORKS.md#the-cache-folder). |

The server and CLI also read a `.env` file from their working directory. Variables already set in the environment take precedence.

The server only uses `ANNAS_BASE_URL` and the official mirrors `annas-archive.gl`, `.pk` and `.gd`; [Choosing a mirror](HOW-IT-WORKS.md#choosing-a-mirror) explains how it picks one. Whatever host you set in `ANNAS_BASE_URL` receives your credentials, so only use one you trust.

### Choosing the download folder

Downloads go to `~/annas-archive` (`%USERPROFILE%\annas-archive` on Windows), which is created on the first download. Set `ANNAS_DOWNLOAD_PATH` to use another folder. It must be an absolute path, except that a leading `~` stands for your home folder.

On macOS, Desktop, Documents, Downloads and iCloud Drive are protected: the first download there makes macOS ask whether your MCP client may access the folder. If that access is denied, downloads fail with `[CONFIG] cannot write to the download folder`; allow your client in System Settings → Privacy & Security → Files and Folders, or pick a folder directly in your home folder. Avoid `/tmp`, which the operating system clears.

## Tools

The AI fills in each tool's options from your request. [HOW-IT-WORKS.md](HOW-IT-WORKS.md#tools) lists them and what each tool returns, and `--help` shows them on the [command line](#command-line).

Before downloading, the server checks completed regular files directly in your download folder against the full MD5 hash. If one matches, it returns that file's path and size regardless of its name or extension, without downloading it or making a fresh account request; `downloads_left` is omitted. For article downloads by DOI, the DOI is resolved to a hash first. If no local file matches, the server downloads a verified copy and chooses a unique filename without overwriting an existing file. A fast download may use your membership's allowance, which Anna's Archive counts over a rolling 18- or 24-hour window. Downloading a file again within that window is free, and `account_status` shows how many are left.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A variable is missing or invalid, Anna's Archive rejected the secret key or the download folder can't be written to. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Check that your membership allows searching. If the error says no fast downloads are left, wait for the window to roll over; `account_status` shows the allowance. If signing in itself failed, see [Signing in](#signing-in). |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, id, filter, sort, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise [`timeout_seconds`](HOW-IT-WORKS.md#tools). |
| `[UPSTREAM]` | The archive or a download server failed. The error says which one and why; retry later. |

A failed download lists every server it tried and why each one failed. The server only checks your credentials when you search or download, not when it starts.

If the server doesn't start at all, run `npx -y annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which macOS, most Linux distributions and Windows 10 or later include.

## Command line

Every tool is also a command. Set the same variables (or put them in a `.env` file), then:

```bash
npx -y annas-mcp book-search "pride and prejudice" --language en --format epub
npx -y annas-mcp book-search --author "Jane Austen" --year 1813 --sort oldest
npx -y annas-mcp article-search 10.1038/nature14539
npx -y annas-mcp metadata-search 9780141439518
npx -y annas-mcp book-details <hash>
npx -y annas-mcp account-status
npx -y annas-mcp book-download <hash> "Pride and Prejudice.epub"
npx -y annas-mcp article-download 10.1038/nature14539
```

Add `--json` for machine-readable output, `--timeout 10m` to change the timeout and `--help` to any command for all of its options.

## Privacy

annas-mcp runs on your machine and has no telemetry. It sends your secret key (and cookie, if you set one) only to Anna's Archive, and downloads files from the servers Anna's Archive points it to. DOI lookups may contact doi.org. The `npx` launcher and the MCP Bundle contact GitHub to fetch and verify release binaries. Apart from downloaded files, it stores only the release binary, the [saved session](HOW-IT-WORKS.md#the-saved-session) and recent mirror health, all in the [cache folder](HOW-IT-WORKS.md#the-cache-folder). Questions: [open an issue](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
