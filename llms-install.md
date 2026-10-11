# Installing annas-mcp

Instructions for an AI agent setting up this MCP server for a user. The [README](README.md) has the full documentation.

## Requirements

- Node.js 18 or newer (`node --version`).
- An Anna's Archive membership. Searching needs Lucky Librarian or higher; downloading works with any tier.

## Ask the user for

1. **Secret key** (required): the key they log in to Anna's Archive with. It is on their account page, https://annas-archive.gl/account/. Treat it as a password: put it only in the MCP config, never in chat output or files in a repository.
2. **Download folder** (optional): files go to `annas-archive` in the user's home folder unless `ANNAS_DOWNLOAD_PATH` names another absolute folder, which is created if missing. Only ask if the user wants a different folder. Avoid `/tmp`, which the operating system clears. On macOS, Desktop, Documents and Downloads make macOS ask the user for permission on the first download.

## Configure the client

Add this server entry to the client's MCP settings, replacing the secret key:

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "npx",
      "args": ["-y", "annas-mcp"],
      "env": {
        "ANNAS_SECRET_KEY": "the-users-secret-key"
      }
    }
  }
}
```

On native Windows (not WSL), use `"command": "cmd"` and `"args": ["/c", "npx", "-y", "annas-mcp"]`.

No other variables are needed. The server signs in with the secret key by itself and saves the session; it never needs a browser cookie unless signing in fails.

## Verify

1. Run `npx -y annas-mcp --version` in a terminal. It prints the version once the release binary is downloaded and verified.
2. Restart or reload the MCP client so it starts the server, and check that it lists four tools: `book_search`, `article_search`, `book_download` and `article_download`.
3. Call `book_search` with the query `pride and prejudice`. A page of results means the secret key and membership work.

## If something fails

Errors start with a code:

- `[CONFIG]`: a variable is missing or invalid, or Anna's Archive rejected the secret key. Ask the user to copy the key again. If it says it cannot write to the download folder, follow the steps in the error or remove `ANNAS_DOWNLOAD_PATH` to use the default folder.
- `[UPSTREAM_BLOCKED]`: the membership doesn't allow searching (it needs Lucky Librarian or higher), or signing in failed. If the error mentions `ANNAS_ACCOUNT_COOKIE`, give the user the steps it lists for copying the `aa_account_id2` cookie from their browser, and add that value as `ANNAS_ACCOUNT_COOKIE` in the `env` block.
- The server doesn't start: run `npx -y annas-mcp --version` and report its error.
