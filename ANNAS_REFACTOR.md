# Plan: Extract a self-contained Anna's Archive SDK into `anna/`

Status: implemented and locally verified on October 12, 2026. The original design below is retained as the migration record; historical source paths describe the pre-refactor layout.

## Execution record

- Baseline: only this plan was untracked; no unrelated source changes were present. Offline race/coverage passed at 91.1% overall and 98.8% each for parser and matcher. Launcher tests passed with local loopback access. Initial attempts encountered sandbox cache/network restrictions, resolved using a writable Go cache and authorized local test execution.
- Implemented one public `anna` SDK with `NewClient(Config) (*Client, error)`, shared domain validation, public errors, injected logging, client-owned unread diagnostics, and SDK-owned fixtures and maintenance tools. The MCP/CLI use public `anna`; rendering, environment defaults, platform advice and interface limits remain in application adapters.
- `CodeConfig` names the existing `CONFIG` category because `Config` is now the public configuration type. Provided nonempty download/session paths must be absolute; empty settings remain valid until needed. Application timeout defaults moved to the adapters. HTTP client settings are copied; transports remain shared and caller-owned.
- All 186 moved fixture files remain byte-for-byte identical. The reviewed upstream commit is unchanged. Initial extraction validation used offline tests only; the explicitly requested live follow-up is recorded below. No fixtures were refreshed, upstream commits accepted, or releases published.
- Added public-package tests, runnable opt-in examples, `anna/README.md`, and an offline temporary external-module consumer. `npm run check:sdk` audits runtime and test imports and is included in `npm test`.
- Final local checks passed: formatting, `git diff --check`, `go vet ./...`, all offline Go tests with race detection, coverage gates (90.0% overall, parser 98.8%, matcher 98.8%), version checks, npm/unit/launcher/adapter checks, external consumer and dependency audit, MCP bundle validation/packing, package-content checks, GoReleaser configuration validation and the full unpublished snapshot build.
- Docker follow-up passed locally using Homebrew Docker and Colima outside the sandbox. The earlier claim that Docker was not installed was incorrect: its Homebrew binary was outside PATH and Colima was stopped. The updated image `annas-mcp:ci` built successfully for Linux/arm64, ran as nonroot UID 65532, passed the MCP handshake with version `v0.6.5` and all seven expected tools, and produced zero MCP/CLI stderr bytes. Runtime filesystem inspection confirmed the image contains no SDK Go source or test fixtures and that the download directory belongs to the nonroot user. The existing Docker CI job and Linux/macOS/Windows test matrix remain in place; cross-platform test execution remains CI’s responsibility.
- Release preparation: v0.7.0 packages the public SDK and applications together. Documentation and version metadata were updated for the existing commit, push, CI and annotated-tag release workflow.
- Requested live follow-up passed on October 12, 2026: six distinct books were searched, inspected and downloaded through MCP and through CLI (12 fresh downloads total). Every file independently matched its advertised MD5 and passed EPUB ZIP integrity checks. Two books per interface passed both changed-title and renamed-file reuse in fresh credentialless processes with an unusable origin (eight deduplication checks); file inode and modification time stayed unchanged. All 25 captured stderr logs were empty: zero errors and zero warnings. The opt-in `npm run test:live:books` harness reproduces this verification; its report records the tested binary SHA-256 and per-operation results.

## Objective

Put all code and supporting assets responsible for interacting with Anna's Archive in one `anna/` directory. Expose a public Go client that other projects can import, and make the existing MCP server and CLI consumers of that same API.

The SDK includes parsing, citation matching, authentication, session persistence, mirror selection, search, metadata retrieval, account status, statistics, and downloads. Its tests, upstream fixtures, capture tools, and upstream-change monitoring live with it.

Keep one repository and one Go module. The public import path will be:

```go
import "github.com/SokolskyNikita/annas-mcp/anna"
```

The existing module's release tags will version both the SDK and applications. Do not create a nested `go.mod`, a separate repository, or a second copy of the client.

## Scope and ownership

The boundary is responsibility, not whether a file mentions Anna's Archive. The SDK owns knowledge of the archive's protocol and behavior. The application owns how people and MCP clients invoke those capabilities.

