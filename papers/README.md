# Go SDK for scholarly papers

Use `github.com/SokolskyNikita/annas-mcp/papers` to find a work, inspect available copies and download or read it. The same coordinator powers the article tools in MCP and the CLI. Applications supply credentials explicitly. Callers that only use Anna’s Archive can continue to import [the archive SDK](../anna/README.md).

Install the SDK with `go get github.com/SokolskyNikita/annas-mcp/papers@v0.8.0`.

## Download by DOI

In this example the calling application supplies absolute download and cache directories. Set a context deadline for the entire operation, including fallback. To enable AA fallback, pass a configured `*anna.Client` as `Config.Archive`, using the same output directory. The DOI identifies the paper. The provider serving the file resolves its download URL.

```go
func fetch(ctx context.Context, key, directory, cache string, archive *anna.Client) (papers.DownloadResult, error) {
    cfg := papers.Config{OpenAlexAPIKey: key, DownloadPath: directory, CacheDir: cache}
    if archive != nil {
        cfg.Archive = archive
    }
    client, err := papers.NewClient(cfg)
    if err != nil {
        return papers.DownloadResult{}, err
    }
    ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
    defer cancel()
    return client.Download(ctx, papers.DownloadOptions{DOI: "10.1038/s41586-021-03819-2"}, nil)
}
```

Pass a progress callback in place of `nil` to receive bytes written and total bytes when known. On success, use `result.Path` on the server host. `Provider` and `Route` identify the source. A hosted PDF can have an unknown manuscript version. To require a particular version or location, obtain its identifier with `Details` and pass the selection in `DownloadOptions`.

## Search for a dissertation without a DOI

```go
page, err := client.Search(ctx, papers.SearchOptions{
    Query: "graph algorithms", WorkType: "dissertation", Limit: 5,
})
if err != nil {
    return err
}
for _, work := range page.Results {
    fmt.Println(work.OpenAlexID, work.Title, work.Authors, work.Access)
}
```

After selecting a hit, call `client.Download(ctx, papers.DownloadOptions{OpenAlexID: work.OpenAlexID}, nil)`. A no-DOI thesis can have hosted or repository content. If only a landing page is known, give the reader that link. Use `client.Details(ctx, id, papers.DetailsOptions{})` for the citation and copy choices. For exact-ID searches, the single work is in `SearchResult.Work`.

## Reading and recovery

Call `client.Read(ctx, papers.ReadOptions{ID: id})` for a bounded passage of structured full text. Check `Source` before treating the passage as full text. Use `*result.NextArgs` for a continuation. Content retrieval can use OpenAlex allowance. Search and details only retrieve metadata.

Pass a caller-owned context into each operation. Use `errors.As(err, &failure)` with `var failure *papers.Error` to inspect a provider failure’s code, reason, retryability and any known landing page. Use `client.Status(ctx)` to inspect OpenAlex usage when needed. Keep keys out of logs, raw URLs and user-facing messages.
