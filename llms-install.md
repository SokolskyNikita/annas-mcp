# Installing annas-mcp

Instructions for an agent setting up annas-mcp. Read the [README](README.md) for the complete configuration and workflows.

## Pick the configured provider

Use Node.js 18 or newer. Configure `OPENALEX_API_KEY` for papers, dissertations, open PDFs and structured text. Configure `ANNAS_SECRET_KEY` for Anna’s Archive books, catalogs and archive retrieval. Configure both for eligible fallback. Archive search requires Lucky Librarian or higher; archive download requires a paid membership. OpenAlex has a separate usage allowance.

Use an existing exported credential or the client’s secret setting. If a credential is missing, ask the user to put it into the client configuration or local environment. Treat keys as passwords and keep their values out of chat, tool arguments and repository files. A desktop client may not inherit a key from `.zshrc.secrets`.

The optional `ANNAS_DOWNLOAD_PATH` is an absolute output folder. It defaults to `annas-archive` in the user’s home folder and is created on first download. On macOS, Desktop, Documents, Downloads and iCloud Drive may need Files and Folders permission. For Docker, mount the output folder and explain that returned paths are on the server host.

## Configure the client

An OpenAlex-only MCP entry looks like this. Add `ANNAS_SECRET_KEY` alongside `OPENALEX_API_KEY` when archive access is wanted.

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "annas-mcp"],
      "env": {
        "OPENALEX_API_KEY": "set-this-in-the-client-secret-config"
      }
    }
  }
}
```

On native Windows, use `"command": "cmd"` and `"args": ["/c", "npx", "-y", "annas-mcp"]`. For an archive-only install, use `ANNAS_SECRET_KEY` in the environment block. No browser cookie is needed unless archive sign-in fails. The program reads its process environment and existing dotenv configuration; it does not execute shell configuration files.

## Verify without downloading

1. Run `npx -y annas-mcp --version`. The launcher downloads and verifies the release binary.
2. Reload the client and check the nine tools are listed: `book_search`, `article_search`, `article_details`, `article_read`, `metadata_search`, `book_details`, `account_status`, `book_download` and `article_download`.
3. For OpenAlex, call `article_search` with `query` set to `10.1038/s41586-021-03819-2` and `provider` set to `openalex`. Verify the returned identity. Search does not fetch the PDF body. For archive access, call `book_search` with `query` set to `pride and prejudice`.
4. If diagnosis is needed, use `account_status` with the provider being checked. Avoid status calls before every normal request.

## Use the tools efficiently

For a supplied DOI or W ID and an explicit download request, call `article_download` directly. It resolves access internally. With only a title or an ambiguous citation, call `article_search`, compare authors, year and type, then replay the selected hit’s `download_args`. A no-DOI dissertation downloads by its W ID. Follow `next_search_args` for another page and preserve its provider and filters.

Use `article_details` to inspect abstracts, copies, versions and location IDs. A strict version or selected location constrains retrieval. Use `article_read` for bounded structured full text and follow its continuation. The first read may consume OpenAlex content allowance. Label an abstract as an abstract. A successful download returns its real server-side path and provider. An availability flag, landing page or record alone does not establish that the file was obtained.

## Recover from failures

Read the error’s provider, reason, retry information and suggested next action. A missing or rejected OpenAlex key concerns `OPENALEX_API_KEY`. A missing or rejected AA key concerns `ANNAS_SECRET_KEY`. A daily budget failure requires an allowance reset; a temporary throttle may include a retry time. If a work has no supported file route, return the best available landing page and explain what was found.

Archive membership and cookie errors apply to the archive route. When an error explicitly mentions `ANNAS_ACCOUNT_COOKIE`, give the supplied browser-cookie steps. For a local output permission failure, use the default output directory or follow the macOS permission guidance in the error. For startup failure, run the version command and report its error without exposing credentials.