| Responsibility | Owner after refactor |
| --- | --- |
| Archive endpoints, request construction, response interpretation | `anna/` |
| HTML/JSON parsing, text cleanup, page-layout diagnostics | `anna/internal/parse/` |
| DOI metadata retrieval, SciDB fallback, citation identity verification | `anna/` and `anna/internal/match/` |
| Trusted archive origins, mirror probes, health, cooldowns, failover | `anna/internal/mirror/` |
| Secret-key sign-in, cookie validation, renewal, session-file format | `anna/` |
| Search semantics, domain validation, pagination, identifiers | `anna/` |
| Download grants, transfer retries/resume, checksums, file reuse | `anna/` |
| Filesystem download mechanics and safe filename selection | `anna/` |
| Captured upstream pages, normalization corpus, golden outputs, fuzz seeds | Respective packages under `anna/` |
| Upstream source hooks and change-detection implementation | `anna/scripts/` and parser tests |
| Environment-variable reading, `.env`, home-directory defaults | Application configuration under `internal/env/` |
| MCP tools, schemas, instructions, result envelopes | `internal/modes/` |
| CLI flags, comma-separated input parsing, text rendering, exit handling | `internal/modes/` |
| Application diagnostics and environment-specific troubleshooting | Application adapters |
| npm launchers, binary distribution, Docker, MCP bundles, release metadata | Existing application locations |
| CI scheduling and repository-wide checks | `.github/` and root `scripts/`, invoking SDK tools where needed |

Application integration tests and their synthetic tool/CLI fixtures stay with the application. Archive behavior tests and captured upstream responses move into the SDK. Existing MCP smoke and CLI healthcheck scripts remain application checks; they must not become an alternative implementation of archive behavior.

## Target layout

```text
anna/
  README.md                     SDK usage and behavioral contracts
  doc.go                        Go package documentation
  client.go                     Public Client, Config, construction
  structs.go                    Public domain and result types
  errors.go                     Public error codes, type, classification
  search_options.go             Domain search normalization/validation
  client_*.go                   Search, HTTP, statistics, download operations
  details.go                    Record identifiers and details
  account.go                    Membership and download allowance
  session*.go                   Sign-in, renewal, persistence
  doi_lookup.go                 DOI resolution and fallback orchestration
  scidb.go                      SciDB handling
  download_*.go                 Transfer, verification, local reuse
  ...                           Other existing archive implementation files
  *_test.go                     Client tests, including public API tests
  testdata/                     Client fixtures and shared archive test cases
  examples/                     Small standalone consumer examples
  internal/
    parse/                      Stateless upstream response parsing
      testdata/                 Captured pages, corpus, golden files, fuzz seeds
    match/                      Citation identity checks
      testdata/                 Matching cases and fuzz seeds
    mirror/                     Origins, probes, selection, health, persistence
      testdata/                 Probe responses and mirror cases
  scripts/
    check-upstream.mjs          Upstream-change detection and reporting
    check-upstream.test.mjs     Offline tests for that tooling
    upstream-watch.json        Watched source paths, hooks, reviewed revision

cmd/annas-mcp/                   Existing executable entry point
internal/modes/                 MCP/CLI adapters and output rendering
internal/env/                   Application configuration
internal/logger/                Application logging, if still needed
internal/version/               Application version
internal/testpolicy/            Repository-wide test-data policy
scripts/                        Release, packaging, integration, coverage checks
.github/workflows/              Repository CI and scheduled upstream checks
go.mod                          Existing shared module
go.sum
```

This is an ownership map, not a requirement to rename or combine every implementation file. Preserve recognizable file boundaries during the move. Keep fixtures beside their owning package instead of flattening every fixture into a single directory.

## Dependency rules

The intended dependency direction is:

```text
MCP / CLI / application configuration
                  |
                  v
             public anna
                  |
                  v
       anna/internal/{parse,match,mirror}
```

1. Production SDK code must not import repository packages outside `anna/`.
2. SDK tests and maintenance tools must not depend on application configuration or private application fixtures. Optional developer settings may be read by explicitly invoked live-test helpers, not by the production client.
3. Application code must use the public `anna` package. It must not import the SDK's parser, matcher, or mirror internals. Placing these under `anna/internal/` makes Go enforce this boundary for application packages outside `anna/`.
4. Private SDK packages must not import their parent `anna` package and create an import cycle. They return their own internal results/errors; the public client translates them as needed.
5. The SDK may use standard-library and third-party packages. Its runtime dependency graph must not pull in Cobra, the MCP SDK, application environment loading, or the application's global logger.
6. Sharing one `go.mod` means application dependencies remain listed at module level. A clean SDK package dependency graph does not mean a separate dependency manifest in this first extraction.

