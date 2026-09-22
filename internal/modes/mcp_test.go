package modes

import (
	"context"
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
