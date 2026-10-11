# Changelog

Release dates are GitHub publication dates in UTC. Versions up to v0.0.15 were published from an earlier repository that was forked from iosifache/annas-mcp. v0.0.16 restarted the project from a fresh history once all of that code had been rewritten, so those earlier tags are not available here.

## [v0.6.1](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.6.1) - 2026-10-11

- Reuse a file in the download folder when its content matches the requested MD5, returning its existing path and byte count without downloading it again. DOI downloads resolve the record first; concurrent requests for the same hash share the first download. When no match exists, save the new download with the next unused filename, with no fixed collision limit.

## [v0.6.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.6.0) - 2026-10-11

- `book_search` and `metadata_search` results get an `edition` field. A result's publisher line reads "publisher, edition", and the whole line used to be returned as the publisher, so a record without a publisher reported "2019" or "2nd ed" as one. `publisher` now holds the publisher alone. Articles still report the whole line as `journal`.
- Fix DOI lookup accepting a record that names a different paper's DOI whenever its title and authors matched.
- Clean up text from publishers' records. Titles, authors and descriptions that were HTML-escaped twice ("Trengove &amp; coll") are decoded. Formatting tags such as `<i>`, `<p>` and Crossref's `jats:` tags are removed; text such as "AT&T" or "Vector<T>" is left alone. Right-to-left marks in Hebrew titles and publishers no longer show up as escape codes; joiners that shape Bengali and Hindi letters are kept.
- Drop more affiliations from author lists: ones after a short note such as "(Pandha, Hardev)", blocks holding an e-mail address, text after "Affiliation:", Scopus-style "Hamel, R.. Alzheimer Centre Limburg, …" lists and short affiliations in full-width parentheses. Roles, notes and corporate authors stay.
- DOI lookup folds the accents of any Latin letter when comparing author names, so "Gülşen Özdemir-Ağaoğlu" matches "Gulsen Ozdemir-Agaoglu"; marks that tell letters apart in other scripts are kept. A pasted DOI with repeated prefixes, such as `https://doi.org/doi:10.1000/x`, is accepted.
- A paper found through SciDB reports its year, size, source libraries and `fast_download` from its record page, with the whole publisher line as `journal`, the way search results do. The journal used to come out as a year or the publisher alone, and the size was missing.
- A page past the last result reports no more pages and a total of at least 0, instead of a total guessed from the page number.
- `ANNAS_BASE_URL` refuses a language version of an official mirror, such as `es.annas-archive.gl`, with a `[CONFIG]` error naming the mirror to use. The server reads Anna's pages in English.
- Read Anna's pages through the hooks its own scripts use instead of styling classes, so a restyling no longer breaks search results or record pages. When part of a page can't be read, the server logs one short warning, without search terms or account details, and returns what it read.
- For contributors: the parsers moved to `internal/anna/parse` and DOI matching to `internal/anna/match`. Their tests run on pages captured from the live archive (including searches in ten languages besides English), on about 11,600 strings from real search cards and as fuzz tests. `npm run check:upstream` and a weekly workflow flag changes to Anna's Archive's source that the parsers depend on. Test data moved out of Go source into files.

## [v0.5.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.5.0) - 2026-10-11

