# annas-mcp

An MCP server and a CLI that search [Anna's Archive](https://annas-archive.gl) for books and journal articles, then download the file. Cursor, Claude, Codex, and other MCP clients call `book_search`, `book_download`, `article_search`, and `article_download`. The same binary runs those commands from the shell. A book download uses the MD5 hash from search. An article download uses that hash or a DOI.

[![Tests](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml/badge.svg)](https://github.com/SokolskyNikita/annas-mcp/actions/workflows/test.yml)
[![Release](https://img.shields.io/github/v/release/SokolskyNikita/annas-mcp)](https://github.com/SokolskyNikita/annas-mcp/releases)

This is [SokolskyNikita's fork](https://github.com/SokolskyNikita/annas-mcp) of [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp). The archive includes public-domain and Creative Commons works. Download a file only when you have the right to obtain it.

## Account cookie

Anna's Archive rejects clients that are not a browser session. Search sends the `aa_account_id2` cookie as `ANNAS_ACCOUNT_COOKIE`.

Set it to the raw value, to `aa_account_id2=...`, or to a full `Cookie` header. Only `aa_account_id2` is sent.

The cookie expires about once a week. This MCP stores the value you set and does not refresh it, so search starts returning 403 until you paste a new one and restart the server.

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

## Install

Node.js 18 or newer. `npx` downloads the binary for your system from the latest [release](https://github.com/SokolskyNikita/annas-mcp/releases), checks it against the published checksum, and caches it in `~/.cache/annas-mcp`. Each launch checks for a newer release before it starts. If that check fails, it starts the cached binary.

Search needs `ANNAS_ACCOUNT_COOKIE`. Downloads need `ANNAS_SECRET_KEY` and an absolute `ANNAS_DOWNLOAD_PATH`. The key comes from an [Anna's Archive membership](https://annas-archive.gl/donate) at Lucky Librarian or higher ([API FAQ](https://annas-archive.gl/faq#api)). A lower tier does not grant fast-download access, so downloads fail.

Put these in the client `env` block. A `.env` file is read from the process working directory, which is usually not the directory that contains the binary.

### Cursor and Claude Desktop

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

### Claude Code

```bash
claude mcp add annas-mcp \
  --env ANNAS_SECRET_KEY=your-api-key \
  --env ANNAS_DOWNLOAD_PATH=/absolute/path/to/downloads \
  --env ANNAS_ACCOUNT_COOKIE=your-aa-account-id2-value \
  -- npx -y github:SokolskyNikita/annas-mcp
```

### Codex

In `~/.codex/config.toml`:

```toml
[mcp_servers.annas-mcp]
command = "npx"
args = ["-y", "github:SokolskyNikita/annas-mcp"]

[mcp_servers.annas-mcp.env]
ANNAS_SECRET_KEY = "your-api-key"
ANNAS_DOWNLOAD_PATH = "/absolute/path/to/downloads"
ANNAS_ACCOUNT_COOKIE = "your-aa-account-id2-value"
```

## Tools

Search first. Pass `hash` from either search tool to `book_download` or `article_download`. Pass `doi` to `article_download`. A DOI that does not resolve is a tool error.

Use these when the user names a book, paper, standard, or citation and needs the file, not when a web snippet is enough.

- Search for the book *Designing Data-Intensive Applications* and download the EPUB.
- Look up DOI `10.48550/arXiv.1706.03762` and download the PDF.
- Find journal articles about attention mechanisms, then download the one whose description matches.

| Tool | What it returns |
| --- | --- |
| `book_search` | One page of books. Each hit has `hash`, `format`, and `description` when the page includes one. |
| `book_download` | `{"path":"/absolute/file.epub","bytes":300188}` |
| `article_search` | One paper when `query` is a DOI. Otherwise one page of journal articles, with `hash` and `description`. |
| `article_download` | The same path object as `book_download`. |

Search responses also include `matched` and `limit`. `matched` is how many hits were on the page. `limit` is how many were returned. The default limit is 10. Raise it, then raise `page`, when the file you want is not in the list.

```json
{
  "page": 1,
  "language": "en",
  "matched": 26,
  "limit": 10,
  "results": [
    {
      "hash": "abc123def456",
      "title": "Title",
      "authors": "Author",
      "publisher": "Publisher",
      "language": "English",
      "format": "EPUB",
      "size": "0.3MB",
      "description": "Short excerpt from the record.",
      "url": "https://annas-archive.gl/md5/abc123def456"
    }
  ]
}
```

Keyword article results use `journal` and `page_url` in place of `publisher` and `url`, and include `doi` when the record text contains one. The response also has `"index": "journals"`. A DOI passed to `article_search` returns one object: `doi`, `title`, `authors`, `journal`, `size`, `hash`, `description`, `page_url`. If SciDB has no file, lookup searches for the DOI and then for the title registered at doi.org.

The file is checked against the MD5 `hash`. An existing file with the same name is kept; the new file gets a short hash suffix.

Anna's search can still return an empty later page when its search server is slow. Retry that page, or change the query. Do not send `content=book_any`; it is not a current filter and makes later pages empty.

| Tool | Arguments | CLI |
| --- | --- | --- |
| `book_search` | `query`. Optional `content` (`book_fiction`, `book_nonfiction`, `book_unknown`, `book_comic`, `magazine`, `standards_document`), `language` (`en`), `page` (default 1), `limit` (default 10), `timeout_seconds` | `book-search --language en --content book_fiction "query"` |
| `book_download` | `hash`, `title`. Optional `format` (`pdf`, `epub`); otherwise taken from the response. `timeout_seconds` | `book-download abc123def456 "my-book.epub"` |
| `article_search` | `query`: a DOI (`10.…`) or keywords. Optional `content` (a book filter; otherwise the journals index), `language`, `page`, `limit` (default 10), `timeout_seconds` | `article-search "10.1038/nature12373"` |
| `article_download` | `doi` or `hash` from search. Optional `title`, `format`, `timeout_seconds` | `article-download "10.1038/nature12373"` |

The CLI prints the whole page unless you pass `--limit`.

### Errors

Tool failures are returned as MCP errors. The text starts with a stable code.

| Code | Meaning |
| --- | --- |
| `[INVALID_ARGUMENT]` | The arguments are wrong. Change them and call again. |
| `[NOT_FOUND]` | The DOI or query did not resolve to a file. |
| `[CONFIG]` | `ANNAS_SECRET_KEY` or `ANNAS_DOWNLOAD_PATH` is missing or not absolute. |
| `[UPSTREAM_BLOCKED]` | Anna's Archive returned 403. Refresh `ANNAS_ACCOUNT_COOKIE` and restart. |
| `[REQUEST_TIMEOUT]` | The call was cancelled or the timeout elapsed. |
| `[UPSTREAM]` | The archive or the download failed for another reason. |

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `ANNAS_ACCOUNT_COOKIE` | Search | `aa_account_id2` value. It expires about once a week. |
| `ANNAS_SECRET_KEY` | Downloads | API key from Lucky Librarian or a higher tier. Lower tiers cannot download. |
| `ANNAS_DOWNLOAD_PATH` | Downloads | Absolute directory where files are saved. |
| `ANNAS_BASE_URL` | | Mirror hostname used when automatic selection is off or fails. Default `annas-archive.gl`. |
| `ANNAS_AUTO_BASE_URL` | | Automatic mirror selection. Default on. Set to `false` to use `ANNAS_BASE_URL` only. |

### Mirrors

Automatic selection is the default. The server reads the [SLUM](https://open-slum.org/) page, prefers mirrors marked up, then protected, probes them, and uses the first that answers. If discovery or probing fails, it uses `ANNAS_BASE_URL`, then `annas-archive.gl`.

Set `ANNAS_AUTO_BASE_URL=false` to skip that and send every request to `ANNAS_BASE_URL`, or to `annas-archive.gl` when that variable is unset.
