# Changelog

Release dates are GitHub publication dates in UTC. Versions up to v0.0.15 were published from an earlier repository that was forked from iosifache/annas-mcp. v0.0.16 restarted the project from a fresh history once all of that code had been rewritten, so those earlier tags are not available here.

## [v0.3.1](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.3.1) - 2026-10-11

- A DOI lookup in `article_search` or `article_download` no longer reports `[NOT_FOUND]` when Anna's Archive could not be checked. If SciDB, a search or a record page fails, for example with a 502 while the archive is down, the error reports that failure as `[UPSTREAM]`, `[UPSTREAM_BLOCKED]` or `[REQUEST_TIMEOUT]`. `[NOT_FOUND]` now means every request was answered and none of them found the paper.
- Automatic mirror selection probes every mirror at once on Anna's `/dyn/up/` endpoint and uses the first one that answers, instead of running a full search on each mirror in turn. A fresh start now selects a mirror in under a second, where a slow minute could previously spend the whole 15-second selection budget. While every mirror is failing, a single call no longer selects again on each request.

## [v0.3.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.3.0) - 2026-10-11

- Save downloads to `annas-archive` in the home folder (`~/annas-archive`, `%USERPROFILE%\annas-archive` on Windows) when `ANNAS_DOWNLOAD_PATH` is unset, so the secret key is the only setting a client needs. The folder is created on the first download, and unlike Desktop, Documents and Downloads it needs no macOS privacy permission. A leading `~` in `ANNAS_DOWNLOAD_PATH` now stands for the home folder. The MCP Bundle's download folder defaults to the same folder instead of Downloads.
- Check that the download folder can be written before contacting any download server. Previously an unwritable folder failed only after the file had been requested from each fast server. The `[CONFIG]` error names the folder, and on macOS explains how to grant a protected folder's permission or pick an unprotected one.
- The MCP instructions ask the client to tell the user where a download was saved.
- Add a `Dockerfile` for `docker run -i` and Docker's MCP Catalog. CI builds the image and checks its MCP handshake.
- Add a project site at https://sokolskynikita.github.io/annas-mcp/, `llms-install.md` for agents that install MCP servers, and a README section on switching from iosifache/annas-mcp.
- The release workflow publishes the MCP Bundle to Smithery. `npm run bump` sets the version in every file and adds the CHANGELOG heading.

## [v0.2.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.2.0) - 2026-10-09

- Sign in to Anna's Archive with `ANNAS_SECRET_KEY`, which is the account's login key as well as its API key, so the weekly `aa_account_id2` cookie no longer has to be copied from a browser. The server signs in before its first archive request. When the archive answers 403 to an expired session, it signs in again and retries the request once. Concurrent requests share one sign-in. With automatic mirror selection on, sign-in falls back to the other official mirrors.
- Save the signed-in session in `session.json` in the cache folder `npx` already uses (`ANNAS_MCP_CACHE_DIR` moves both), so restarts and new `npx` launches don't sign in again. Only its owner can read the file. A SHA-256 fingerprint ties it to the secret key, and it records which `ANNAS_ACCOUNT_COOKIE` the session replaced, so an expired configured cookie is skipped until a different one is set.
- `ANNAS_SECRET_KEY` is now the one required credential. `ANNAS_ACCOUNT_COOKIE` is optional: it is used first when set and replaced by a fresh sign-in once it expires.
- When signing in fails, the error names what each mirror answered and gives the steps for copying `aa_account_id2` into `ANNAS_ACCOUNT_COOKIE` by hand. The MCP instructions ask the client to pass those steps on. A rejected secret key is reported as `[CONFIG]`, and a 403 that a fresh session does not fix points at the membership tier.
- The live smoke test now ignores `ANNAS_ACCOUNT_COOKIE` and saves the session in a temporary folder, so it always signs in with `ANNAS_SECRET_KEY` from scratch. The SciDB live test no longer needs a cookie.

## [v0.1.2](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.1.2) - 2026-09-29

- Publish `.tar.gz` archives for macOS, Linux and FreeBSD, and have the launcher download them. The launcher no longer needs `xz`, which minimal systems such as slim Docker images lack. `.tar.xz` archives are still published for older launchers, and the launcher falls back to them for releases without `.tar.gz`.
- Fix a race on Windows where two launchers starting at once could fail with `EPERM` while recording the cached binary.
- Add an icon to the MCP Bundle, the MCP Registry entry and the server's `initialize` response, which also reports the project website.
- Add `npm run publish:smithery`, which publishes the release's MCP Bundle to Smithery with the tool schemas Smithery requires.

## [v0.1.1](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.1.1) - 2026-09-29

