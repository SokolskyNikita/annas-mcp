# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)
[![npm](https://img.shields.io/npm/v/annas-mcp)](https://www.npmjs.com/package/annas-mcp)
[![annas-mcp MCP server](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp/badges/score.svg)](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp)

An MCP server and command-line tool for finding books through [Anna's Archive](https://annas-archive.gl) and papers and dissertations through [OpenAlex](https://openalex.org), then downloading or reading the work you pick. Use `annas-cli` in your terminal or connect `annas-mcp` to Claude, Cursor, Codex and other AI clients.

**Configure an OpenAlex API key, an Anna's Archive membership, or both.** OpenAlex supports research discovery, cached open PDFs, repository PDFs and structured full text. An Anna's Archive [membership](https://annas-archive.gl/donate) is required for archive operations.

| Anna's Archive membership | Search through the MCP | Download by file hash or book-page URL |
| --- | --- | --- |
| No paid membership | ❌ | ❌ |
| **Brilliant Bookworm** (cheapest tier) | ❌ | ✅ |
| **Lucky Librarian or higher** (recommended) | ✅ | ✅ |

For archive access, Lucky Librarian or higher allows both search and download. With the basic tier, supply an archive file hash or book-page URL. OpenAlex access has its own account allowance.

Disclaimer: I'm not affiliated with Anna's Archive. They were forced to introduce strict anti-scraping limits in 2026 due to massive DDoS attacks, with only paying members exempt.

**[MCP setup](#mcp-setup)** · **[CLI setup](#cli-setup)** · **[Go SDK](https://github.com/SokolskyNikita/annas-mcp/blob/main/anna/README.md)**

For Anna's Archive it signs in with your membership's secret key and renews the session by itself, so you never copy cookies from a browser. If you're coming from iosifache/annas-mcp, [switching](#switching-from-iosifacheannas-mcp) is a one-line change.

| Tool | What it does |
| --- | --- |
| `book_search` | Finds books, textbooks, manuals and standards by title, author or topic, filtered by format, language, source or year; can include file activity counts |
| `article_search` | Finds papers and dissertations by keywords, citation, DOI or OpenAlex work ID, with usable next-call arguments |
| `article_details` | Inspects identifiers, abstract, copies, versions, licenses and access links for a selected work |
| `article_read` | Returns bounded passages of OpenAlex structured full text with provenance and continuation |
| `metadata_search` | Searches library catalogs such as Open Library, WorldCat and ISBNdb for editions and ISBNs |
| `book_details` | Shows a result's ISBNs and other identifiers, edition, description and reported file problems |
| `account_status` | Shows Anna’s membership and allowance, or OpenAlex usage and rate limits |
| `book_download` | Downloads a book using a file hash or Anna's Archive book-page URL |
| `article_download` | Downloads a paper or dissertation by DOI, OpenAlex work ID or archive hash, trying eligible access routes |

Use it only for material you are entitled to obtain, under the laws and terms that apply to you.

## Go SDK

For coordinated research search, retrieval and reading, use [`papers`](papers/README.md). Its examples cover provider fallback and no-DOI dissertations.

Go applications can import `github.com/SokolskyNikita/annas-mcp/anna` for the same search, metadata, account and verified-download operations. See the [SDK guide and examples](https://github.com/SokolskyNikita/annas-mcp/blob/main/anna/README.md) for explicit configuration, errors, timeouts and concurrency. The SDK and applications share this module’s release tags. Requires Go 1.26 or newer; install with `go get github.com/SokolskyNikita/annas-mcp/anna@v0.8.0`.

## Setup

You need Node.js 18 or newer. Set `OPENALEX_API_KEY` for research access, `ANNAS_SECRET_KEY` for archive access, or both for the full retrieval flow. A free OpenAlex key can be created through [OpenAlex](https://openalex.org). OpenAlex-hosted file requests consume its account allowance. Archive search needs Lucky Librarian or higher; archive download works with any paid tier.

### Anna’s Archive key

Copy the secret key you log in with from your Anna's Archive [account page](https://annas-archive.gl/account/). It is also your API key ([API FAQ](https://annas-archive.gl/faq#api)).

### MCP setup

Add the server with the provider key you use. The examples below show OpenAlex. To use the archive, add `ANNAS_SECRET_KEY` in the same environment settings. For both providers, pass both keys.

#### Claude Code

```bash
claude mcp add annas-mcp \
  --env OPENALEX_API_KEY=your-openalex-key \
  -- npx -y annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y annas-mcp`.

#### Claude Desktop, one click

Download [annas-mcp.mcpb](https://github.com/SokolskyNikita/annas-mcp/releases/latest/download/annas-mcp.mcpb) and open it, or drag it onto Settings → Extensions. Claude Desktop offers optional OpenAlex and Anna’s Archive keys and a download folder. Configure the provider you use.

#### Claude Desktop or Cursor, by hand

Add this to Claude Desktop's config (Settings → Developer → Edit Config) or to Cursor's `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "annas-mcp"],
      "env": {
        "OPENALEX_API_KEY": "your-openalex-key"
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
OPENALEX_API_KEY = "your-openalex-key"
```

#### Homebrew: lighter on memory

`npx` keeps npm running beside the server for the whole session, about 100 MB per client window. On macOS and Linux, Homebrew installs the binary itself, so each client starts only the server, about 20 MB:

```bash
brew install SokolskyNikita/tap/annas-mcp
```

Then use `annas-mcp` as the command in any of the configurations above, with `mcp` as its only argument (`"args": ["mcp"]` in JSON, `args = ["mcp"]` in TOML). For Claude Code:

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-secret-key \
  -- annas-mcp mcp
```

The same install provides `annas-cli`. Update with `brew upgrade annas-mcp`; unlike `npx`, the server does not check for new releases itself.

#### Gemini CLI

```bash
gemini extensions install https://github.com/SokolskyNikita/annas-mcp
```

Gemini CLI offers optional provider keys, which it keeps in the system keychain, plus the download folder and archive cookie. Configure OpenAlex, Anna’s Archive, or both and skip the settings you do not use. Change them later with `gemini extensions config annas-mcp`.

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
      "args": ["run", "-i", "--rm", "-e", "OPENALEX_API_KEY", "-v", "/absolute/path/to/downloads:/downloads", "annas-mcp"],
      "env": { "OPENALEX_API_KEY": "your-openalex-key" }
    }
  }
}
```

On Linux, add `"--user", "1000:1000"` with your own user and group IDs before `"annas-mcp"`, so the container can write to the folder.

#### Try it

Restart your client and ask *"Download the AlphaFold paper, DOI 10.1038/s41586-021-03819-2"* or *"Find dissertations about graph algorithms."* With Anna’s Archive configured, you can also ask *"Find the EPUB of Pride and Prejudice and download it."* The AI searches, picks a copy and tells you where it saved the file: `annas-archive` in your home folder unless you [chose another](#choosing-the-download-folder).

### CLI setup

Install both commands with one package:

```bash
npm install -g annas-mcp
export OPENALEX_API_KEY="your-openalex-key"

annas-cli --help
annas-cli article-search "graph algorithms" --work-type dissertation
```

Both commands support the same operations. With no arguments, `annas-cli` shows help and `annas-mcp` starts MCP. See [Configuration](#configuration) for settings and [Command line](#command-line) for all commands.

Or run without installing globally:

```bash
npx -y --package=annas-mcp annas-cli article-search "graph algorithms" --work-type dissertation
```

## Switching from iosifache/annas-mcp

This server retains the original four tools, adds research and catalog tools, and reads the same `ANNAS_SECRET_KEY`, `ANNAS_DOWNLOAD_PATH` and `ANNAS_BASE_URL` variables. In your client's config, replace the path to the old binary with `npx`:

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
| `OPENALEX_API_KEY` | OpenAlex research access | OpenAlex key for paper and dissertation discovery, hosted content and usage. |
| `ANNAS_SECRET_KEY` | Archive operations | Your Anna's Archive secret key. The server signs in with it and uses it for downloads. |
| `ANNAS_DOWNLOAD_PATH` | Optional | Folder for downloaded files. Defaults to `annas-archive` in your home folder. See [Choosing the download folder](#choosing-the-download-folder). |
| `ANNAS_ACCOUNT_COOKIE` | Optional | An `aa_account_id2` cookie to use before signing in. See [Signing in](#signing-in). |
| `ANNAS_BASE_URL` | Optional | Mirror to try first; the official mirrors stand in for it while it fails. Defaults to `annas-archive.gl`. Language versions such as `es.annas-archive.gl` are refused, since the server reads the archive in English. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where the server saves provider sessions and research cache metadata, and `npx` caches release binaries. See [The cache folder](HOW-IT-WORKS.md#the-cache-folder). |

The server and CLI also read a `.env` file from their working directory. Variables already set in the environment take precedence. Terminal commands can inherit an exported key from your shell. Desktop MCP clients usually need the key in their own environment configuration. The program reads environment variables; it does not source `.zshrc` or `.zshrc.secrets`. Keep keys out of chat, tool arguments and repository files.

The server only uses `ANNAS_BASE_URL` and the official mirrors `annas-archive.gl`, `.pk` and `.gd`; [Choosing a mirror](HOW-IT-WORKS.md#choosing-a-mirror) explains how it picks one. Whatever host you set in `ANNAS_BASE_URL` receives your credentials, so only use one you trust.

### Choosing the download folder

Downloads go to `~/annas-archive` (`%USERPROFILE%\annas-archive` on Windows), which is created on the first download. Set `ANNAS_DOWNLOAD_PATH` to use another folder. It must be an absolute path, except that a leading `~` stands for your home folder.

On macOS, Desktop, Documents, Downloads and iCloud Drive are protected: the first download there makes macOS ask whether your MCP client may access the folder. If that access is denied, downloads fail with `[CONFIG] cannot write to the download folder`; allow your client in System Settings → Privacy & Security → Files and Folders, or pick a folder directly in your home folder. Avoid `/tmp`, which the operating system clears.

## Tools

The AI fills in each tool's options from your request. [HOW-IT-WORKS.md](HOW-IT-WORKS.md#tools) lists them and what each tool returns, and `--help` shows them on the [command line](#command-line).

For Anna’s Archive downloads, the server checks completed regular files directly in your download folder against the full MD5 hash. If one matches, it returns that file's path and size regardless of its name or extension, without downloading it or making a fresh account request; `downloads_left` is omitted. For article downloads by DOI, the DOI is resolved to a hash first. If no local file matches, the server downloads a verified copy and chooses a unique filename without overwriting an existing file. A fast download may use your membership's allowance, which Anna's Archive counts over a rolling 18- or 24-hour window. Downloading a file again within that window is free, and `account_status` shows how many are left.

### Paper and dissertation workflow

A known DOI or OpenAlex work ID can go directly to `article_download`. The server resolves the file and tries supported routes within one operation. It checks OpenAlex-hosted PDFs, then explicit open PDF locations, then Anna’s DOI route when configured. An explicit archive hash selects that exact file. Use `provider=anna` or `provider=openalex` to constrain a request.

With only a title or citation, search first, compare authors, year and work type, and replay the selected hit’s `download_args`. For a dissertation, use `work_type=dissertation`; an OpenAlex W ID works even if there is no DOI. `article_details` exposes available copies, versions and locations. Supply a location or strict version only when that distinction matters. A hosted PDF may have an unknown version. A landing-page-only result is still a useful link, and does not promise a downloadable file.

`article_read` fetches and returns bounded passages of structured full text when OpenAlex has it. The first read may consume content allowance. Reuse its continuation arguments for the next passage. An abstract is labeled separately from full text. Search and details do not download file bodies.

Downloads return an absolute path on the server host, bytes, provider and reuse evidence. A verified local copy avoids another content request. A client without access to that host can use `article_read` or the returned public work link. Mount the download directory into the host when using Docker.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A required provider variable is missing or invalid, that provider rejected its key, or the download folder can't be written to. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Check that your membership allows searching; when it does not (Brilliant Bookworm), the error says so and `account_status` shows `search_allowed: false`. If the error says no fast downloads are left, wait for the window to roll over; `account_status` shows the allowance. If signing in itself failed, see [Signing in](#signing-in). |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, id, filter, sort, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise [`timeout_seconds`](HOW-IT-WORKS.md#tools). |
| `[UPSTREAM]` | The archive or a download server failed. The error says which one and why; retry later. |

A failed download reports the providers or servers it tried and why each failed. Read provider-specific reasons before retrying. An invalid key or exhausted daily budget needs a configuration change or allowance reset. A temporary rate limit can include a retry delay. When the work exists but no file was obtained, use its returned landing link or another available copy. The server only checks your credentials when you search or download, not when it starts.

If the server doesn't start at all, run `npx -y annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which macOS, most Linux distributions and Windows 10 or later include.

## Command line

Every tool is also a command. Follow [CLI setup](#cli-setup), set the same variables (or put them in a `.env` file), then:

```bash
annas-cli book-search "pride and prejudice" --language en --format epub --include-stats
annas-cli book-search --author "Jane Austen" --year 1813 --sort oldest
annas-cli article-search 10.1038/nature14539
annas-cli article-search "graph algorithms" --work-type dissertation --json
annas-cli article-details W1588575005
annas-cli article-download --openalex-id W1588575005
annas-cli article-read W1588575005
annas-cli account-status --provider openalex
annas-cli metadata-search 9780141439518
annas-cli book-details <hash>
annas-cli account-status
annas-cli book-download <hash> "Pride and Prejudice.epub"
annas-cli article-download 10.1038/nature14539
```

Add `--include-stats` to an archive book or article search to include per-file download, list, quality-vote, report and comment counts when available. Add `--json` for machine-readable output, `--json-errors` for structured failures on stderr, `--timeout 10m` to change the timeout and `--help` to any command for all of its options.

## Privacy

annas-mcp runs on your machine and has no telemetry. Anna’s credentials go to the configured archive hosts. OpenAlex authentication goes to its official API/content hosts; direct repository and publisher PDF requests use no provider credentials. Research queries go to the provider chosen for the operation, and fallback may query the other configured provider. DOI resolution on the archive route may contact doi.org. The npm launcher and bundle contact GitHub to fetch and verify release binaries. The cache stores release binaries, the archive session and mirror health, and research artifact metadata used for verified local reuse. Downloaded files go to your configured output directory. Questions are welcome through [issues](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

For opt-in paper and thesis checks through fresh CLI and MCP processes, run `npm run test:live:papers`. This uses the configured OpenAlex key and a bounded content budget.

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