## Public API

### Client and operations

Build on the current client instead of replacing its model. The initial public surface covers:

| Operation | Existing API to retain as the starting point |
| --- | --- |
| Book search | `FindBook` |
| Article search | `FindArticle` |
| Catalog metadata search | `FindMetadata` |
| DOI lookup | `LookupDOI` |
| Record details | `Details` |
| Public file statistics | `FileStats` |
| Membership and allowance | `AccountStatus` |
| Book download | `DownloadBook` |
| Article download | `DownloadArticle` |

Expose the configuration, search options, domain records, pagination information, account data, download results, and progress callback types these operations need. No exported method signature should force callers to name a type in `anna/internal/`.

Keep the current method names and explicit timeout arguments for the first extraction unless a concrete correctness issue requires a change. Document their actual semantics: a positive operation timeout adds a deadline; zero currently adds no operation-wide deadline, while internal network limits still apply. Application defaults continue to be supplied by the adapters. Do not silently change zero to the CLI's default timeout.

Review currently exported implementation helpers before publication. Exporting a symbol from today's internal package does not automatically make it a desirable SDK commitment. For identifier utilities genuinely needed by adapters, expose a small domain API, such as DOI normalization/validation and record-ID parsing, that delegates to private implementations. Do not expose the parser package to satisfy one adapter call.

### Construction and configuration

Use `NewClient(Config) (*Client, error)` for the public constructor. Return malformed configuration errors immediately instead of retaining the current delayed `configErr` state.

The constructor must make no network requests. It may restore explicitly configured local session/mirror state, preserving existing best-effort behavior. Missing credentials are not a construction error: validate access requirements at the operation that needs them. A client should still be constructible for local-file reuse and tests without credentials.

Retain explicit settings for base URL, automatic mirror selection, credentials, initial account cookie, download directory, optional session file, and injected HTTP client. Add optional injected logging. Preserve these distinctions:

- Empty base URL uses the current SDK default, with the official mirror list maintained inside the SDK.
- Preserve the existing meaning of `AutoBaseURL`: false fixes the configured/default origin; true enables failover. The application still defaults this setting to true.
- Empty session-file configuration disables session and mirror persistence. The SDK does not choose a cache directory in the user's home folder.
- Empty download-directory configuration is valid for search and metadata usage; reject it only when a download operation requires a destination.
- A supplied but invalid configuration value should fail predictably. Keep operation-specific requirements separate from constructor validation so unrelated operations remain usable.
- The application remains responsible for `~` expansion, home-directory discovery, `.env`, and `ANNAS_*` variable precedence.
- Document ownership of injected HTTP clients/transports and verify that configuration is not unexpectedly mutated by SDK use.

Retain the existing session and mirror-state formats and application paths. Users should not need to sign in again or move their cache because code moved into a public package.

### Errors

Move the reusable error implementation from `internal/apperr/` into `anna/errors.go`, including cause preservation and classification. Retain the current machine-readable categories: `INVALID_ARGUMENT`, `CONFIG`, `NOT_FOUND`, `UPSTREAM_BLOCKED`, `REQUEST_TIMEOUT`, and `UPSTREAM`.

Expose `Error` and `CodeOf`, with wrapped causes compatible with `errors.Is` and `errors.As`. Keep cancellation and compound download-failure classification intact. Internal mirror errors should continue to be translated by the client without importing the public package from inside mirror code.

Migrate application callers to the SDK error contract. Remove the old generic error package once unused rather than maintaining two classifiers. Application-only input/configuration failures may use the same categories without creating a reverse SDK dependency.

SDK messages refer to configuration fields and archive operations. The adapters add guidance about environment variables, MCP clients, and platform settings. Preserve user-facing error codes and useful troubleshooting, using structured error context where necessary instead of branching on error-message text.

### Logging and state

Add an optional standard-library `*slog.Logger` to configuration and use a discard logger when it is absent. Thread logging into mirror selection and persistence. Configure application SDK diagnostics on stderr with the current intended verbosity, preserving clean MCP stdout.

Remove SDK calls to the application-global Zap logger. Review the process-global unread-page diagnostic deduplication in `unread.go`; make it client-owned so one SDK client cannot suppress another client's diagnostics.

