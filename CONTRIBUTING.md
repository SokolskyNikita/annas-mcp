# Contributing

Contributions that improve search accuracy, download reliability, mirror handling, the MCP tool schemas or the cross-platform launcher are welcome.

## Build from source

Requires Go 1.26 or newer.

```bash
git clone https://github.com/SokolskyNikita/annas-mcp.git
cd annas-mcp
go build -o ./annas-mcp ./cmd/annas-mcp
```

To try your build in an MCP client, point the client at the binary with the `mcp` argument and the usual [environment variables](README.md#configuration):

```json
{
  "mcpServers": {
    "annas-mcp": {
      "command": "/absolute/path/to/annas-mcp",
      "args": ["mcp"],
      "env": { "ANNAS_SECRET_KEY": "…" }
    }
  }
}
```

The `npx` launcher always runs the latest published release, never your checkout. `go install github.com/SokolskyNikita/annas-mcp/cmd/annas-mcp@latest` installs the latest tag without Node.

## Project layout

| Path | Contents |
| --- | --- |
| `cmd/annas-mcp` | Executable entry point |
| `internal/modes` | MCP server and CLI, sharing one service layer for validation and output |
| `internal/anna` | Archive search and result parsing, record details, account status, DOI lookup, downloads and sign-in, including the saved session |
| `internal/mirror` | Mirror selection: health checks, holds on failing mirrors and the official mirror list |
| `internal/env`, `internal/apperr`, `internal/version` | Configuration, stable error codes, embedded version |
| `bin`, `lib` | The `npx` launcher: release lookup, checksum verification, archive extraction and caching |
| `server.json`, `manifest.json`, `glama.json` | Listings for the MCP Registry, the MCP Bundle (Claude Desktop, Smithery) and Glama |
| `Dockerfile` | Container image for `docker run -i` and Docker's MCP Catalog |
| `HOW-IT-WORKS.md` | Tool options and results, downloads, mirror selection, the cache folder and the saved session; shipped in release archives and the MCP Bundle |
| `llms-install.md` | Setup steps for AI agents that install MCP servers for a user, such as Cline |
| `assets` | Icon (`icon.svg`, rendered to `icon.png` at 512×512) and the GitHub social preview (`social-preview.svg`, rendered to `social-preview.png` at 1280×640) |
| `docs` | Project website, served by GitHub Pages. Keep its setup snippets in step with the README |
| `plugin.json`, `mcp.json` | [Agent Plugins](https://agent-plugins.org) manifest for plugin directories such as cursor.directory. It carries no credentials; the server reads the `ANNAS_*` variables from its environment |
| `scripts` | Test, health-check and release tooling |

## Checks

Run these before opening a pull request. None of them need archive credentials; they use fixtures and a mock release server.

```bash
gofmt -w cmd internal
go vet ./...
npm run check:version   # embedded and npm versions must agree
npm test                # version, launcher and stdio integration tests
npm run coverage        # Go race tests; requires 80% total coverage
```

CI runs the same checks on Linux, macOS and Windows. It also lints the workflows with actionlint, syntax-checks the shell scripts, builds a GoReleaser snapshot and builds the Docker image, checking its MCP handshake with `scripts/check-docker.mjs`. `scripts/check-package-contents.mjs` checks that test data stays out of the binary, the npm package, the image, the release archives and the MCP Bundle.

### Captured pages

The parser tests read real pages from Anna's Archive and DOI.org, saved gzipped in `internal/anna/parse/testdata/pages` and listed in its `manifest.json`. Each page's parsed output is kept beside it as `<name>.golden.json`, and `TestGoldenPages` fails when the parsers make anything else of it.

- After changing a parser, run `go test ./internal/anna -run TestGoldenPages -update` and review the diff of the `.golden.json` files: every change in it should be one you meant.
- To refresh the pages, or after adding one to the manifest, run `ANNAS_MCP_CAPTURE=1 go test ./internal/anna -run '^TestCapturePages$' -count=1 -v`, then update the outputs the same way. Capturing signs in with `ANNAS_SECRET_KEY` and reads pages without downloading files. It replaces the account's id, cookie and key, and SciDB's signed file links, and fails if any of them remains.
- Check new outputs against the live pages before committing them, so the tests describe what the archive shows rather than what the parser happened to read.

### Live tests

These use real credentials from your environment or `.env`.

- `node scripts/smoke-mcp.mjs ./annas-mcp` exercises all seven MCP tools against the live archive, including real downloads (so it uses download quota), and verifies sizes and checksums. It ignores `ANNAS_ACCOUNT_COOKIE` and saves the session in a temporary folder, so it always tests signing in with `ANNAS_SECRET_KEY`.
- `scripts/healthcheck.sh --live` runs real searches without downloading anything.
- `ANNAS_MCP_TEST_SCIDB_LIVE=1 go test ./internal/anna -run '^TestLiveSciDBFallback$' -count=1 -v` tests the SciDB PDF fallback on its own.
- `ANNAS_MCP_TEST_LIVE=1 go test ./internal/anna -run '^TestLiveSearchDetailsAndAccount$' -count=1 -v` checks search filters and result cards, catalog search, `book_details` and the account page against the live archive. It downloads nothing, so it uses no download quota.
- `ANNAS_MCP_TEST_MIRROR_LIVE=1 go test ./internal/mirror -run '^TestLiveSelect$' -count=1 -v` selects a mirror the way a first run does, then checks each official mirror and logs how long it took. It needs no credentials.

## Releasing

1. Run `npm run bump patch` (or `minor`, `major` or an explicit `X.Y.Z`). It sets the version in `internal/version/version.txt`, `package.json`, `server.json` and `manifest.json`, and adds the release's heading to [CHANGELOG.md](CHANGELOG.md). Write the release notes under that heading.
2. Run the checks above, `goreleaser check -f .goreleaser` and the live smoke test.
3. Commit, push to `main` and wait for CI to pass.
4. Run `scripts/manage-tag.sh add` to create and push the annotated tag.

The tag triggers the release workflow. It builds the binaries, publishes them with SHA-256 checksums to GitHub Releases, attaches `annas-mcp.mcpb` (built by `npm run pack:mcpb`), then installs the release through the `npx` launcher on each platform and checks the MCP handshake and all seven tools. `node scripts/verify-release.mjs vX.Y.Z` repeats that check locally.

When the repository variable `PUBLISH_REGISTRIES` is `true`, the workflow then publishes the launcher to npm through npm trusted publishing and the new version to the [MCP Registry](https://registry.modelcontextprotocol.io) through GitHub OIDC. Neither needs a stored secret. The npm package holds only the launcher (`bin`, `lib`); binaries always come from GitHub Releases.

With the same variable set, the workflow also publishes the bundle to [Smithery](https://smithery.ai/servers/sokolx/annas-mcp) with `npm run publish:smithery`. Smithery has no keyless publishing, so that job needs the repository secret `SMITHERY_API_KEY`, an API key from your Smithery account. To publish by hand, run `npm run publish:smithery` after the release workflow finishes. It needs `zip` and a one-time `npx @smithery/cli auth login`; `--dry-run` builds the bundle without publishing.

Never move or recreate a published tag: the launcher caches binaries by release and checksum.
