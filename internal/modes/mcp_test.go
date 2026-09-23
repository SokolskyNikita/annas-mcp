package modes

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPToolsDescribeReadOnlySearchAndDownloads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer()
	if _, err := server.Connect(ctx, serverTransport); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient("annas-mcp-test", "0.0.0", nil)
	session, err := client.Connect(ctx, clientTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]*mcp.Tool{}
	for _, tool := range listed.Tools {
		byName[tool.Name] = tool
	}
	for _, name := range []string{"book_search", "article_search"} {
		tool := byName[name]
		if tool == nil || tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s should be marked read-only", name)
		}
	}
	for _, name := range []string{"book_download", "article_download"} {
		tool := byName[name]
		if tool == nil || tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
			t.Fatalf("%s should be marked as a write", name)
		}
		if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
			t.Fatalf("%s should not be marked destructive", name)
		}
	}
}

func TestBookSearchRejectsAnInvalidPageWithoutCallingUpstream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer()
	if _, err := server.Connect(ctx, serverTransport); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient("annas-mcp-test", "0.0.0", nil)
	session, err := client.Connect(ctx, clientTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name: "book_search",
		Arguments: map[string]any{
			"query": "example",
			"page":  -1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected an invalid page to be a tool error")
	}
	if text := toolText(t, result); !strings.Contains(text, "[INVALID_ARGUMENT]") {
		t.Fatalf("expected an invalid-argument code, got %q", text)
	}
}

func TestLimitResultsCapsThePage(t *testing.T) {
	t.Parallel()

	items := []string{"a", "b", "c"}
	got, matched, limit := limitResults(items, 2)
	if matched != 3 || limit != 2 || len(got) != 2 || got[0] != "a" {
		t.Fatalf("limit = %d matched = %d got = %v", limit, matched, got)
	}
	got, matched, limit = limitResults(items, 0)
	if matched != 3 || limit != 3 || len(got) != 3 {
		t.Fatalf("default limit = %d matched = %d got = %v", limit, matched, got)
	}
}

func TestCodedErrorUsesStableCodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err  string
		code string
	}{
		{"page must be 1 or greater", "INVALID_ARGUMENT"},
		{"no paper found for DOI: 10.1/abc", "NOT_FOUND"},
		{"Anna's Archive returned 403. Set ANNAS_ACCOUNT_COOKIE to a fresh aa_account_id2 value", "UPSTREAM_BLOCKED"},
		{"ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH environment variables must be set", "CONFIG"},
		{"context canceled", "REQUEST_TIMEOUT"},
		{"download failed: status 500", "UPSTREAM"},
	}
	for _, tc := range cases {
		got := codedError(errors.New(tc.err))
		prefix := "[" + tc.code + "] "
		if !strings.HasPrefix(got, prefix) {
			t.Fatalf("codedError(%q) = %q, want prefix %s", tc.err, got, prefix)
		}
	}
}

func toolText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("tool result has no content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text == nil {
		t.Fatalf("content type %T", result.Content[0])
	}
	return text.Text
}

func TestArticleDownloadRequiresDOIOrHash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := newMCPServer()
	if _, err := server.Connect(ctx, serverTransport); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient("annas-mcp-test", "0.0.0", nil)
	session, err := client.Connect(ctx, clientTransport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })

	if !strings.Contains(serverInstructions, "full text") {
		t.Fatal("server instructions should say when to use the tools")
	}

	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var download *mcp.Tool
	for _, tool := range listed.Tools {
		if tool.Name == "article_download" {
			download = tool
		}
	}
	if download == nil || !strings.Contains(download.Description, "hash") {
		t.Fatal("article_download should accept a hash from search")
	}

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "article_download",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Fatal("expected a missing doi and hash to be a tool error")
	}
}
