# Anna's Archive Go SDK

Import `github.com/SokolskyNikita/annas-mcp/anna`. The MCP server and CLI use this same client. There is one Go module and one release/tag stream for the SDK and applications; the SDK runtime does not depend on Cobra, the MCP SDK, environment loading, or the application's logger.

## Install

Requires Go 1.26 or newer. Add the SDK to your own module:

```sh
go get github.com/SokolskyNikita/annas-mcp/anna@v0.7.0
```

```go
client, err := anna.NewClient(anna.Config{
    SecretKey: key,
    AutoBaseURL: true,
})
if err != nil {
    return err
}
page, err := client.FindBook(ctx, "computing", anna.SearchOptions{
    Language: []string{"en"},
    Format: []string{"epub"},
    Page: 1,
}, time.Minute)
```

The example assumes imports of `anna` and `time`, and caller-supplied `key` and `ctx`. Runnable [search/error handling](examples/search/main.go) and [download](examples/download/main.go) examples are available. Run them explicitly with `go run ./anna/examples/search` or `go run ./anna/examples/download <md5> <directory>`. They read `ANNAS_SECRET_KEY` themselves and may contact the archive; routine tests only compile them.

## Configuration and construction

`NewClient(Config) (*Client, error)` performs no network requests. It validates a supplied origin, cookie, and absolute filesystem paths immediately, and may restore an explicitly configured session file locally. Missing credentials are allowed; operations requiring archive access validate them. Empty `DownloadPath` is valid for searches and metadata. Downloads require an absolute destination, and new fast downloads require `SecretKey`; an existing checksum-matching local file can be reused without credentials.

| Field | Contract |
| --- | --- |
| `BaseURL` | Empty uses `https://annas-archive.gl`. A hostname or HTTPS origin is accepted; paths, credentials, query strings, translated archive hosts, and HTTP are rejected. |
| `AutoBaseURL` | False fixes the configured/default origin. True enables failover through the SDK's official mirror list. Applications default this to true. |
| `SecretKey` | Account secret for sign-in and fast downloads. Excluded from JSON. |
| `AccountCookie` | Optional initial raw `aa_account_id2` value or Cookie header. Validated and normalized; excluded from JSON. |
| `DownloadPath` | Absolute directory, or empty until downloads are needed. The SDK creates it for a new download. |
| `SessionFile` | Optional absolute session-file path. Empty disables both session and mirror persistence. `mirrors.json` lives beside it. Existing formats are unchanged. |
| `HTTPClient` | Optional injected HTTP client. The SDK copies client settings, shares the transport, and does not mutate or close the supplied client/transport. Its requests do not use the supplied cookie jar, and its redirect checks constrain credential forwarding. The shared transport must support concurrent requests. |
| `Logger` | Optional `*slog.Logger`. Nil discards diagnostics. Applications inject a warning-level stderr logger so MCP stdout stays clean. |

The SDK does not read `.env`, `ANNAS_*`, expand `~`, discover a home directory, or pick a cache path. Those are application responsibilities. Configuration structs should not be formatted as unrestricted debug output: JSON excludes credentials but ordinary Go formatting does not. Treat `Paper.DownloadURL` as a bearer credential; it is excluded from JSON.

## Operations and results

- `FindBook`, `FindArticle`, and `FindMetadata` accept typed `SearchOptions`. `NormalizeSearch` is available to adapters; the client applies it before requests. Filters are slices, with `-` prefixes for exclusions. Language tags are normalized without changing caller-owned slices. Field names, year values, indexes, sorts, identifiers and filters are validated by the SDK. `ResolveSearch` only applies legacy content/default-index mapping; it does not replace validation.
- `LookupDOI` verifies citation identity and uses the existing SciDB/search fallback. `IsDOIQuery`, `NormalizeDOI`, and `ValidDOI` support identifier handling without exposing parser internals. `FindArticle` is keyword search; use `LookupDOI` for DOI resolution.
- `Details` accepts identifiers handled by `ParseRecordID`, including MD5 hashes and archive record URLs. `AccountStatus` reports membership and allowance.
- `FileStats` returns known statistics by hash, omitting failed/unknown lookups. Optional statistics failure does not discard successful search results in the application adapters. A nil `Stats` differs from a populated struct containing zeros.
- `DownloadBook` verifies the file hash, retries/resumes transfers and reuses matching local files. `DownloadArticle` requires exactly one of DOI or hash. `DownloadResult.DownloadsLeft` is absent when allowance is unknown or an existing file was reused.

`SearchPage.Results` contains exact results; `Partial` contains looser matches that may ignore filters. `TotalIsLowerBound` and `MorePages` retain the upstream pagination distinctions. `MetadataRecord` describes a catalog entry without promising a downloadable file. Domain records retain their existing JSON field names; search envelopes, display limits, and plain-text rendering belong to adapters.

All operations with an explicit timeout add a deadline only when it is positive. Zero adds no operation-wide deadline. The caller's earlier context deadline still wins, and internal network limits remain active. The SDK does not silently supply the CLI's 60-second search or 30-minute download defaults. `FileStats` has its own bounded request budget.

## Errors

`CodeOf(err)` returns `INVALID_ARGUMENT`, `CONFIG`, `NOT_FOUND`, `UPSTREAM_BLOCKED`, `REQUEST_TIMEOUT`, or `UPSTREAM`. The configuration category is named `CodeConfig` in Go because `Config` is the configuration type. `Error` provides `Code`, `Message`, optional configuration `Field`, and wrapped `Err`. `NewError` and `WrapError` let adapters preserve the same error contract for their own failures.

```go
if errors.Is(err, context.Canceled) {
    // The caller canceled the operation.
}
var detail *anna.Error
if errors.As(err, &detail) {
    fmt.Println(detail.Code, detail.Field)
}
fmt.Println(anna.CodeOf(err))
```

Cancellation and deadline errors classify as `REQUEST_TIMEOUT`. Compound download failures retain their causes while classifying by the final failure; overall cancellation takes precedence. SDK messages name configuration fields; the MCP/CLI adapters add environment and platform guidance.

## Concurrency and persistence

One client supports concurrent operations. Sign-in is coordinated, mirror health and statistics state are protected, and a shared statistics budget limits parallel requests. Same-hash downloads on one client wait for each other and reuse the verified result. Input slices are copied during search normalization; callers should not mutate inputs while a call is using them.

Progress callbacks run synchronously during transfer, must return promptly, and may restart or report an existing offset after retries/resume. A callback reused by concurrent operations must synchronize its own state. A waiting same-hash call or local-file reuse does not report a new transfer.

Separate clients/processes do not share download locks. Session and mirror files preserve existing best-effort persistence and atomic replacement; they do not provide transactional cross-process coordination. Do not rely on them as a general filesystem lock.

## Testing and maintenance

Run `go test ./anna/...` for offline behavior and public-API tests. `npm run check:sdk` audits runtime/test imports and runs a separate temporary Go module with a local `replace`, an injected fake transport, and network module lookup disabled. `npm run coverage` runs the full suite with race detection and existing coverage thresholds.

Fixtures remain beside their owning packages; captured outputs have not been regenerated. Upstream monitoring lives in `anna/scripts/`, with `npm run check:upstream` as its entry point. See [contributor instructions](../CONTRIBUTING.md) for opt-in capture, crawl, mirror, search, SciDB and quota-consuming download tests. These helpers alone read explicit environment settings, with repository `.env` fallback. The live dedupe helper accepts `ANNAS_BASE_URL` and an optional absolute `ANNAS_SESSION_FILE`; it no longer imports application configuration or infers an application cache path.