- Pick mirrors without SLUM (open-slum.org). The server checks the official mirrors itself, all at once on Anna's small `/dyn/up/` endpoint, and keeps using one for as long as it works. `ANNAS_BASE_URL` is now the mirror tried first, with the official mirrors standing in for it while it fails; before, it was only used, unchecked, when automatic selection failed.
- Retry a call once on another mirror when its mirror fails: it can't be reached, doesn't answer within 30 seconds, returns a page that isn't Anna's Archive or answers with a 5xx or 429. Previously the call failed and only the next call moved on. A failing mirror is set aside for 15 seconds, twice as long with each further failure in a row, up to 5 minutes.
- When every mirror is failing, calls fail at once with what each mirror last did and when to try again, instead of waiting on each one. The mirrors are checked again at most every 30 seconds, so the server recovers soon after the archive or your connection comes back.
- Contact the mirrors only while a tool call is running, never in the background. Mirror health is saved as `mirrors.json` in the cache folder, so a new run, a CLI command or another MCP client uses a mirror that worked in the last 10 minutes without checking it, and skips one that just failed.
- A `403` that a fresh session doesn't fix counts against a mirror only when another mirror then works. When two mirrors refuse, the error says the account lacks access, instead of every mirror being reported down.
- Limit how long one request can wait: 30 seconds for a response and 60 seconds for each pause in a download, separately from `timeout_seconds`. A mirror that went quiet used to hold a search for the whole minute, and a stalled download for 30 minutes.
- Continue a download that drops or stalls partway where it stopped, with up to two `Range` requests per server, instead of starting over on the next server. The MD5 check still covers the whole file.
- Sign-in gives each mirror 15 seconds and skips mirrors that are set aside, so one that hangs no longer uses up the call. A sign-in that failed only because no mirror could be reached is no longer remembered for a minute.
- Speed up DOI lookups. The SciDB page is requested in its final form instead of through up to four slow redirects, and a SciDB miss no longer follows on to a search. When SciDB misses, the main and journal indexes are searched at once, the lookup stops after the first query that finds the paper and record pages are read four at a time, returning as soon as the paper is found. If DOI.org doesn't answer within 10 seconds, the lookup goes on without it. "Attention Is All You Need" by arXiv DOI went from 24 to 14 seconds.
- Find arXiv papers whose archive record differs from DOI.org only in form, such as "Adam: A Method for Stochastic Optimization", which came back `[NOT_FOUND]` although the archive has its ICLR version. Placeholder DOIs (`10.0000/…`) on records without one no longer count as a conflicting DOI, and "Family, Given; Family, Given" author lists are read correctly. Authors are compared by family name and first initial, and for arXiv DOIs a record from the year after the preprint matches. A record with a different real DOI is still rejected, and `[NOT_FOUND]` now suggests searching by title to find another version, such as the published one.
- Report a search page whose results can't be read as `[UPSTREAM]` instead of returning no results, so a change in Anna's page layout isn't mistaken for the work being absent.
- When a call's retry on a second mirror fails too, the error says what both mirrors answered. The fast download API's `403`, which is about the account, no longer counts against the mirror.
- Move tool details, the cache folder and the saved session from the README to HOW-IT-WORKS.md, which also explains how mirrors are chosen. It ships in the release archives and the MCP Bundle.

## [v0.4.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.4.0) - 2026-10-11

