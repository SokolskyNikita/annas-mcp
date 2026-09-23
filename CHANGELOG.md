# Changelog

Changes made in this fork, reconstructed from Git history and grouped by the first release containing them. Release dates are GitHub publication dates in UTC.

## [v0.0.12](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.12) — 2026-09-23

- Verify DOI search candidates against their own record identifiers or matching citation metadata, reject conflicting identifiers, and preserve DOI suffix punctuation.
- Resolve SciDB fallback PDFs from the article page's PDF.js viewer, verify the PDF signature and archive hash, and keep account cookies off external file hosts. Added an opt-in live test with the fast API deliberately disabled.
- Restrict automatic mirror discovery to verified official domains before sending credentials; preserve explicitly configured base URLs.
- Correct membership documentation: Brilliant Bookworm includes JSON API access, while Lucky Librarian's browser-check exemption applies to normal browser use.

[All changes since v0.0.11](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.11...v0.0.12)

## [v0.0.11](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.11) — 2026-09-23

- Fixed Windows launcher installation under Git Bash by selecting the system's native `tar.exe`. Git Bash's GNU tar misinterprets Windows drive paths and does not support the required ZIP extraction.
- Ran launcher integration tests under Bash on all CI platforms to cover Windows shell differences before publication.
- Completed published-asset, launcher-version, and MCP-startup verification successfully on Linux, macOS, and Windows.

[All changes since v0.0.10](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.10...v0.0.11)

## [v0.0.10](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.10) — 2026-09-23

- Refactored the archive client, mirror discovery, shared MCP/CLI services, and npm launcher into focused modules; removed obsolete helpers, configuration wrappers, and unused dependencies.
- Migrated the Go module to `github.com/SokolskyNikita/annas-mcp`, updated the MCP SDK to v1.8.0, and moved to the Go 1.26 toolchain.
- Required an account cookie for archive requests and an API key for every download, including DOI downloads, with missing configuration reported before network access.
- Added typed application errors, consistent argument validation, and operation-wide cancellation/timeouts covering retries, with a 24-hour maximum.
- Improved DOI URL normalization, exact DOI/title matching, SciDB query preservation, search-card parsing, cookie validation, and credential redaction.
- Made mirror discovery lazy and bounded, cached successful selections and fallbacks, invalidated failed mirrors, and tightened origin/redirect checks.
- Hardened download integrity with empty-file and content-length checks, safer HTML detection, temporary-file publication, collision handling, and portable filename sanitization.
- Hardened launcher archive-entry validation, cached-binary verification, concurrent installation, and offline fallback; improved Windows compatibility and subprocess lifecycle handling.
- Moved HTML and other test data into external fixtures; expanded regression, CLI, MCP, and launcher coverage; added race checks and an 80% total Go coverage gate. Added an opt-in live smoke test for all four MCP tools, while making health checks local by default.
- Expanded CI to Linux, macOS, and Windows, with workflow linting and all eight release targets built as snapshots. Gated release publication on passing checks and added post-publish asset, launcher-version, and MCP-startup verification. The Windows verification exposed the Git Bash extraction issue fixed in v0.0.11.
- Corrected release version embedding, synchronized package/tag metadata, and restricted the tag script to new annotated tags from a clean checkout matching `origin/main`.
- Reorganized and deduplicated the README, replaced real bibliographic examples with fictional placeholders, and clarified membership levels, API keys, weekly cookie renewal, tool contracts, and release procedures.

[All changes since v0.0.9](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.9...v0.0.10)

## [v0.0.9](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.9) — 2026-09-23

- Added `limit` to MCP searches, defaulting to ten results, with `matched` and `limit` metadata and guidance to increase the limit before advancing pages.
- Added CLI `--limit`; the CLI continued to print the full upstream page by default.
- Returned MCP success payloads as both JSON text and `structuredContent`, and introduced stable error prefixes such as `[INVALID_ARGUMENT]`, `[NOT_FOUND]`, and `[UPSTREAM_BLOCKED]`.
- Added a read-only GitHub Actions test workflow for `main` pushes and pull requests, initially running Go and launcher tests on Linux, plus a README test-status badge.

[All changes since v0.0.8](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.8...v0.0.9)

## [v0.0.8](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.8) — 2026-09-23

- Corrected search filters to use the journals index for articles and omit the obsolete `book_any` filter, fixing empty later result pages.
- Included descriptions and discovered DOIs in search results, and improved result metadata parsing.
- Improved DOI lookup through SciDB redirects, DOI searches, and title-metadata fallback when a direct file was unavailable.
- Added article downloads by search-result hash, with optional title and format overrides.
- Changed the npm launcher to check for the latest release before starting, falling back to a cached binary when the update check failed.
- Documented weekly cookie expiration and manual retrieval through browser developer tools.
- Removed the MIT `LICENSE` file and README license section that had been added in v0.0.7.

[All changes since v0.0.7](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.7...v0.0.8)

## [v0.0.7](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.7) — 2026-09-22

- Reworked archive requests and HTML parsing to propagate contexts and cancellation, handle blocked responses, and expose content, language, and page options through MCP and CLI searches.
- Standardized MCP success responses as JSON text and downloads as path/byte-count results; added download progress notifications.
- Added download-server retries, MD5 verification when a hash was known, an 8 GiB size limit, safer filenames, and collision handling that preserved existing files. Fixed the size-limit constant for 32-bit builds.
- Enabled the inherited automatic mirror discovery by default, added parsing/ranking for SLUM's directory status badges, and retained configured-mirror fallback.
- Added SHA-256 verification of release archives and versioned launcher caches, with cached startup and background update checks.
- Expanded archive, mirror, MCP, and launcher tests; rewrote setup/tool documentation, described download membership requirements, and removed stale screenshots.
- Added an MIT `LICENSE` file and README license section; these were subsequently removed in v0.0.8.

[All changes since v0.0.6](https://github.com/SokolskyNikita/annas-mcp/compare/v0.0.6...v0.0.7)

## [v0.0.6](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.0.6) — 2026-09-22

- Added `ANNAS_ACCOUNT_COOKIE` support for authenticated archive requests and mirror probes. Accepted raw cookie values or browser cookie headers, forwarding only `aa_account_id2`, and documented account-session setup for DDoS-Guard-protected pages.
- Added the Node/npm launcher and `npx -y github:SokolskyNikita/annas-mcp` installation path. The launcher selected the platform's GitHub release archive, extracted and cached the executable, started MCP by default, and forwarded CLI arguments.
- Added launcher tests, npm package metadata, fork-specific setup instructions, and the GoReleaser project name for the fork's first release.

[All changes since the fork baseline](https://github.com/SokolskyNikita/annas-mcp/compare/c095963ad5d10d33da3c614495246b6ec7f680d4...v0.0.6)

## Fork baseline — 2026-09-22

The fork was created from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) at commit [c095963](https://github.com/iosifache/annas-mcp/commit/c095963ad5d10d33da3c614495246b6ec7f680d4). It inherited the MCP server, CLI, book/article operations, DOI/SciDB support, configurable mirrors/timeouts, and opt-in SLUM mirror discovery. The first fork-specific commit was [3590412](https://github.com/SokolskyNikita/annas-mcp/commit/3590412d134eae331c974b3798f9db9105643bca), adding account-cookie support.

Earlier commits and tags, including upstream's `v0.1` tag, belong to the [upstream history](https://github.com/iosifache/annas-mcp/commits/c095963ad5d10d33da3c614495246b6ec7f680d4/).
