package modes

import (
	"context"
	"errors"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const serverInstructions = "Use these tools when the user needs the full text of a specific book, paper, standard, or citation. Search before downloading and compare title, authors, language, and format. article_search accepts bare DOIs, doi: identifiers, DOI URLs, or keywords. Keywords search journal articles; book_search searches books. Pass the selected 32-character hash to a download tool, or a DOI to article_download. Download format only names the extension; it does not convert files. Search returns at most 10 hits by default; matched counts the parsed hits on that page. Raise limit before increasing page. Downloads write under ANNAS_DOWNLOAD_PATH, preserve existing files, and return path and bytes. Errors begin with a stable code: INVALID_ARGUMENT, CONFIG, NOT_FOUND, UPSTREAM_BLOCKED, REQUEST_TIMEOUT, or UPSTREAM. Report errors; do not claim a file was downloaded unless the tool succeeded."

func toolFailure[T any](err error) (*mcp.CallToolResult, T, error) {
	var zero T
	return nil, zero, errors.New(codedError(err))
}

func progressNotifier(ctx context.Context, req *mcp.CallToolRequest) anna.ProgressFunc {
	if req == nil || req.Session == nil || req.Params == nil {
		return nil
	}
	token := req.Params.GetProgressToken()
	if token == nil {
		return nil
	}
	return func(done, total int64) {
		_ = req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token, Progress: float64(done), Total: float64(total), Message: "Downloading",
		})
	}
}

func tool(name, description string, readOnly bool) *mcp.Tool {
	openWorld, destructive := true, false
	return &mcp.Tool{Name: name, Description: description, Annotations: &mcp.ToolAnnotations{
		ReadOnlyHint: readOnly, OpenWorldHint: &openWorld, DestructiveHint: &destructive,
	}}
}

func newMCPServer(svc *service, searchTimeout, downloadTimeout time.Duration) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "annas-mcp", Version: version.GetVersion()}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		Capabilities: &mcp.ServerCapabilities{},
	})
	mcp.AddTool(server, tool("book_search", "Search books, textbooks, manuals, and standards by title, author, or topic. Returns one page with hash, format, metadata, and description when available. Pass a selected hash to book_download.", true),
		func(ctx context.Context, req *mcp.CallToolRequest, params SearchParams) (*mcp.CallToolResult, searchResult[*anna.Book], error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, searchTimeout)
			if err != nil {
				return toolFailure[searchResult[*anna.Book]](err)
			}
			result, err := svc.bookSearch(ctx, params, timeout, defaultSearchLimit)
			if err != nil {
				return toolFailure[searchResult[*anna.Book]](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("article_search", "Find a paper by DOI (including DOI URLs), or search journal articles by keywords. DOI lookup returns one paper; keyword search returns one page. Compare metadata before downloading by hash or DOI.", true),
		func(ctx context.Context, req *mcp.CallToolRequest, params SearchParams) (*mcp.CallToolResult, any, error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, searchTimeout)
			if err != nil {
				return toolFailure[any](err)
			}
			result, err := svc.articleSearch(ctx, params, timeout, defaultSearchLimit)
			if err != nil {
				return toolFailure[any](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("book_download", "Download the selected book using its hash from search. Saves a verified file under ANNAS_DOWNLOAD_PATH and returns path and bytes. Format sets the extension; it does not convert the file.", false),
		func(ctx context.Context, req *mcp.CallToolRequest, params BookDownloadParams) (*mcp.CallToolResult, anna.DownloadResult, error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, downloadTimeout)
			if err != nil {
				return toolFailure[anna.DownloadResult](err)
			}
			result, err := svc.bookDownload(ctx, params, timeout, progressNotifier(ctx, req))
			if err != nil {
				return toolFailure[anna.DownloadResult](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("article_download", "Download a selected paper by exactly one of doi or hash. Saves the file under ANNAS_DOWNLOAD_PATH and returns path and bytes. Optional title and format control its filename.", false),
		func(ctx context.Context, req *mcp.CallToolRequest, params ArticleDownloadParams) (*mcp.CallToolResult, anna.DownloadResult, error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, downloadTimeout)
			if err != nil {
				return toolFailure[anna.DownloadResult](err)
			}
			result, err := svc.articleDownload(ctx, params, timeout, progressNotifier(ctx, req))
			if err != nil {
				return toolFailure[anna.DownloadResult](err)
			}
			return nil, result, nil
		})
	return server
}

func runMCP(ctx context.Context, svc *service, searchTimeout, downloadTimeout time.Duration) error {
	// Configuration is local and mirror discovery is lazy: the transport can
	// negotiate and list tools without contacting the archive or status page.
	return newMCPServer(svc, searchTimeout, downloadTimeout).Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
}