- Add filters to `book_search` and `article_search`: `format` (file extension), `source` (source library), `sort` (by publication year, file size or date added, or random), phrase searches on `title`, `author`, `publisher` and `year`, `downloadable_only` and `search_descriptions`. `format`, `language`, `content` and `source` take comma-separated lists, and a leading `-` excludes a value, as in `-pdf`. `query` can be left out when one of the field searches is set. An unknown `source` is rejected with the valid ones, read from the archive's own search page; Anna's Archive itself answers it with unrelated files.
- Search results include each file's year, content type, source libraries, every language the record lists, `fast_download` (whether the download tools can fetch it) and `possible_issues` (the archive's warning that the file may be broken). Each page reports `total` and `more_pages`.
- Fix search results that mixed in Anna's looser "partial matches", which ignore filters. A filtered search could return files in other formats, and a search with an unknown filter value returned only unrelated files. They now come back separately as `partial_matches`, which only fill the room the exact results leave within `limit`.
- Add `book_details`, which returns the full record behind a hash, a catalog id or a record URL: ISBN, DOI, OCLC, Open Library and other identifiers, the edition, the full description, subjects and classifications, alternative titles and authors, problems that source libraries reported for the file (with a better copy's hash when the archive knows one), and download and quality counts.
- Add `metadata_search`, which searches library catalogs such as Open Library, WorldCat, ISBNdb, Goodreads and Google Books, including books with no downloadable file, to find editions and ISBNs.
- Add `account_status`, which shows the active memberships and how many fast downloads are left in the rolling 18- or 24-hour window. Downloads report `downloads_left` too.
- When every fast server fails for a file that is stored in more than one collection, try up to two more collections. Only the first request for a file counts against the allowance, so this costs time, not downloads. A failing download API is still tried once per server, not again for each collection.
- Stop at the first fast download API answer that every other server would repeat: no fast downloads left (now an `[UPSTREAM_BLOCKED]` error instead of three HTTP 429 attempts), a rejected secret key (`[CONFIG]`), an account without a membership or a hash the archive does not know (`[NOT_FOUND]`). `article_download` by DOI still falls back to SciDB when fast downloads run out.
- The CLI gains `metadata-search`, `book-details` and `account-status`, flags for every new search filter, a line with the total and the next page after search results, and the fast downloads left after a download.
- Add an opt-in live test of search, catalog, details and account parsing that downloads nothing (`ANNAS_MCP_TEST_LIVE=1`). The live smoke test and release verification check all seven tools.

## [v0.3.1](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.3.1) - 2026-10-11

- A DOI lookup in `article_search` or `article_download` no longer reports `[NOT_FOUND]` when Anna's Archive could not be checked. If SciDB, a search or a record page fails, for example with a 502 while the archive is down, the error reports that failure as `[UPSTREAM]`, `[UPSTREAM_BLOCKED]` or `[REQUEST_TIMEOUT]`. `[NOT_FOUND]` now means every request was answered and none of them found the paper.
- Automatic mirror selection probes every mirror at once on Anna's `/dyn/up/` endpoint and uses the first one that answers, instead of running a full search on each mirror in turn. A fresh start now selects a mirror in under a second, where a slow minute could previously spend the whole 15-second selection budget. While every mirror is failing, a single call no longer selects again on each request.

## [v0.3.0](https://github.com/SokolskyNikita/annas-mcp/releases/tag/v0.3.0) - 2026-10-11

- Save downloads to `annas-archive` in the home folder (`~/annas-archive`, `%USERPROFILE%\annas-archive` on Windows) when `ANNAS_DOWNLOAD_PATH` is unset, so the secret key is the only setting a client needs. The folder is created on the first download, and unlike Desktop, Documents and Downloads it needs no macOS privacy permission. A leading `~` in `ANNAS_DOWNLOAD_PATH` now stands for the home folder. The MCP Bundle's download folder defaults to the same folder instead of Downloads.
- Check that the download folder can be written before contacting any download server. Previously an unwritable folder failed only after the file had been requested from each fast server. The `[CONFIG]` error names the folder, and on macOS explains how to grant a protected folder's permission or pick an unprotected one.
- The MCP instructions ask the client to tell the user where a download was saved.
- Add a `Dockerfile` for `docker run -i` and Docker's MCP Catalog. CI builds the image and checks its MCP handshake.
- Add a project site at https://sokolskynikita.github.io/annas-mcp/, `llms-install.md` for agents that install MCP servers and a README section on switching from iosifache/annas-mcp.
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
- Remove numbered affiliation text (for example `;1 FAIR - Facebook AI Research …`) from author fields in book search, article/DOI lookup and DOI identity matching, and normalize the spacing around commas.

## v0.0.14 - 2026-09-24

- Replace raw HTML download errors with server hostnames, HTTP statuses and short explanations. Keep every fast-server failure and the SciDB fallback outcome in the error, with their underlying causes. HTTP and transport diagnostics leave out signed URLs.
- Take a failed download's error code from its final attempt. Cancelling the operation still takes precedence, and earlier causes stay in the error.

## v0.0.13 - 2026-09-23

- Verify a published release through its GitHub release ID when the tag endpoint omits uploaded assets.

## v0.0.12 - 2026-09-23

- Verify DOI search candidates against their own record identifiers or matching citation metadata. Reject conflicting identifiers and keep the punctuation in DOI suffixes.
- Resolve SciDB fallback PDFs from the article page's PDF.js viewer and check their PDF signature and archive hash. Account cookies are never sent to external file hosts. Add an opt-in live test with the fast API disabled.
- Restrict automatic mirror discovery to verified official domains before sending credentials; keep explicitly configured base URLs.
- Correct membership documentation: Brilliant Bookworm includes JSON API access, while Lucky Librarian's browser-check exemption applies to normal browser use.
- All eight archives and their checksums were published, but post-publication verification failed on inconsistent asset metadata from GitHub's tag endpoint. v0.0.13 fixes that.

## v0.0.11 - 2026-09-23

- Fixed Windows launcher installation under Git Bash by selecting the system's native `tar.exe`. Git Bash's GNU tar misinterprets Windows drive paths and does not support the required ZIP extraction.
- Ran launcher integration tests under Bash on all CI platforms to cover Windows shell differences before publication.

## v0.0.10 - 2026-09-23

- Split the archive client, mirror discovery, shared MCP/CLI services and npm launcher into smaller modules. Removed obsolete helpers, configuration wrappers and unused dependencies.
- Renamed the Go module to `github.com/SokolskyNikita/annas-mcp`, updated the MCP SDK to v1.8.0 and moved to the Go 1.26 toolchain.
- Required an account cookie for archive requests and an API key for every download, including DOI downloads, with missing configuration reported before network access.
- Added typed application errors, consistent argument validation and operation-wide cancellation/timeouts covering retries, with a 24-hour maximum.
- Reworked DOI URL normalization, exact DOI/title matching, SciDB query preservation, search-card parsing, cookie validation and credential redaction.
- Made mirror discovery lazy and bounded, cached successful selections and fallbacks, invalidated failed mirrors and tightened origin/redirect checks.
- Checked downloads for empty files and content-length mismatches, tightened HTML detection, wrote each download to a temporary file before moving it into place, handled name collisions and made filename sanitization portable.
- Tightened the launcher's archive-entry validation, cached-binary verification, concurrent installation and offline fallback. Reworked its Windows support and subprocess lifecycle handling.
- Moved HTML and other test data into external fixtures and added race checks and an 80% total Go coverage gate. Added an opt-in live smoke test for all four MCP tools and made health checks local by default.
- Expanded CI to Linux, macOS and Windows, with workflow linting and snapshot builds of all eight release targets. Gated release publication on passing checks and added post-publish checks of the assets, launcher version and MCP startup. The Windows check exposed the Git Bash extraction issue fixed in v0.0.11.
- Corrected release version embedding, synchronized package/tag metadata and restricted the tag script to new annotated tags from a clean checkout matching `origin/main`.
- Replaced real bibliographic examples in the README with fictional placeholders, and rewrote its sections on membership levels, API keys, weekly cookie renewal, tool contracts and release procedures.

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
- Rewrote the setup and tool documentation, described download membership requirements and removed stale screenshots.
- Added an MIT `LICENSE` file and README license section; these were removed again in v0.0.8.

## v0.0.6 - 2026-09-22

- Added `ANNAS_ACCOUNT_COOKIE` support for authenticated archive requests and mirror probes. It accepted raw cookie values or browser cookie headers and forwarded only `aa_account_id2`. Documented account-session setup for DDoS-Guard-protected pages.
- Added the Node/npm launcher and `npx -y github:SokolskyNikita/annas-mcp` installation path. The launcher selected the platform's GitHub release archive, extracted and cached the executable, started MCP by default and forwarded CLI arguments.
- Added launcher tests, npm package metadata, fork-specific setup instructions and the GoReleaser project name for the fork's first release.

[All changes since the fork baseline](https://github.com/SokolskyNikita/annas-mcp/compare/c095963ad5d10d33da3c614495246b6ec7f680d4...v0.0.6)

## Fork baseline - 2026-09-22

The fork was created from [iosifache/annas-mcp](https://github.com/iosifache/annas-mcp) at commit [c095963](https://github.com/iosifache/annas-mcp/commit/c095963ad5d10d33da3c614495246b6ec7f680d4). It inherited the MCP server, CLI, book/article operations, DOI/SciDB support, configurable mirrors/timeouts and opt-in SLUM mirror discovery. The first fork-specific commit, [3590412](https://github.com/SokolskyNikita/annas-mcp/commit/3590412d134eae331c974b3798f9db9105643bca), added account-cookie support.
