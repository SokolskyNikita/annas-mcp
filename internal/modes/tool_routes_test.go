package modes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolCase struct {
	Name      string
	Tool      string
	Arguments map[string]any
	ResultKey string `json:"result_key"`
}

type records struct {
	Books  []*anna.Book
	Papers []*anna.Paper
}

func TestEveryMCPRouteReturnsStructuredResultsAndPreservesTimeout(t *testing.T) {
	t.Parallel()
	inputs := fixtureJSON[records](t, "records.json")
	for _, tc := range fixtureJSON[[]toolCase](t, "tool-cases.json") {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			archive := &fakeArchive{books: inputs.Books, papers: inputs.Papers}
			session := connectTestServer(t, archive)
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.Tool, Arguments: tc.Arguments})
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError || result.StructuredContent == nil {
				t.Fatalf("unexpected result: %+v", result)
			}
			var decoded map[string]any
			if err := json.Unmarshal([]byte(toolText(t, result)), &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded[tc.ResultKey] == nil {
				t.Fatalf("missing %s: %v", tc.ResultKey, decoded)
			}
			if archive.calls != 1 || archive.timeout != 23*time.Second {
				t.Fatalf("calls=%d timeout=%s", archive.calls, archive.timeout)
			}
			if tc.Tool == "article_search" && tc.ResultKey == "results" && archive.options.Index != "journals" {
				t.Fatalf("article keyword search used %+v", archive.options)
			}
		})
	}
}

func TestEveryMCPRouteReportsUpstreamErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureJSON[[]toolCase](t, "tool-cases.json") {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			session := connectTestServer(t, &fakeArchive{failure: apperr.New(apperr.UpstreamBlocked, "account access denied")})
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.Tool, Arguments: tc.Arguments})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.HasPrefix(toolText(t, result), "[UPSTREAM_BLOCKED]") {
				t.Fatalf("lost error code: %+v", result)
			}
		})
	}
}

func TestEveryMCPRouteRejectsOutOfRangeTimeoutBeforeUpstream(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureJSON[[]toolCase](t, "tool-cases.json") {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			archive := &fakeArchive{}
			session := connectTestServer(t, archive)
			tc.Arguments["timeout_seconds"] = 86401
			result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: tc.Tool, Arguments: tc.Arguments})
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError || !strings.HasPrefix(toolText(t, result), "[INVALID_ARGUMENT]") || archive.calls != 0 {
				t.Fatalf("invalid timeout reached upstream: %+v, calls=%d", result, archive.calls)
			}
		})
	}
}

func TestDownloadToolsDeliverRequestedProgress(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureJSON[[]toolCase](t, "tool-cases.json") {
		if !strings.HasSuffix(tc.Tool, "_download") {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			progress := make(chan *mcp.ProgressNotificationParams, 1)
			options := &mcp.ClientOptions{ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
				progress <- req.Params
			}}
			session := connectTestServerOptions(t, &fakeArchive{}, options)
			params := &mcp.CallToolParams{Name: tc.Tool, Arguments: tc.Arguments}
			params.SetProgressToken("download-test")
			result, err := session.CallTool(t.Context(), params)
			if err != nil || result.IsError {
				t.Fatalf("download: %v %+v", err, result)
			}
			select {
			case notification := <-progress:
				if notification.ProgressToken != "download-test" || notification.Progress != 123 || notification.Total != 123 {
					t.Fatalf("unexpected progress: %+v", notification)
				}
			case <-time.After(time.Second):
				t.Fatal("missing progress notification")
			}
		})
	}
}