Document concurrency guarantees backed by the existing locking and race tests: sign-in coordination, mirror state, statistics limits/cache, same-hash download coordination, and progress callback behavior. Distinguish in-process coordination from behavior across separate processes sharing a directory; do not promise stronger file locking than the implementation provides.

### Validation and result semantics

Move archive rules from `internal/modes/service.go` and `internal/env/env.go` into the SDK where appropriate. The same typed request must receive the same domain validation whether it originates in an MCP tool, CLI, or external Go application.

The SDK owns valid identifiers, cookie/origin handling, search field/filter semantics, language normalization, sort restrictions, article DOI/hash exclusivity, and download target validation. Adapters own comma-separated flags, schema decoding, CLI aliases, display limits, and application timeout policies. Keep adapter-level resource limits where they serve the interface rather than the upstream protocol.

Preserve the existing distinctions between exact and partial search results, known and unknown statistics, lower-bound totals, catalog records without files, and verified DOI matches. Keep optional statistics behavior and failure handling unchanged. Avoid introducing new search ranking, pagination, or download behavior during extraction.

Preserve existing JSON tags needed by MCP/CLI consumers. Document which fields and types constitute the SDK contract. Signed download URLs and credentials must remain excluded from serialized output and diagnostic logging.

### Presentation

Move the CLI-specific `String()` renderers and their formatting helpers out of SDK domain types and into `internal/modes/`. Update CLI call sites and interface constraints accordingly. Preserve current labels, order, and output formatting with existing adapter tests.

Keep MCP result envelopes, tool schemas, tool descriptions, instructions, and output limits in the adapters. The SDK returns domain results; the adapter translates them into the existing response shape.

## Migration map and specific dependencies

| Current location or dependency | Planned change |
| --- | --- |
| `internal/anna/*.go` and client fixtures | Move to `anna/`, with application concerns removed |
| `internal/anna/parse/` | Move intact to `anna/internal/parse/`, then fix references |
| `internal/anna/match/` | Move intact to `anna/internal/match/`, then fix references |
| `internal/mirror/` | Move to `anna/internal/mirror/`; inject logger |
| `internal/apperr/` | Move reusable errors/tests to public `anna`; update application imports |
| `internal/anna/client_download.go` imports `internal/env` | Remove the application default-folder dependency and move platform/environment advice to adapters |
| SDK and mirrors import `internal/logger` | Replace with client/injected logging |
| `internal/modes/service.go` imports `internal/anna/parse` for `IsDOIQuery` | Route DOI-query detection through a public domain helper |
| `internal/env/env.go` imports `internal/mirror` | Let SDK configuration validation own archive-origin rules; env only reads application settings |
| Cookie validation duplicated between env and client | Consolidate archive cookie handling inside SDK; preserve accepted application input forms |
| `internal/anna/download_dedupe_live_test.go` imports application env | Build explicit SDK configuration in an SDK-owned live-test helper |
| Root upstream-watch script, tests, and JSON | Move to `anna/scripts/`; keep root npm command as the entry point |
| Root `README.md`, `CONTRIBUTING.md`, `HOW-IT-WORKS.md` | Update architecture, paths, examples, and links without changing installation instructions |

Audit every reference, including tests, before deleting old directories. There should be no production implementation left in `internal/anna`, `internal/mirror`, or an application error package used by the SDK.

## Fixtures, capture, and maintenance tooling

Move existing fixtures and expected outputs without regenerating them. A directory refactor should not change expected parsed values.

Known path-sensitive areas to update:

- The client test fixture embed currently includes `parse/testdata/*`; its target becomes `internal/parse/testdata/*` relative to `anna/`. Keep embeds exclusively in test files.
- Capture and crawl tests currently write into `parse/testdata/pages` and `parse/testdata/corpus`. Update output locations and helper comments.
- Several live tests read `../../.env`; moving packages changes that meaning. Use one explicit SDK-owned live-test configuration helper with documented settings. Production code must not inherit this behavior.
- `parse/hooks_test.go` reads the upstream watch JSON through a relative path. Update it to the watch file inside `anna/scripts/` and keep the hook-coverage check intact.
- Match/corpus tests and fuzz seeds may reach into sibling parser fixtures. Update those paths and keep all referenced data inside `anna/`.
- `scripts/check-upstream.mjs` currently derives paths from its location and embeds old package paths in report/issue text. Update watch-file resolution, tests, and generated instructions after moving it.

