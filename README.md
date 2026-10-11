# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)
[![npm](https://img.shields.io/npm/v/annas-mcp)](https://www.npmjs.com/package/annas-mcp)
[![annas-mcp MCP server](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp/badges/score.svg)](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp)

An MCP server that lets Claude, Cursor, Codex and other AI clients search [Anna's Archive](https://annas-archive.gl) for books and papers and download the file you pick. It also works as a command-line tool.

It signs in with your membership's secret key and renews the session by itself, so you never copy cookies from a browser. Coming from iosifache/annas-mcp? See [Switching](#switching-from-iosifacheannas-mcp); it's a one-line change.

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

You need Node.js 18 or newer and an Anna's Archive [membership](https://annas-archive.gl/donate):

| To… | Membership |
| --- | --- |
| Search | **Lucky Librarian** or higher |
| Download | Any tier |

### 1. Get your secret key

Copy the secret key you log in with from your Anna's Archive [account page](https://annas-archive.gl/account/). It is also your API key ([API FAQ](https://annas-archive.gl/faq#api)). The server signs in with it by itself, so you don't need to copy cookies from your browser.

### 2. Add the server to your client

Files are saved to an `annas-archive` folder in your home folder. To save them somewhere else, add `ANNAS_DOWNLOAD_PATH` (see [Choosing the download folder](#choosing-the-download-folder)).

#### Claude Code

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-secret-key \
  -- npx -y annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y annas-mcp`.

#### Claude Desktop, one click

Download [annas-mcp.mcpb](https://github.com/SokolskyNikita/annas-mcp/releases/latest/download/annas-mcp.mcpb) and open it, or drag it onto Settings → Extensions. Claude Desktop asks for your secret key, and lets you pick a different download folder.

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

On Linux, add `"--user", "1000:1000"` with your own user and group IDs before `"annas-mcp"`, so the container can write to the folder. Each container signs in again when it starts, since `--rm` discards the saved session.

### 3. Try it

Restart your client and ask something like *"Find the EPUB of Pride and Prejudice and download it."* The AI searches, picks a copy and returns the path of the saved file.

On first run `npx` downloads the latest release and caches it once its checksum matches. Later runs start from the cache and pick up new releases automatically.

## Switching from iosifache/annas-mcp

This server keeps the same four tools, adds three more, and reads the same `ANNAS_SECRET_KEY`, `ANNAS_DOWNLOAD_PATH` and `ANNAS_BASE_URL` variables. In your client's config, replace the path to the old binary with `npx`:

```diff
-      "command": "/path/to/annas-mcp",
-      "args": ["mcp"],
+      "command": "npx",
+      "args": ["-y", "annas-mcp"],
```

What changes:

- Searches run as your signed-in member account, so they aren't turned away with `403` by the archive's browser check. Searching needs Lucky Librarian or higher.
- The server picks a working official mirror by itself. Set `ANNAS_AUTO_BASE_URL=false` to use only `ANNAS_BASE_URL`.
- Downloads are checked against the archive's MD5 hash, and `npx` picks up new releases without a manual download.

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
| `ANNAS_DOWNLOAD_PATH` | Optional | Folder for downloaded files. Defaults to `annas-archive` in your home folder. See [Choosing the download folder](#choosing-the-download-folder). |
| `ANNAS_ACCOUNT_COOKIE` | Optional | An `aa_account_id2` cookie to use before signing in. See [Signing in](#signing-in). |
| `ANNAS_BASE_URL` | Optional | Mirror to fall back on when automatic selection fails. Defaults to `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where the server saves its session and `npx` caches release binaries. |

The server picks a working mirror by itself, and only among the official domains `annas-archive.gl`, `.pk` and `.gd`. Whatever host you set in `ANNAS_BASE_URL` receives your credentials, so only use one you trust.

### Choosing the download folder

Downloads go to `~/annas-archive` (`%USERPROFILE%\annas-archive` on Windows), which is created on the first download. Set `ANNAS_DOWNLOAD_PATH` to use another folder. It must be an absolute path, except that a leading `~` stands for your home folder.

On macOS, Desktop, Documents, Downloads and iCloud Drive are protected: the first download there makes macOS ask whether your MCP client may access the folder. If that access is denied, downloads fail with `[CONFIG] cannot write to the download folder`; allow your client in System Settings → Privacy & Security → Files and Folders, or pick a folder directly in your home folder. Avoid `/tmp`, which the operating system clears.

The server and CLI also read a `.env` file from their working directory. Variables already set in the environment take precedence.

## Tools

Every tool accepts `timeout_seconds`, covering the whole operation including retries. Downloads default to 30 minutes and the other tools to 60 seconds.

### Searching

`book_search` and `article_search` return one page of results for a `query`. Each result includes a `hash`, title, authors, year, format, size and source libraries. Books also include the language, publisher and content type, and articles the journal. `fast_download` says whether Anna's fast download servers have the file, which the download tools need, and `possible_issues` marks files the archive warns might be broken.

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
| `limit` | 10 | Results to return from the page. Raise this before asking for another page. |
| `page` | 1 | Result page. Anna's Archive pages hold 50 results. |

`format`, `language`, `content` and `source` take comma-separated lists, and a leading `-` excludes a value: `"format": "epub,mobi"`, `"source": "-scihub"`.

Every result in `results` matches all filters. `total` counts all matches (it is a lower bound when `total_is_lower_bound` is set), and `more_pages` says whether another page exists. When a first page has fewer results than `limit`, Anna's Archive suggests looser matches; these come back separately as `partial_matches` and may ignore the filters.

Give `article_search` a DOI (`10.1038/nature14539`, `doi:…` or `https://doi.org/…`) to get that one article instead of a page of results.

### Catalogs and details

`metadata_search` searches catalog records from Open Library, WorldCat, ISBNdb, Goodreads, Google Books and other sources, including books that have no downloadable file. It takes the same `query`, `title`, `author`, `publisher`, `year`, `language`, `sort`, `limit` and `page` fields, and its `source` filter takes catalogs such as `ol`, `oclc`, `isbndb`, `goodreads` or `gbooks`. Each result has an `id` such as `isbndb:9780521879286`. Searching `book_search` for an ISBN it finds is a good way to find files of that edition.

`book_details` takes a `hash` from either search tool or an `id` from `metadata_search` as its `id`, and returns the full record: ISBNs, DOI, OCLC, Open Library and other identifiers, the edition, the full description, subjects and Dewey/Library of Congress classes, alternative titles and authors, problems source libraries reported for the file (with a better copy's hash when the archive knows one), and download and quality counts.

### Downloading

`book_download` takes the `hash` and a `title`. `article_download` takes either a `doi` or a `hash`, plus an optional `title`. Both return the saved file's `path`, its size in `bytes`, and `downloads_left`: the account's remaining fast downloads.

`title` becomes the filename. Pass `format` (such as `epub` or `pdf`) to set the extension; this doesn't convert the file, and without it the extension is detected from the download. An existing file is never overwritten: a name clash gets a short suffix.

Files are checked against the archive's MD5 hash, and incomplete or corrupt downloads are deleted. A download tries three fast servers, and when the file is stored in more than one collection and every server fails, up to two more collections. When the fast download servers don't have a paper, or the account has no fast downloads left, `article_download` by DOI falls back to the PDF on Anna's SciDB page.

### Account status

Each new file uses one fast download from your membership's allowance, which Anna's Archive counts over a rolling 18- or 24-hour window. Downloading a file again within that window is free. `account_status` returns `member`, `downloads_left`, `downloads_limit`, `window_hours` and your active `memberships`.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A variable is missing or invalid, Anna's Archive rejected the secret key, or the download folder can't be written to. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Check that your membership allows searching. If the error says no fast downloads are left, wait for the window to roll over; `account_status` shows the allowance. If signing in itself failed, see [Signing in](#signing-in). |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, id, filter, sort, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise `timeout_seconds`. |
| `[UPSTREAM]` | The archive or a download server failed. Check your daily download quota and secret key, then retry. |

A failed download lists every server it tried and why each one failed. The server only checks your credentials when you search or download, not when it starts.

If the server doesn't start at all, run `npx -y annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which comes preinstalled on macOS, mainstream Linux distributions and Windows 10 or later.

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

annas-mcp runs on your machine and has no telemetry. It sends your secret key (and cookie, if you set one) only to Anna's Archive, and downloads files from the servers Anna's Archive points it to. DOI lookups may contact doi.org. The `npx` launcher and the MCP Bundle contact GitHub to fetch and verify release binaries. Downloaded files stay in the download folder; nothing else is stored apart from the cached binary and the [saved session](#signing-in). Questions: [open an issue](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
