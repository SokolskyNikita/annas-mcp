# Anna's Archive MCP server and CLI

An [MCP server](https://modelcontextprotocol.io/introduction) and command-line tool for searching and downloading books and articles from [Anna's Archive](https://annas-archive.gl). It can also select a mirror that [SLUM](https://open-slum.org/) currently reports as healthy.

> [!IMPORTANT]
> This repository is [SokolskyNikita's fork](https://github.com/SokolskyNikita/annas-mcp) of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp). The original project has not been updated for about three months (last change in June 2026). This fork exists to keep the server and CLI fully working.

> [!NOTE]
> Anna's Archive holds a large collection of documents, including works under permissive licenses such as Creative Commons and materials in the public domain. This project is a retrieval utility. Use it only where you have the right to obtain a work, and respect the effort that goes into creating one.

> [!WARNING]
> Mirrors go offline. If a link in this document fails to open, see [Mirror selection](#mirror-selection).

## Requirements

Search requires `ANNAS_ACCOUNT_COOKIE`, the `aa_account_id2` session cookie from a browser that can open Anna's Archive. See [Account cookie](#account-cookie).

Downloads require:

- [A donation to Anna's Archive](https://annas-archive.gl/donate), which grants JSON API access
- [An API key](https://annas-archive.gl/faq#api)

To run the MCP server, you also need an MCP client such as [Claude Desktop](https://claude.ai/download).

## Setup

Download a binary from the original project's [GitHub releases](https://github.com/iosifache/annas-mcp/releases).

To use the MCP server, register the binary with your client. Claude Desktop example:

```json
"anna-mcp": {
    "command": "/path/to/annas-mcp",
    "args": ["mcp"],
    "env": {
        "ANNAS_SECRET_KEY": "your-api-key",
        "ANNAS_DOWNLOAD_PATH": "/path/to/downloads",
        "ANNAS_BASE_URL": "annas-archive.gl",
        "ANNAS_ACCOUNT_COOKIE": "your-aa-account-id2-value"
    }
}
```

## Configuration

Set these as environment variables, or store them in an `.env` file in the same directory as the binary.

| Variable | Required for | Description |
| --- | --- | --- |
| `ANNAS_SECRET_KEY` | Downloads | Anna's Archive API key. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory where files are saved. |
| `ANNAS_BASE_URL` | Optional | Mirror hostname. Defaults to `annas-archive.gl`. When automatic discovery is on, this is the fallback. |
| `ANNAS_AUTO_BASE_URL` | Optional | Set to `true` to choose a mirror from [SLUM](https://open-slum.org/). |
| `ANNAS_ACCOUNT_COOKIE` | Search | Value of the `aa_account_id2` cookie. See [Account cookie](#account-cookie). |

### Account cookie

Anna's Archive blocks clients that are not a normal browser session. Search sends the `aa_account_id2` cookie so those requests get through.

1. Open [Anna's Archive](https://annas-archive.gl) in a browser and wait until the page loads.
2. Open developer tools. In Firefox, use **Storage → Cookies**. In Chrome, use **Application → Cookies**.
3. Select `https://annas-archive.gl` and copy the value of the cookie named `aa_account_id2`.
4. Set `ANNAS_ACCOUNT_COOKIE` to that value. You can also paste `aa_account_id2=...` or the whole `Cookie` header; the client keeps only `aa_account_id2`.

The cookie expires. When search starts failing, open the site again and copy a new value.

### Mirror selection

Anna's Archive publishes several mirrors, and their availability changes. With automatic discovery off, the tool uses `ANNAS_BASE_URL`, or `annas-archive.gl` when that variable is unset.

With `ANNAS_AUTO_BASE_URL=true`, the tool reads the public SLUM status page, ranks Anna mirror candidates by recent health and latency, probes them locally, and uses the best reachable mirror. If discovery or probing fails, it falls back to `ANNAS_BASE_URL`, then to `annas-archive.gl`.

### Timeouts

HTTP requests time out after one hour by default.

On the CLI, override the timeout with `--timeout`:

```bash
annas-mcp --timeout 1h book-download abc123def456 "my-book.pdf"
```

MCP tools accept an optional `timeout_seconds` argument. Pass `3600` for one hour.

## Demo

### MCP server

<img src="screenshots/claude.png" width="600" alt="Claude Desktop searching Anna's Archive through the MCP server" />

### CLI

<img src="screenshots/cli.png" width="400" alt="Book search in the annas-mcp CLI" />

## Operations

| Operation | MCP tool | CLI command | Example |
| --- | --- | --- | --- |
| Search books by title, author, or topic | `book_search` | `book-search` | `book-search "machine learning python"` |
| Download a book by its MD5 hash | `book_download` | `book-download` | `book-download abc123def456 "my-book.pdf"` |
| Search articles by DOI or keywords | `article_search` | `article-search` | `article-search "10.1038/nature12345"` |
| Download an article by its DOI | `article_download` | `article-download` | `article-download "10.1038/nature12345"` |
