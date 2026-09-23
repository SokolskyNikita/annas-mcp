package modes

import (
	"context"
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
