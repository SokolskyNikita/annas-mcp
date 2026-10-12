package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBookSearchStatsOnlyEnrichTrimmedExactResults(t *testing.T) {
	first := "0123456789abcdef0123456789abcdef"
	second := "1123456789abcdef0123456789abcdef"
	discarded := "2123456789abcdef0123456789abcdef"
	partial := "3123456789abcdef0123456789abcdef"
	archive := &fakeArchive{
		books:   []*anna.Book{{Hash: first}, {Hash: second}, {Hash: discarded}},
		partial: []*anna.Book{{Hash: partial}},
		stats: map[string]anna.RecordStats{
			first:  {Downloads: 17, Lists: 3},
			second: {},
		},
	}
	result, err := (&service{archive: archive}).bookSearch(t.Context(), SearchParams{
		Query: "example", Limit: 2, IncludeStats: true,
	}, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if archive.statsCalls != 1 || len(archive.statsHashes) != 2 || archive.statsHashes[0] != first || archive.statsHashes[1] != second {
		t.Fatalf("stats hashes=%v calls=%d", archive.statsHashes, archive.statsCalls)
	}
	if result.Matched != 3 || result.Limit != 2 || len(result.Results) != 2 || len(result.PartialMatches) != 0 {
		t.Fatalf("page changed during enrichment: %+v", result)
	}
	if result.Results[0].Stats == nil || *result.Results[0].Stats != (anna.RecordStats{Downloads: 17, Lists: 3}) {
		t.Fatalf("first stats=%+v", result.Results[0].Stats)
	}
	if result.Results[1].Stats == nil || *result.Results[1].Stats != (anna.RecordStats{}) {
		t.Fatalf("confirmed zero stats were not attached: %+v", result.Results[1].Stats)
	}
	if result.Results[0].Hash != first || result.Results[1].Hash != second {
		t.Fatalf("result order changed: %+v", result.Results)
	}
}

func TestArticleSearchStatsDeduplicateHashesAndIncludePartialMatches(t *testing.T) {
	first := "0123456789abcdef0123456789abcdef"
	second := "1123456789abcdef0123456789abcdef"
	archive := &fakeArchive{
		papers:         []*anna.Paper{{Hash: strings.ToUpper(first)}},
		articlePartial: []*anna.Paper{{Hash: first}, {Hash: second}},
		stats: map[string]anna.RecordStats{
			first:  {Downloads: 4},
			second: {Downloads: 9},
		},
	}
	value, err := (&service{archive: archive}).articleSearch(t.Context(), SearchParams{
		Query: "example", Limit: 3, IncludeStats: true,
	}, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	result := value.(searchResult[*anna.Paper])
	if archive.statsCalls != 1 || len(archive.statsHashes) != 2 || archive.statsHashes[0] != first || archive.statsHashes[1] != second {
		t.Fatalf("stats hashes=%v calls=%d", archive.statsHashes, archive.statsCalls)
	}
	if len(result.Results) != 1 || len(result.PartialMatches) != 2 {
		t.Fatalf("search result shape changed: %+v", result)
	}
	for _, paper := range []*anna.Paper{result.Results[0], result.PartialMatches[0]} {
		if paper.Stats == nil || paper.Stats.Downloads != 4 {
			t.Fatalf("duplicate hash did not receive stats: %+v", paper)
		}
	}
	if result.PartialMatches[1].Stats == nil || result.PartialMatches[1].Stats.Downloads != 9 {
		t.Fatalf("partial match stats=%+v", result.PartialMatches[1].Stats)
	}
}

func TestDOISearchStatsRequireAFileHash(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	archive := &fakeArchive{
		doiPaper: &anna.Paper{DOI: "10.1000/example", Hash: strings.ToUpper(hash)},
		stats:    map[string]anna.RecordStats{hash: {Downloads: 21}},
	}
	value, err := (&service{archive: archive}).articleSearch(t.Context(), SearchParams{
		Query: "10.1000/example", IncludeStats: true,
	}, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	paper := value.(*anna.Paper)
	if paper.Stats == nil || paper.Stats.Downloads != 21 || archive.statsCalls != 1 || archive.statsHashes[0] != hash {
		t.Fatalf("DOI stats=%+v hashes=%v calls=%d", paper.Stats, archive.statsHashes, archive.statsCalls)
	}

	archive = &fakeArchive{doiPaper: &anna.Paper{DOI: "10.1000/example"}}
	value, err = (&service{archive: archive}).articleSearch(t.Context(), SearchParams{
		Query: "10.1000/example", IncludeStats: true,
	}, time.Second, 10)
	if err != nil {
		t.Fatal(err)
	}
	if value.(*anna.Paper).Stats != nil || archive.statsCalls != 0 {
		t.Fatalf("hashless DOI performed a stats lookup: %+v, calls=%d", value, archive.statsCalls)
	}
}

func TestStatsFailureIsOptionalAndCancellationPropagates(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	archive := &fakeArchive{
		books:        []*anna.Book{{Hash: hash}},
		statsFailure: errors.New("stats unavailable"),
	}
	result, err := (&service{archive: archive}).bookSearch(t.Context(), SearchParams{
		Query: "example", IncludeStats: true,
	}, time.Second, 10)
	if err != nil || result.Results[0].Stats != nil {
		t.Fatalf("optional stats failure affected search: result=%+v err=%v", result, err)
	}

	started := make(chan struct{}, 1)
	archive = &fakeArchive{
		books: []*anna.Book{{Hash: hash}},
		statsFunc: func(ctx context.Context, _ []string) (map[string]anna.RecordStats, error) {
			started <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := (&service{archive: archive}).bookSearch(ctx, SearchParams{
			Query: "example", IncludeStats: true,
		}, time.Second, 10)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		cancel()
		t.Fatal("stats lookup did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("search did not return after cancellation")
	}
}

func TestIncludeStatsUsesOneOperationDeadline(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	archive := &fakeArchive{
		books: []*anna.Book{{Hash: hash}},
	}
	archive.statsFunc = func(ctx context.Context, _ []string) (map[string]anna.RecordStats, error) {
		statsDeadline, hasStatsDeadline := ctx.Deadline()
		if !archive.hasBookDeadline || !hasStatsDeadline || !statsDeadline.Equal(archive.bookDeadline) {
			return nil, errors.New("stats lookup did not share search deadline")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	_, err := (&service{archive: archive}).bookSearch(t.Context(), SearchParams{
		Query: "example", IncludeStats: true,
	}, 20*time.Millisecond, 10)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want operation deadline", err)
	}
}

func TestIncludeStatsCLIAndMCPSchema(t *testing.T) {
	hash := "0123456789abcdef0123456789abcdef"
	newArchive := func() *fakeArchive {
		return &fakeArchive{
			books: []*anna.Book{{Title: "Example", Hash: hash}},
			stats: map[string]anna.RecordStats{hash: {Downloads: 5}},
		}
	}

	archive := newArchive()
	var output bytes.Buffer
	cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"book-search", "--include-stats", "--json", "example"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var page searchResult[*anna.Book]
	if err := json.Unmarshal(output.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if archive.statsCalls != 1 || len(page.Results) != 1 || page.Results[0].Stats == nil || page.Results[0].Stats.Downloads != 5 {
		t.Fatalf("CLI JSON did not include stats: %s", output.String())
	}

	archive = newArchive()
	output.Reset()
	cmd = newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"book-search", "--include-stats", "example"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Downloads: 5") {
		t.Fatalf("plain CLI output omitted counts: %s", output.String())
	}

	archive = newArchive()
	output.Reset()
	cmd = newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"book-search", "--json", "example"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if archive.statsCalls != 0 || strings.Contains(output.String(), `"stats"`) {
		t.Fatalf("default CLI result changed: calls=%d output=%s", archive.statsCalls, output.String())
	}

	session := connectTestServer(t, newArchive())
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "book_search" && tool.Name != "article_search" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("unexpected %s schema: %T", tool.Name, tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s schema has no properties: %+v", tool.Name, schema)
		}
		if _, ok := properties["include_stats"]; !ok {
			t.Fatalf("%s schema lacks include_stats", tool.Name)
		}
		if tool.Name == "book_search" && !schemaHasProperty(tool.OutputSchema, "stats") {
			t.Fatal("book_search output schema lacks stats")
		}
	}
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "book_search", Arguments: map[string]any{"query": "example", "include_stats": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var mcpPage searchResult[*anna.Book]
	if err := json.Unmarshal([]byte(toolText(t, result)), &mcpPage); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(mcpPage.Results) != 1 || mcpPage.Results[0].Stats == nil || mcpPage.Results[0].Stats.Downloads != 5 {
		t.Fatalf("MCP result omitted stats: %+v", result)
	}

	archive = newArchive()
	archive.papers = []*anna.Paper{{DOI: "10.1000/example", Hash: hash}}
	archive.doiPaper = &anna.Paper{DOI: "10.1000/example", Hash: hash}
	session = connectTestServer(t, archive)
	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "article_search", Arguments: map[string]any{"query": "example", "include_stats": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var articlePage searchResult[*anna.Paper]
	if err := json.Unmarshal([]byte(toolText(t, result)), &articlePage); err != nil {
		t.Fatal(err)
	}
	if result.IsError || len(articlePage.Results) != 1 || articlePage.Results[0].Stats == nil || articlePage.Results[0].Stats.Downloads != 5 {
		t.Fatalf("article page MCP result omitted stats: %+v", result)
	}

	result, err = session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "article_search", Arguments: map[string]any{"query": "10.1000/example", "include_stats": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doiPaper anna.Paper
	if err := json.Unmarshal([]byte(toolText(t, result)), &doiPaper); err != nil {
		t.Fatal(err)
	}
	if result.IsError || doiPaper.Stats == nil || doiPaper.Stats.Downloads != 5 {
		t.Fatalf("DOI MCP result omitted stats: %+v", result)
	}
}

func schemaHasProperty(schema any, property string) bool {
	switch value := schema.(type) {
	case map[string]any:
		if properties, ok := value["properties"].(map[string]any); ok {
			if _, ok := properties[property]; ok {
				return true
			}
		}
		for _, child := range value {
			if schemaHasProperty(child, property) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if schemaHasProperty(child, property) {
				return true
			}
		}
	}
	return false
}