- Publish the launcher to npm as `annas-mcp` and list the server in the MCP Registry as `io.github.SokolskyNikita/annas-mcp`. `npx -y github:SokolskyNikita/annas-mcp` keeps working.
- Attach `annas-mcp.mcpb` to each release: an MCP Bundle for one-click installs in Claude Desktop and for Smithery, which asks for the cookie, API key and download folder.
- Give every MCP tool a human-readable title. Add `glama.json` and a Privacy section to the README for directory listings.
- `npm run check:version` also checks the versions in `server.json` and `manifest.json`.

## [v0.1.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.1.0) - 2026-09-28

- Stop returning the SciDB PDF link as `download_url` in `article_search` results and CLI output. It is a signed link issued to the member session that works for anyone who has it until it expires. `article_download` still resolves it internally.
- Remove affiliation blocks that follow the last author in parentheses (for example `Lin Ning(Innovative Institute of …;Department of …)`), and normalize the spacing of `;`-separated author lists.
- Correct the membership requirements in the README: searches need Lucky Librarian or higher, and downloads work with any tier.

## [v0.0.16](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.16) - 2026-09-28

- Restart the repository from a fresh history under the MIT license. Credit iosifache/annas-mcp for the original idea; none of its code remains.
- Rewrite the mirror resolver around separate discovery and fallback steps, with the same selection, probing, timeout and fallback behavior. Consolidate the release configuration into a single build with identical archives, and include LICENSE in every archive.
- Book search results now list `title` first in JSON output. CLI text output is unchanged.

## v0.0.15 - 2026-09-28

- Stop after the first fast-download API response when Anna's Archive reports that a file has no fast download copy (`Invalid domain_index or path_index`). Return `NOT_FOUND` with advice to pick another copy, instead of three identical upstream failures. DOI downloads still fall back to SciDB.
- Infer the saved file's extension from the download URL when the file server sends a bare `attachment` disposition and a generic `application/octet-stream` type. This fixes files such as AZW3 books being saved as `.bin` when `format` is omitted.
- Remove numbered affiliation text (for example `;1 FAIR - Facebook AI Research …`) from author fields in book search, article/DOI lookup and DOI identity matching, and normalize the spacing around commas. Add regression tests for all three changes.

## v0.0.14 - 2026-09-24

- Replace raw HTML download errors with server hostnames, HTTP statuses and short explanations. Keep every fast-server failure and the SciDB fallback outcome in the error, with their underlying causes. HTTP and transport diagnostics leave out signed URLs.
- Take a failed download's error code from its final attempt. Cancelling the operation still takes precedence, and earlier causes stay in the error. Add regression tests for mixed failures, credential redaction, cancellation and MCP tool error output.

## v0.0.13 - 2026-09-23

- Verify a published release through its GitHub release ID when the tag endpoint omits uploaded assets. The archive, checksum, version and MCP checks all still run. Add regression tests for inconsistent metadata and mismatched release identities.

## v0.0.12 - 2026-09-23

- Verify DOI search candidates against their own record identifiers or matching citation metadata. Reject conflicting identifiers and keep the punctuation in DOI suffixes.
- Resolve SciDB fallback PDFs from the article page's PDF.js viewer and check their PDF signature and archive hash. Account cookies are never sent to external file hosts. Add an opt-in live test with the fast API disabled.
- Restrict automatic mirror discovery to verified official domains before sending credentials; keep explicitly configured base URLs.
- Correct membership documentation: Brilliant Bookworm includes JSON API access, while Lucky Librarian's browser-check exemption applies to normal browser use.
- All eight archives and their checksums were published, but post-publication verification failed on inconsistent asset metadata from GitHub's tag endpoint. v0.0.13 fixes that.

## v0.0.11 - 2026-09-23

- Fixed Windows launcher installation under Git Bash by selecting the system's native `tar.exe`. Git Bash's GNU tar misinterprets Windows drive paths and does not support the required ZIP extraction.
- Ran launcher integration tests under Bash on all CI platforms to cover Windows shell differences before publication.
- The published assets, launcher version and MCP startup passed verification on Linux, macOS and Windows.

## v0.0.10 - 2026-09-23

