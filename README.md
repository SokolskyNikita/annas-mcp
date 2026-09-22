# annas-mcp

MCP server and CLI for searching and downloading books and articles from [Anna's Archive](https://annas-archive.gl).

This is [SokolskyNikita's fork](https://github.com/SokolskyNikita/annas-mcp) of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp). Upstream last changed in June 2026.

The archive includes public-domain and Creative Commons works. Download a file only when you have the right to obtain it.

## Install

Node.js 18 or newer. `npx` downloads the binary for your system from the latest [release](https://github.com/SokolskyNikita/annas-mcp/releases), checks it against the published checksum, and caches it in `~/.cache/annas-mcp`. The next launch starts that binary immediately. A newer release is downloaded in the background and used on the following launch.

Search needs `ANNAS_ACCOUNT_COOKIE`. Downloads need `ANNAS_SECRET_KEY` and an absolute `ANNAS_DOWNLOAD_PATH`. The key comes from an [Anna's Archive membership](https://annas-archive.gl/donate) at Lucky Librarian or higher ([API FAQ](https://annas-archive.gl/faq#api)). A lower tier does not grant fast-download access, so downloads fail.

Put these in the client `env` block. A `.env` file is read from the process working directory, which is usually not the directory that contains the binary.

### MCP client

Cursor, Claude Desktop, and other clients that read MCP JSON:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "github:SokolskyNikita/annas-mcp"],
      "env": {
        "ANNAS_SECRET_KEY": "your-api-key",
        "ANNAS_DOWNLOAD_PATH": "/absolute/path/to/downloads",
        "ANNAS_ACCOUNT_COOKIE": "your-aa-account-id2-value"
      }
    }
  }
}
```

Claude Code:

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-api-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  --env ANNAS_ACCOUNT_COOKIE=your-aa-account-id2-value \
  -- npx -y github:SokolskyNikita/annas-mcp
```

Codex, in `~/.codex/config.toml`:

```toml
[mcp_servers.annas-mcp]
command = "npx"
args = ["-y", "github:SokolskyNikita/annas-mcp"]

[mcp_servers.annas-mcp.env]
ANNAS_SECRET_KEY = "your-api-key"
ANNAS_DOWNLOAD_PATH = "/absolute/path/to/downloads"
ANNAS_ACCOUNT_COOKIE = "your-aa-account-id2-value"
```

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | Search | `aa_account_id2` value. It expires about once a week. See below. |
| `ANNAS_SECRET_KEY` | Downloads | API key from Lucky Librarian or a higher tier. Lower tiers cannot download. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory where files are saved. |
| `ANNAS_BASE_URL` | | Mirror hostname used when automatic selection is off or fails. Default `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | | Automatic mirror selection. Default on. Set to `false` to use `ANNAS_BASE_URL` only. |

### Account cookie

Anna's Archive rejects clients that are not a browser session. Search sends the `aa_account_id2` cookie.

Set `ANNAS_ACCOUNT_COOKIE` to the raw value, to `aa_account_id2=...`, or to a full `Cookie` header. Only `aa_account_id2` is sent.

The cookie expires about once a week. This MCP stores the value you set and does not refresh it, so search starts returning 403 until you paste a new one and restart the server. That weekly refresh is a current limitation.

Open [annas-archive.gl](https://annas-archive.gl) and wait until the page loads, then copy `aa_account_id2` with either method below.

**Cookie store**

1. Open developer tools.
2. In Chrome, open **Application → Cookies** and select the site. In Firefox, open **Storage → Cookies**.
3. Copy the value of `aa_account_id2`.

**Network request**

1. Open developer tools and switch to the **Network** tab.
2. Reload the page and select a request to the archive.
3. Copy `aa_account_id2` from the request's **Cookies** list, or from the `Cookie` request header.

A 403 on search means the cookie has expired. Repeat either method, update `ANNAS_ACCOUNT_COOKIE`, and restart the MCP process so it reads the new value.

### Mirrors

Automatic selection is the default. The server reads the [SLUM](https://open-slum.org/) page, prefers mirrors marked up, then protected, probes them, and uses the first that answers. If discovery or probing fails, it uses `ANNAS_BASE_URL`, then `annas-archive.gl`.

Set `ANNAS_AUTO_BASE_URL=false` to skip that and send every request to `ANNAS_BASE_URL`, or to `annas-archive.gl` when that variable is unset.

### Timeouts

Search waits 60 seconds. Downloads wait 30 minutes. Cancelling the MCP tool call stops the HTTP request.

Override either limit on the CLI with `--timeout`:

```bash
annas-mcp --timeout 1h book-download abc123def456 "my-book.pdf"
```

MCP tools take an optional `timeout_seconds` argument.

## Tools

Search first. Pass `hash` from `book_search` to `book_download`. Pass `doi` to `article_download`. A DOI that does not resolve is a tool error.

`book_search`, and `article_search` with keywords, return one page:

```json
{
  "page": 1,
  "content": "book_any",
  "language": "en",
  "results": [
    {
      "hash": "abc123def456",
      "title": "Title",
      "authors": "Author",
      "publisher": "Publisher",
      "language": "English",
      "format": "EPUB",
      "size": "0.3MB",
      "url": "https://annas-archive.gl/md5/abc123def456"
    }
  ]
}
```

Keyword article results use `journal` and `page_url` in place of `publisher` and `url`. A DOI passed to `article_search` returns one object: `doi`, `title`, `authors`, `journal`, `size`, `hash`, `page_url`.

Both download tools return `{"path":"/absolute/file.epub","bytes":300188}`. The file is checked against the MD5 `hash`. An existing file with the same name is kept; the new file gets a short hash suffix.

Anna's search can return an empty later page, with the text "No files found", even when more matches exist. Retry the same page, or change the query.

| Tool | Arguments | CLI |
| --- | --- | --- |
| `book_search` | `query`. Optional `content` (default `book_any`; also `book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine`, `standards_document`, and other Anna content tokens), `language` (`en`), `page` (default 1), `timeout_seconds` | `book-search --language en --content book_fiction "query"` |
| `book_download` | `hash`, `title`. Optional `format` (`pdf`, `epub`); otherwise taken from the response. `timeout_seconds` | `book-download abc123def456 "my-book.epub"` |
| `article_search` | `query`: a DOI (`10.…`) or keywords. Optional `content` (default `journal`), `language`, `page`, `timeout_seconds` | `article-search "10.1038/nature12373"` |
| `article_download` | `doi`, optional `timeout_seconds` | `article-download "10.1038/nature12373"` |

## License

[MIT](LICENSE)
