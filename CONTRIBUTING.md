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
      "env": { "ANNAS_ACCOUNT_COOKIE": "…", "ANNAS_SECRET_KEY": "…", "ANNAS_DOWNLOAD_PATH": "…" }
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
| `internal/anna` | Archive search, DOI lookup and downloads |
| `internal/mirror` | Mirror discovery and probing |
| `internal/env`, `internal/apperr`, `internal/version` | Configuration, stable error codes, embedded version |
| `bin`, `lib` | The `npx` launcher: release lookup, checksum verification, archive extraction and caching |
| `server.json`, `manifest.json`, `glama.json` | Listings for the MCP Registry, the MCP Bundle (Claude Desktop, Smithery) and Glama |
| `assets` | Icon (`icon.svg`, rendered to `icon.png` at 512×512) |
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

CI runs the same checks on Linux, macOS and Windows. It also lints the workflows with actionlint, syntax-checks the shell scripts and builds a GoReleaser snapshot.

### Live tests

These use real credentials from your environment or `.env`.

- `node scripts/smoke-mcp.mjs ./annas-mcp` exercises all four MCP tools against the live archive, including real downloads (so it uses download quota), and verifies sizes and checksums.
- `scripts/healthcheck.sh --live` runs real searches without downloading anything.
- `ANNAS_MCP_TEST_SCIDB_LIVE=1 go test ./internal/anna -run '^TestLiveSciDBFallback$' -count=1 -v` tests the SciDB PDF fallback on its own.

## Releasing

1. Set the new version in `internal/version/version.txt` (`vX.Y.Z`) and `package.json` (`X.Y.Z`), and add an entry to [CHANGELOG.md](CHANGELOG.md).
2. Run the checks above, `goreleaser check -f .goreleaser` and the live smoke test.
3. Commit, push to `main` and wait for CI to pass.
4. Run `scripts/manage-tag.sh add` to create and push the annotated tag.

Step 1 also covers `server.json` (both `version` fields) and `manifest.json`; `npm run check:version` fails if any of them disagree.

The tag triggers the release workflow. It builds the binaries, publishes them with SHA-256 checksums to GitHub Releases, attaches `annas-mcp.mcpb` (built by `npm run pack:mcpb`), then installs the release through the `npx` launcher on each platform and checks the MCP handshake and all four tools. `node scripts/verify-release.mjs vX.Y.Z` repeats that check locally.

When the repository variable `PUBLISH_REGISTRIES` is `true`, the workflow then publishes the launcher to npm through npm trusted publishing and the new version to the [MCP Registry](https://registry.modelcontextprotocol.io) through GitHub OIDC. Neither needs a stored secret. The npm package holds only the launcher (`bin`, `lib`); binaries always come from GitHub Releases.

After the release workflow finishes, run `npm run publish:smithery` to update the [Smithery](https://smithery.ai/servers/sokolx/annas-mcp) listing. It needs `zip` and a one-time `npx @smithery/cli auth login`; `--dry-run` builds the bundle without publishing.

Never move or recreate a published tag: the launcher caches binaries by release and checksum.
