# annas-mcp

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)
[![npm](https://img.shields.io/npm/v/annas-mcp)](https://www.npmjs.com/package/annas-mcp)
[![annas-mcp MCP server](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp/badges/score.svg)](https://glama.ai/mcp/servers/SokolskyNikita/annas-mcp)

An MCP server and command-line tool for searching [Anna's Archive](https://annas-archive.gl) and downloading books, papers, textbooks, manuals and standards. Use `annas-cli` in your terminal or connect `annas-mcp` to Claude, Cursor, Codex and other AI clients.

An Anna's Archive [membership](https://annas-archive.gl/donate) is required for archive access:

| Anna's Archive membership | Search through the MCP | Download by file hash or book-page URL |
| --- | --- | --- |
| No paid membership | ❌ | ❌ |
| **Brilliant Bookworm** (cheapest tier) | ❌ | ✅ |
| **Lucky Librarian or higher** (recommended) | ✅ | ✅ |

Lucky Librarian or higher allows both search and download. With the basic tier, supply an archive file hash or book-page URL.

Disclaimer: I'm not affiliated with Anna's Archive. They were forced to introduce strict anti-scraping limits in 2026 due to massive DDoS attacks, with only paying members exempt.

**[MCP setup](#mcp-setup)** · **[CLI setup](#cli-setup)** · **[Go SDK](https://github.com/SokolskyNikita/annas-mcp/blob/main/anna/README.md)** · **[Optional OpenAlex support](#openalex-optional)**

The server signs in with your membership's secret key and renews the session by itself, so you never copy cookies from a browser. If you're coming from iosifache/annas-mcp, [switching](#switching-from-iosifacheannas-mcp) is a one-line change.

| Tool | What it does |
| --- | --- |
| `book_search` | Finds books, textbooks, manuals and standards by title, author or topic, filtered by format, language, source or year; can include file activity counts |
| `article_search` | Finds papers by title, author, topic, citation or DOI |
| `article_details` | Inspects a selected paper's metadata and available copy details |
| `metadata_search` | Searches library catalogs such as Open Library, WorldCat and ISBNdb for editions and ISBNs |
| `book_details` | Shows a result's ISBNs and other identifiers, edition, description and reported file problems |
| `account_status` | Shows your membership, search access and remaining fast downloads |
| `book_download` | Downloads a book using a file hash or Anna's Archive book-page URL |
| `article_download` | Downloads a paper by DOI or archive file hash |
| `article_read` | Reads passages of structured full text with the [optional OpenAlex integration](#openalex-optional) |

Use it only for material you are entitled to obtain, under the laws and terms that apply to you.

## Go SDK

Go applications can import `github.com/SokolskyNikita/annas-mcp/anna` for the same search, metadata, account and verified-download operations. See the [SDK guide and examples](https://github.com/SokolskyNikita/annas-mcp/blob/main/anna/README.md) for explicit configuration, errors, timeouts and concurrency. The SDK and applications share this module’s release tags. Requires Go 1.26 or newer; install with `go get github.com/SokolskyNikita/annas-mcp/anna@v0.8.0`.

## Setup

You need Node.js 18 or newer and your Anna's Archive secret key. Search needs Lucky Librarian or higher; download works with any paid tier.

### Anna’s Archive key

Copy the secret key you log in with from your Anna's Archive [account page](https://annas-archive.gl/account/). It is also your API key ([API FAQ](https://annas-archive.gl/faq#api)).

### MCP setup

Add the server to your client with `ANNAS_SECRET_KEY` set to your membership's secret key.

#### Claude Code

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-secret-key \
  -- npx -y annas-mcp
```

On native Windows (not WSL), end the command with `-- cmd /c npx -y annas-mcp`.

#### Claude Desktop, one click

Download [annas-mcp.mcpb](https://github.com/SokolskyNikita/annas-mcp/releases/latest/download/annas-mcp.mcpb) and open it, or drag it onto Settings → Extensions. Enter your Anna's Archive key and choose a download folder. You can skip the other optional settings.

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

Enter your Anna's Archive key, which Gemini CLI keeps in the system keychain, and choose a download folder. Skip the other optional settings unless you need them. Change them later with `gemini extensions config annas-mcp`.

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

#### Try it

Restart your client and ask *"Find the EPUB of Pride and Prejudice and download it"* or *"Download the paper with DOI 10.1038/nature14539."* The AI locates the copy and tells you where it saved the file: `annas-archive` in your home folder unless you [chose another](#choosing-the-download-folder).

### CLI setup

Install both commands with one package:

```bash
npm install -g annas-mcp
export ANNAS_SECRET_KEY="your-secret-key"

annas-cli --help
annas-cli book-search "pride and prejudice" --language en --format epub
```

Both commands support the same operations. With no arguments, `annas-cli` shows help and `annas-mcp` starts MCP. See [Configuration](#configuration) for settings and [Command line](#command-line) for all commands.

Or run without installing globally:

```bash
npx -y --package=annas-mcp annas-cli book-search "pride and prejudice" --format epub
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
| `ANNAS_SECRET_KEY` | Archive operations | Your Anna's Archive secret key. The server signs in with it and uses it for downloads. |
| `ANNAS_DOWNLOAD_PATH` | Optional | Folder for downloaded files. Defaults to `annas-archive` in your home folder. See [Choosing the download folder](#choosing-the-download-folder). |
| `ANNAS_ACCOUNT_COOKIE` | Optional | An `aa_account_id2` cookie to use before signing in. See [Signing in](#signing-in). |
| `ANNAS_BASE_URL` | Optional | Mirror to try first; the official mirrors stand in for it while it fails. Defaults to `annas-archive.gl`. Language versions such as `es.annas-archive.gl` are refused, since the server reads the archive in English. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `false` to always use `ANNAS_BASE_URL` instead of picking a mirror automatically. |
| `ANNAS_MCP_CACHE_DIR` | Optional | Where the server saves provider sessions and research cache metadata, and `npx` caches release binaries. See [The cache folder](HOW-IT-WORKS.md#the-cache-folder). |
| `OPENALEX_API_KEY` | Optional | Enables [OpenAlex support](#openalex-optional) for additional paper and dissertation access. |

The server and CLI also read a `.env` file from their working directory. Variables already set in the environment take precedence. Terminal commands can inherit an exported key from your shell. Desktop MCP clients usually need the key in their own environment configuration. The program reads environment variables; it does not source `.zshrc` or `.zshrc.secrets`. Keep keys out of chat, tool arguments and repository files.

The server only uses `ANNAS_BASE_URL` and the official mirrors `annas-archive.gl`, `.pk` and `.gd`; [Choosing a mirror](HOW-IT-WORKS.md#choosing-a-mirror) explains how it picks one. Whatever host you set in `ANNAS_BASE_URL` receives your credentials, so only use one you trust.

### Choosing the download folder

Downloads go to `~/annas-archive` (`%USERPROFILE%\annas-archive` on Windows), which is created on the first download. Set `ANNAS_DOWNLOAD_PATH` to use another folder. It must be an absolute path, except that a leading `~` stands for your home folder.

On macOS, Desktop, Documents, Downloads and iCloud Drive are protected: the first download there makes macOS ask whether your MCP client may access the folder. If that access is denied, downloads fail with `[CONFIG] cannot write to the download folder`; allow your client in System Settings → Privacy & Security → Files and Folders, or pick a folder directly in your home folder. Avoid `/tmp`, which the operating system clears.

## Tools

The AI fills in each tool's options from your request. [HOW-IT-WORKS.md](HOW-IT-WORKS.md#tools) lists them and what each tool returns, and `--help` shows them on the [command line](#command-line).

For Anna’s Archive downloads, the server checks completed regular files directly in your download folder against the full MD5 hash. If one matches, it returns that file's path and size regardless of its name or extension, without downloading it or making a fresh account request; `downloads_left` is omitted. For article downloads by DOI, the DOI is resolved to a hash first. If no local file matches, the server downloads a verified copy and chooses a unique filename without overwriting an existing file. A fast download may use your membership's allowance, which Anna's Archive counts over a rolling 18- or 24-hour window. Downloading a file again within that window is free, and `account_status` shows how many are left.

If you know a paper's DOI, call `article_download` directly; the server resolves the archive file itself. Search first when you only have a title, author or citation. An archive hash selects that exact file.

## OpenAlex (optional)

[OpenAlex](https://openalex.org) is an optional enhancement for broader paper discovery, open PDF access and dissertations, including theses without a DOI. It also enables `article_read`, which returns passages of structured full text for an agent to read or summarize. The Anna's Archive setup above works on its own.

### Enable it

Create a free API key through OpenAlex and add `OPENALEX_API_KEY` alongside `ANNAS_SECRET_KEY` in your MCP client's environment settings, then restart the server. For example, use these values inside the `env` block of a Claude Desktop or Cursor configuration:

```json
{
  "ANNAS_SECRET_KEY": "your-secret-key",
  "OPENALEX_API_KEY": "your-openalex-key"
}
```

For the CLI, export `OPENALEX_API_KEY="your-openalex-key"`. In the Claude Desktop extension or Gemini CLI, fill in the optional OpenAlex key setting. For Docker, also pass `"-e", "OPENALEX_API_KEY"` before the image name in the container arguments.

You can also use OpenAlex alone by omitting `ANNAS_SECRET_KEY`. Archive operations still require membership. OpenAlex has its own account allowance; hosted PDF and structured-text requests consume it. Check usage with `account_status` and `provider="openalex"`.

### Search, download and read

With the key configured, ordinary article searches use OpenAlex by default. DOI downloads try a hosted PDF, then an eligible open repository or publisher PDF, then Anna's Archive when configured. An explicit archive hash still selects that exact file. Use `provider="anna"` or `provider="openalex"` to select a provider explicitly; archive-specific search filters also route to Anna's Archive.

A known DOI or OpenAlex work ID can go directly to `article_download`. With only a title or citation, search first, compare authors, year and work type, and replay the selected hit's `download_args`. For dissertations, use `work_type="dissertation"`; a work ID such as `W1588575005` works even without a DOI. Search also supports author, institution and repository IDs, plus citing, referenced and related works. Replay `next_search_args` to continue a result list with the same provider and filters.

`article_details` exposes identifiers, abstracts, copies, versions, licenses and access links. Supply a location or strict version on download when that distinction matters. A hosted PDF may have an unknown version. A landing-page-only result provides a useful link but does not promise a downloadable file.

`article_read` returns bounded passages when structured full text is available. Reuse its continuation arguments for the next passage. An abstract is labeled separately from full text. Search and details do not download file bodies.

Downloads return an absolute path on the server host, bytes, provider and reuse evidence. A verified local copy avoids another content request. A client without access to that host can use `article_read` or the returned public work link. Mount the download directory into the host when using Docker.

Ask your agent *"Find dissertations about graph algorithms"* or *"Read the AlphaFold paper, DOI 10.1038/s41586-021-03819-2."* The same workflows are available from the CLI:

```bash
annas-cli article-search "graph algorithms" --work-type dissertation --json
annas-cli article-details W1588575005
annas-cli article-download --openalex-id W1588575005
annas-cli article-read 10.1038/s41586-021-03819-2
annas-cli account-status --provider openalex
```

OpenAlex credentials go only to its official API and content hosts. Direct repository and publisher PDF requests use no provider credentials. Queries go to the selected provider, and automatic fallback may query the other configured provider. Failed requests identify the provider and reason; an invalid key or exhausted allowance needs a key update or allowance reset, while a temporary rate limit may include a retry delay.

See the [tool reference](HOW-IT-WORKS.md#tools) for all options, and the [`papers` Go SDK](papers/README.md) for using these workflows in Go applications.

## Troubleshooting

Errors start with a code:

| Code | What to do |
| --- | --- |
| `[CONFIG]` | A required provider variable is missing or invalid, that provider rejected its key, or the download folder can't be written to. Check [Configuration](#configuration). |
| `[UPSTREAM_BLOCKED]` | The archive refused access. Check that your membership allows searching; when it does not (Brilliant Bookworm), the error says so and `account_status` shows `search_allowed: false`. If the error says no fast downloads are left, wait for the window to roll over; `account_status` shows the allowance. If signing in itself failed, see [Signing in](#signing-in). |
| `[NOT_FOUND]` | Nothing matched, or that copy has no fast download. Try another query, or another copy from the search results. |
| `[INVALID_ARGUMENT]` | Fix the hash, DOI, id, filter, sort, page, limit or timeout. |
| `[REQUEST_TIMEOUT]` | Retry, or raise [`timeout_seconds`](HOW-IT-WORKS.md#tools). |
| `[UPSTREAM]` | A service or download server failed. The error says which one and why; retry later. |

A failed download reports the sources it tried and why each failed. Read the reason before retrying; a rejected key needs a configuration change. When the work exists but no file was obtained, use its returned landing link or another available copy. Credentials are checked when an operation needs them, so a successful server startup alone does not confirm account access.

If the server doesn't start at all, run `npx -y annas-mcp --version` in a terminal to see the error. The launcher unpacks releases with the system's `tar`, which macOS, most Linux distributions and Windows 10 or later include.

## Command line

Every tool is also a command. Follow [CLI setup](#cli-setup), set the same variables (or put them in a `.env` file), then:

```bash
annas-cli book-search "pride and prejudice" --language en --format epub --include-stats
annas-cli book-search --author "Jane Austen" --year 1813 --sort oldest
annas-cli article-search 10.1038/nature14539
annas-cli article-details 10.1038/nature14539
annas-cli metadata-search 9780141439518
annas-cli book-details <hash>
annas-cli account-status
annas-cli book-download <hash> "Pride and Prejudice.epub"
annas-cli article-download 10.1038/nature14539
```

Add `--include-stats` to an archive book or article search to include per-file download, list, quality-vote, report and comment counts when available. Add `--json` for machine-readable output, `--json-errors` for structured failures on stderr, `--timeout 10m` to change the timeout and `--help` to any command for all of its options.

## Privacy

annas-mcp runs on your machine and has no telemetry. Anna's Archive receives your archive queries and credentials at the configured archive hosts. DOI resolution may contact doi.org. The npm launcher and bundle contact GitHub to fetch and verify release binaries. The cache stores release binaries, the archive session and mirror health, and metadata used for verified local file reuse. Downloaded files go to your configured output directory. Additional service requests are described under [optional OpenAlex support](#openalex-optional). Questions are welcome through [issues](https://github.com/SokolskyNikita/annas-mcp/issues).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for building from source, tests and releases.

## Credits

The idea for this project comes from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp), which first exposed Anna's Archive as an MCP server. This repository is a complete rewrite and shares no code with it.

## License

[MIT](LICENSE)