Keep `npm run check:upstream` working by changing its target to `anna/scripts/check-upstream.mjs`. Update the weekly workflow to invoke the new path. Preserve the current reviewed upstream commit; moving the watch file must not accept unseen changes.

The upstream script may retain GitHub issue integration as maintenance tooling; keep credentials supplied through the existing CI environment and do not make the SDK runtime depend on GitHub. Moving tooling does not require executing its issue-update mode or contacting the live archive.

Keep existing opt-in live-test switches working during this extraction. Credentialed searches, capture/crawl runs, and quota-consuming downloads remain separate from normal offline tests. Captured pages continue to scrub account identifiers, credentials, and signed URLs using the existing checks.

## Implementation sequence

### Phase 1: Establish the baseline and intended API

- [x] Record repository status and preserve unrelated work.
- [x] Run the current offline test/coverage and adapter checks to establish a baseline; record any pre-existing failure.
- [x] Inventory SDK imports, exported symbols, fixtures, capture paths, and application protocol logic.
- [x] Write the minimal public API contract, including constructor behavior, error codes, timeout semantics, configuration defaults, and result distinctions.
- [x] Identify adapter behavior that must remain stable, including JSON shape, CLI rendering, error guidance, and persisted state.

Deliverable: an agreed implementation boundary and a reproducible baseline, before changing behavior.

### Phase 2: Consolidate code and tests

- [x] Move the client to `anna/`, parser/matcher/mirrors below `anna/internal/`, and their tests/fixtures with them.
- [x] Move error ownership into the SDK and update imports.
- [x] Update internal package imports, fixture embeds, relative paths, and test helper references.
- [x] Resolve illegal imports exposed by the new `internal/` boundary; do not work around it by making implementation packages public.
- [x] Keep this phase focused on ownership and buildability; avoid changing parser or matching algorithms.

Deliverable: the consolidated SDK and application compile, and existing behavior tests still use the same fixture expectations.

### Phase 3: Complete the reusable library boundary

- [x] Make construction return configuration errors without network access.
- [x] Consolidate archive validation and eliminate the SDK's dependencies on application env/logger packages.
- [x] Inject logging, make unread-page diagnostic state client-owned, and preserve stderr application diagnostics.
- [x] Expose only the types and domain helpers needed by real consumers.
- [x] Move CLI renderers and application troubleshooting into the adapters.
- [x] Update MCP/CLI service construction and error handling to use the public API.
- [x] Keep protocol behavior, cache formats, filenames, download verification, and output contracts unchanged.

Deliverable: application adapters consume the same API available to another Go project, with no private SDK imports.

### Phase 4: Move maintenance ownership and update repository integration

- [x] Move upstream-watch implementation, tests, and configuration into `anna/scripts/`.
- [x] Update `package.json` commands, `.github/workflows/upstream-watch.yml`, generated report instructions, and hook checks.
- [x] Update coverage package names in `scripts/check-coverage.mjs` to `anna/internal/parse` and `anna/internal/match`, preserving the existing 95% thresholds and 80% total threshold.
- [x] Add `anna` to formatting commands in CI and contributor documentation; current commands only name `cmd internal`.
- [x] Update build/package checks to recognize the new SDK source location, ensuring fixtures and SDK source do not leak into the npm launcher, binary archives, container runtime, or MCP bundle.
- [x] Keep Go source/test fixtures available in the source repository/module, as required for SDK tests; source distribution differs from application binary distribution.
- [x] Review repository-wide test-data policy, Docker build inputs, release archive definitions, and all remaining old-path references.

Deliverable: CI, release tooling, and upstream maintenance continue to cover the moved code.

### Phase 5: Verify independent consumption and document usage

- [x] Add meaningful `package anna_test` tests that use only public SDK types and an injected HTTP transport.
- [x] Cover constructor errors, a search result with pagination/partial-result semantics, typed errors/cancellation, and a checksum-verified download into a temporary directory.
- [x] Add a small separate-module consumer check using a temporary module and a local `replace` pointing to this repository. It must compile and run without repository-private imports, credentials, or live requests.
- [x] Verify the SDK production dependency graph and test imports obey the ownership rules.
- [x] Add examples for search, download, and inspecting errors; credentialed examples must not execute during routine tests.
- [x] Write package docs and `anna/README.md`; link them from the root README and update contributor instructions.
- [x] Run final offline verification and review the resulting API before release.