- Split the archive client, mirror discovery, shared MCP/CLI services and npm launcher into smaller modules. Removed obsolete helpers, configuration wrappers and unused dependencies.
- Renamed the Go module to `github.com/SokolskyNikita/annas-mcp`, updated the MCP SDK to v1.8.0 and moved to the Go 1.26 toolchain.
- Required an account cookie for archive requests and an API key for every download, including DOI downloads, with missing configuration reported before network access.
- Added typed application errors, consistent argument validation and operation-wide cancellation/timeouts covering retries, with a 24-hour maximum.
- Reworked DOI URL normalization, exact DOI/title matching, SciDB query preservation, search-card parsing, cookie validation and credential redaction.
- Made mirror discovery lazy and bounded, cached successful selections and fallbacks, invalidated failed mirrors and tightened origin/redirect checks.
- Checked downloads for empty files and content-length mismatches, tightened HTML detection, wrote each download to a temporary file before moving it into place, handled name collisions and made filename sanitization portable.
- Tightened the launcher's archive-entry validation, cached-binary verification, concurrent installation and offline fallback. Reworked its Windows support and subprocess lifecycle handling.
- Moved HTML and other test data into external fixtures. Expanded regression, CLI, MCP and launcher coverage, and added race checks and an 80% total Go coverage gate. Added an opt-in live smoke test for all four MCP tools and made health checks local by default.
- Expanded CI to Linux, macOS and Windows, with workflow linting and snapshot builds of all eight release targets. Gated release publication on passing checks and added post-publish checks of the assets, launcher version and MCP startup. The Windows check exposed the Git Bash extraction issue fixed in v0.0.11.
- Corrected release version embedding, synchronized package/tag metadata and restricted the tag script to new annotated tags from a clean checkout matching `origin/main`.
- Reorganized and deduplicated the README and replaced real bibliographic examples with fictional placeholders. Rewrote its explanations of membership levels, API keys, weekly cookie renewal, tool contracts and release procedures.

## v0.0.9 - 2026-09-23

- Added `limit` to MCP searches, defaulting to ten results, with `matched` and `limit` metadata and guidance to increase the limit before advancing pages.
- Added CLI `--limit`; the CLI continued to print the full upstream page by default.
- Returned MCP success payloads as both JSON text and `structuredContent`, and introduced stable error prefixes such as `[INVALID_ARGUMENT]`, `[NOT_FOUND]` and `[UPSTREAM_BLOCKED]`.
- Added a read-only GitHub Actions test workflow for `main` pushes and pull requests, initially running Go and launcher tests on Linux, plus a README test-status badge.

## v0.0.8 - 2026-09-23

- Corrected search filters to use the journals index for articles and omit the obsolete `book_any` filter, fixing empty later result pages.
- Included descriptions and discovered DOIs in search results, and reworked result metadata parsing.
- Made DOI lookup follow SciDB redirects and, when no direct file was available, fall back to DOI searches and then title metadata.
- Added article downloads by search-result hash, with optional title and format overrides.
- Changed the npm launcher to check for the latest release before starting, falling back to a cached binary when the update check failed.
- Documented weekly cookie expiration and manual retrieval through browser developer tools.
- Removed the MIT `LICENSE` file and README license section that had been added in v0.0.7.

## v0.0.7 - 2026-09-22

- Reworked archive requests and HTML parsing to propagate contexts and cancellation and to handle blocked responses. MCP and CLI searches gained content, language and page options.
- Standardized MCP success responses as JSON text and downloads as path/byte-count results; added download progress notifications.
- Added download-server retries, MD5 verification when a hash was known, an 8 GiB size limit, safer filenames and collision handling that preserved existing files. Fixed the size-limit constant for 32-bit builds.
- Enabled the inherited automatic mirror discovery by default, added parsing/ranking for SLUM's directory status badges and kept the configured mirror as a fallback.
- Added SHA-256 verification of release archives and versioned launcher caches, with cached startup and background update checks.
- Expanded archive, mirror, MCP and launcher tests. Rewrote the setup and tool documentation, described download membership requirements and removed stale screenshots.
- Added an MIT `LICENSE` file and README license section; these were removed again in v0.0.8.

## v0.0.6 - 2026-09-22

- Added `ANNAS_ACCOUNT_COOKIE` support for authenticated archive requests and mirror probes. It accepted raw cookie values or browser cookie headers and forwarded only `aa_account_id2`. Documented account-session setup for DDoS-Guard-protected pages.
- Added the Node/npm launcher and `npx -y github:SokolskyNikita/annas-mcp` installation path. The launcher selected the platform's GitHub release archive, extracted and cached the executable, started MCP by default and forwarded CLI arguments.
- Added launcher tests, npm package metadata, fork-specific setup instructions and the GoReleaser project name for the fork's first release.

[All changes since the fork baseline](https://github.com/SokolskyNikita/annas-mcp/compare/c095963ad5d10d33da3c614495246b6ec7f680d4...v0.0.6)

## Fork baseline - 2026-09-22

The fork was created from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) at commit [c095963](https://github.com/iosifache/annas-mcp/commit/c095963ad5d10d33da3c614495246b6ec7f680d4). It inherited the MCP server, CLI, book/article operations, DOI/SciDB support, configurable mirrors/timeouts and opt-in SLUM mirror discovery. The first fork-specific commit, [3590412](https://github.com/SokolskyNikita/annas-mcp/commit/3590412d134eae331c974b3798f9db9105643bca), added account-cookie support.
