# Contributing

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

The npm launchers always run the latest published release, never your checkout. `npm install -g annas-mcp` installs both `annas-cli` (help by default) and `annas-mcp` (MCP by default); both use the same cached native binary. The CLI launcher sets the child process's `argv[0]` to `annas-cli` so help uses the installed command's name. `go install github.com/SokolskyNikita/annas-mcp/cmd/annas-mcp@latest` installs the latest tag without Node, under the name `annas-mcp`; the native binary shows help by default.

## Project layout

| Path | Contents |
| --- | --- |
| `cmd/annas-mcp` | Executable entry point |
| `internal/modes` | MCP server and CLI, sharing one service layer for interface input and output |
| `anna` | Public SDK and error contract: search, record details, account status, DOI lookup, downloads and sign-in, including the saved session |
| `anna/internal/parse` | Everything that reads Anna's Archive's and DOI.org's answers |
| `anna/internal/match` | Checks that a record is the paper a DOI names |
| `anna/internal/mirror` | Mirror selection: health checks, holds on failing mirrors and the official mirror list |
| `internal/env`, `internal/version` | Application configuration and embedded version |
| `anna/scripts` | Upstream watch implementation, tests and reviewed revision |
| `internal/testpolicy` | Checks on the repository's own tests (see [Test data](#test-data)) |
| `bin`, `lib` | The `annas-cli` and `annas-mcp` npm entry points and shared launcher: release lookup, checksum verification, archive extraction and caching |
| `server.json`, `manifest.json`, `gemini-extension.json`, `glama.json` | Listings for the MCP Registry, the MCP Bundle (Claude Desktop, Smithery), the Gemini CLI extension gallery and Glama |
| `Dockerfile` | Container image for `docker run -i` and Docker's MCP Catalog |
| `HOW-IT-WORKS.md` | Tool options and results, downloads, mirror selection, the cache folder and the saved session; shipped in release archives and the MCP Bundle |
| `llms-install.md` | Setup steps for AI agents that install MCP servers for a user, such as Cline |
| `assets` | Icon (`icon.svg`, rendered to `icon.png` at 512×512) and the GitHub social preview (`social-preview.svg`, rendered to `social-preview.png` at 1280×640) |
| `docs` | Project website, served by GitHub Pages. Keep its setup snippets in step with the README |
| `plugin.json`, `mcp.json` | [Agent Plugins](https://agent-plugins.org) manifest for plugin directories such as cursor.directory |
| `scripts` | Test, health-check and release tooling |

## Checks

Run these before opening a pull request. None of them need archive credentials.

```bash
gofmt -w anna cmd internal
go vet ./...
npm run check:version   # embedded and npm versions must agree
npm test                # version, launcher, stdio and external-module SDK checks
npm run coverage        # Go race tests; needs 80% total coverage, and 95% for parse and match from their own tests
```

CI runs the same checks on Linux, macOS and Windows. It also lints the workflows and builds a GoReleaser snapshot and the Docker image. `scripts/check-package-contents.mjs` checks that test data stays out of the binary, the npm package, the image, the release archives and the MCP Bundle.

### Test data

Test data lives in files rather than Go source: case tables in each package's `testdata/cases/<name>.json`, page snippets and API answers beside them, and fuzz seeds in `testdata/fuzz/<FuzzName>`. Case files are decoded strictly, so a misspelled field fails the test. Test data stays plain text so it reads and diffs well; only the captured pages and the corpus, which are large, are gzipped.

`internal/testpolicy` enforces this. It fails on gzipped test data elsewhere, and on a test that writes markup, JSON, text spanning lines, a string over 60 bytes or a table of more than five strings inline. A literal the test is about can stay, with a comment saying `literal:` and why.

### Captured pages

The parser tests read real pages, saved gzipped in `anna/internal/parse/testdata/pages` and listed in its `manifest.json`. What the parsers make of each page is kept beside it as `<name>.golden.json`, and `TestCapturedPages` fails when that changes, showing the first line that differs. The client's tests serve the same pages from fake mirrors.

- After changing a parser, run `go test ./anna/internal/parse -run TestCapturedPages -update` and review the diff of the `.golden.json` files: every change in it should be one you meant.
- To refresh the pages, or after adding one to the manifest, run `ANNAS_MCP_CAPTURE=1 go test ./anna -run '^TestCapturePages$' -count=1 -v` (or set `ANNAS_MCP_CAPTURE` to a comma-separated list of page names), then update the outputs the same way. Capturing signs in with `ANNAS_SECRET_KEY` and downloads no files. It replaces the account's id, cookie and key and SciDB's signed file links, and fails if any of them remains. It drops the pages' scripts, which the parsers never read, except from the `search_books` page (`keep_scripts` in the manifest), so the tests still check that the parsers ignore script text.
- Check new outputs against the live pages before committing them. A golden file only records what the parser read, right or wrong.

### Search card corpus

The text rules also run over about 11,600 strings from real search cards (metadata lines, author lists, publisher links and titles), gzipped in `anna/internal/parse/testdata/corpus`. What the rules make of each is kept beside them as `<name>.golden.jsonl.gz`. `TestCorpus` fails when that changes, and `-update` refreshes it as for the pages. `TestCorpusHasNothingUnread` checks that every part of every metadata line is recognized.

To add more strings, run `ANNAS_MCP_CRAWL=1 go test ./anna -run '^TestCrawlCorpus$' -count=1 -v -timeout 75m`. It signs in with `ANNAS_SECRET_KEY` and runs the searches in `anna/testdata/crawl_searches.json`, one every 20 seconds, downloading nothing. To read corpus changes in `git diff` and `git log -p`, run `git config diff.gzip.textconv "gzip -dc"` once.

### Fuzz tests

Fuzz tests in `parse` and `match` check properties that must always hold, such as cleaning a cleaned author list changing nothing. `go test` runs their seeds, which include the corpus. To search further, run one for a while, for example `go test ./anna/internal/parse -run '^$' -fuzz '^FuzzCleanAuthors$' -fuzztime 1m`, and keep any failing input as a seed once it is fixed.

### Watching Anna's Archive's source

`npm run check:upstream` lists commits to Anna's Archive's source that may break the parsers. It reads the templates and code listed in `anna/scripts/upstream-watch.json`, which render search results, record pages, SciDB pages and the account page, and flags changes near the parsers' hooks. Commits that only change whitespace, comments, scripts, styles, unused classes or translation placeholders are left out. Commits that touch no hook are still listed, since the hook list can miss things. The check keeps a partial clone of Anna's repository in `~/.cache/annas-mcp` (`--repo` sets another folder) and needs no credentials.

After checking each relevant commit against `anna/internal/parse` and recapturing affected pages, run `npm run check:upstream -- --accept` and commit `anna/scripts/upstream-watch.json`. When a parser starts relying on a new part of Anna's markup, add it to `hooks` in `anna/internal/parse/hooks.go`; `TestUpstreamWatchCoversHooks` then fails until `upstream-watch.json` watches it.

A workflow runs the same check every Monday. It opens an issue labelled `upstream-change` for relevant commits, adds later ones to it and closes it once everything is accepted.

### Live tests

These use real credentials from your environment or `.env`.

- `node scripts/smoke-mcp.mjs ./annas-mcp` exercises all seven MCP tools against the live archive, including real downloads (so it uses download quota), and verifies sizes and checksums. It ignores `ANNAS_ACCOUNT_COOKIE` and saves the session in a temporary folder, so it always tests signing in with `ANNAS_SECRET_KEY`.
- `go build -o annas-mcp ./cmd/annas-mcp` followed by `npm run test:live:books` searches, reads details and downloads six public-domain books through both a real stdio MCP process and CLI processes. It independently verifies all MD5 hashes, then checks changed-title and renamed-file reuse for two books per interface using fresh processes without credentials and with an unusable archive origin. The temporary output directory retains downloads, a JSON report, and sanitized stderr logs; temporary sessions are deleted. This opt-in check uses the configured account and download allowance. Pass a different binary with `npm run test:live:books -- /absolute/path/to/binary`.
- `ANNAS_MCP_TEST_DEDUPE_LIVE=1 go test ./anna -run '^TestLiveDownloadDeduplicationAcrossFiveBooks$' -count=1 -v` checks five live book downloads and confirms repeats reuse the matching local file. It reads credentials from the environment or repository `.env`; an optional absolute `ANNAS_SESSION_FILE` reuses a saved session; any new file downloads use quota.
- `scripts/healthcheck.sh --live` runs real searches without downloading anything.
- `ANNAS_MCP_TEST_SCIDB_LIVE=1 go test ./anna -run '^TestLiveSciDBFallback$' -count=1 -v` tests the SciDB PDF fallback on its own.
- `ANNAS_MCP_TEST_LIVE=1 go test ./anna -run '^TestLiveSearchDetailsAndAccount$' -count=1 -v` checks search filters and result cards, catalog search, `book_details` and the account page against the live archive. It downloads nothing, so it uses no download quota.
- `ANNAS_MCP_TEST_MIRROR_LIVE=1 go test ./anna/internal/mirror -run '^TestLiveSelect$' -count=1 -v` selects a mirror the way a first run does, then checks each official mirror and logs how long it took. It needs no credentials.

## Releasing

The public Go SDK and applications share the module version and release tags. Update SDK usage/version examples in `anna/README.md`, the root README and the website when releasing SDK changes. No separate SDK tag or publishing job is needed.

1. Run `npm run bump patch` (or `minor`, `major` or an explicit `X.Y.Z`). It sets the version in `internal/version/version.txt`, `package.json`, `server.json`, `manifest.json` and `gemini-extension.json`, and adds the release's heading to [CHANGELOG.md](CHANGELOG.md). Write the release notes under that heading.
2. Run the checks above, `goreleaser check -f .goreleaser`, `npm run check:upstream` and the live smoke test.
3. Commit, push to `main` and wait for CI to pass.
4. Run `scripts/manage-tag.sh add` to create and push the annotated tag.

The tag triggers the release workflow. It builds the binaries, publishes them with SHA-256 checksums to GitHub Releases, attaches `annas-mcp.mcpb` (built by `npm run pack:mcpb`), then installs the release through the `npx` launcher on each platform and checks the MCP handshake and all seven tools. `node scripts/verify-release.mjs vX.Y.Z` repeats that check locally.

When the repository variable `PUBLISH_REGISTRIES` is `true`, the workflow then publishes the launcher to npm through npm trusted publishing and the new version to the [MCP Registry](https://registry.modelcontextprotocol.io) through GitHub OIDC. Neither needs a stored secret. The npm package holds only the launcher (`bin`, `lib`); binaries always come from GitHub Releases.

With the same variable set, the workflow also publishes the bundle to [Smithery](https://smithery.ai/servers/sokolx/annas-mcp) with `npm run publish:smithery`. Smithery has no keyless publishing, so that job needs the repository secret `SMITHERY_API_KEY`, an API key from your Smithery account. To publish by hand, run `npm run publish:smithery` after the release workflow finishes. It needs `zip` and a one-time `npx @smithery/cli auth login`; `--dry-run` builds the bundle without publishing.

Never move or recreate a published tag: the launcher caches binaries by release and checksum.

SDK contract and examples are documented in [anna/README.md](anna/README.md). Application code imports only public `anna`; SDK production and test code cannot import repository application packages. `npm run check:sdk` enforces both rules and checks independent consumption.