Deliverable: a documented, independently importable SDK used by both existing application interfaces.

These are implementation checkpoints, not separate repositories or release streams. Prefer reviewable changes that separate mechanical moves from behavior-sensitive cleanup, while keeping the final branch buildable.

## Verification plan

Run focused tests as each boundary changes, then the full existing offline checks. Do not rewrite expected outputs just to make a failing move pass.

```sh
gofmt -l anna cmd internal
go test ./anna/...
go test ./internal/modes ./internal/env ./internal/testpolicy
go vet ./...
npm run check:version
npm test
npm run coverage
```

`npm test` must include the relocated upstream-script unit tests. `npm run coverage` continues to run Go tests with race detection and the current coverage gates. Full-suite coverage replaces a redundant standalone full race run.

Inspect package dependencies with `go list -deps ./anna/...` and review test imports separately. Add a focused boundary check if necessary to prevent SDK-to-application imports from returning. Verify the external consumer test cannot gain access by residing under `anna/` itself.

Run existing packaging checks after producing their required build/bundle artifacts: MCP bundle validation, `scripts/check-package-contents.mjs`, Docker build/handshake, and GoReleaser configuration/snapshot validation. Use the existing platform CI matrix for Linux, macOS, and Windows. Record unavailable local tooling rather than claiming checks ran.

Live validation is a separate follow-up when credentials and the relevant scope are available. The extraction should pass its acceptance checks without contacting Anna's Archive, refreshing goldens, accepting upstream commits, publishing releases, or consuming download allowance.

## Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| A move hides a behavior change | Keep fixture outputs unchanged; separate algorithm changes from extraction |
| Application imports become illegal under `anna/internal/` | Route required domain operations through public SDK methods/helpers |
| Error extraction creates a package cycle | Public client owns classification; private packages return internal errors without importing their parent |
| Constructor cleanup blocks unrelated operations | Separate malformed configuration from credentials/destination required only by an operation |
| Live tests retain application dependencies | Centralize explicit test-only SDK configuration and audit test imports |
| CLI output changes when `String()` methods move | Preserve adapter formatting tests and update rendering call sites explicitly |
| Logs contaminate MCP stdout or couple clients | Discard SDK logger by default, injected stderr application logger, per-client diagnostic state |
| Captures or upstream scripts read/write the wrong location | Audit embed paths, relative reads/writes, script-relative roots, and generated instructions |
| Moved packages lose formatting or coverage enforcement | Update explicit CI paths and package coverage keys |
| Packaging begins shipping captured pages | Preserve test-only embeds and run artifact-content checks |
| The new API promises more than implemented | Document timeout, persistence, concurrency, and callback semantics verified by tests |
| Independent use requires hidden application setup | Run an external-module consumer with fake transport and explicit configuration |

## Completion criteria

- [x] All production AA interaction code lives below `anna/`, including parsing, matching, and mirror handling.
- [x] AA-specific behavior tests, upstream fixtures, capture helpers, and change-detection tooling live below `anna/`.
- [x] The SDK has no imports of repository application packages, including in tests.
- [x] The application reaches the SDK exclusively through its public package.
- [x] Another Go module can import the client and exercise it with explicit configuration.
- [x] SDK import/construction requires neither application environment variables nor network access.
- [x] Runtime errors are machine-readable and support wrapped-cause inspection.
- [x] MCP tools, CLI commands, JSON responses, text output, environment settings, and cache formats remain compatible.
- [x] Existing parsing, citation matching, mirror trust, authentication, and verified-download behavior is preserved.
- [x] Offline tests, race/coverage gates, adapter integration, and applicable packaging checks pass.
- [x] Documentation and runnable examples explain how to use the public SDK.
- [x] There is still one repository, one `go.mod`, and one client implementation.

## Deferred work

Do not add a separate repository/module, language bindings, an HTTP service, a public raw-parser API, arbitrary storage backends, or new download/search features as part of this extraction. The folder boundary should make later work easier without requiring those projects now.

Releasing is a subsequent repository release step. Once the refactor is validated, use the existing version/tag process and document the newly public package; do not create an SDK-only publishing pipeline.
