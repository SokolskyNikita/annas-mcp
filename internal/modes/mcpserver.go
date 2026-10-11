package modes

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

//go:embed instructions.md
var serverInstructions string

// iconURL is the published icon without its extension; assets/ has .png and .svg.
const iconURL = "https://raw.githubusercontent.com/SokolskyNikita/annas-mcp/main/assets/icon"

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

func tool(name, title, description string, readOnly bool) *mcp.Tool {
	openWorld, destructive := true, false
	return &mcp.Tool{Name: name, Title: title, Description: description, Annotations: &mcp.ToolAnnotations{
		Title: title, ReadOnlyHint: readOnly, OpenWorldHint: &openWorld, DestructiveHint: &destructive,
	}}
}

func newMCPServer(svc *service, searchTimeout, downloadTimeout time.Duration) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:       "annas-mcp",
		Version:    version.GetVersion(),
		WebsiteURL: "https://github.com/SokolskyNikita/annas-mcp",
		Icons: []mcp.Icon{
			{Source: iconURL + ".png", MIMEType: "image/png", Sizes: []string{"512x512"}},
			{Source: iconURL + ".svg", MIMEType: "image/svg+xml", Sizes: []string{"any"}},
		},
	}, &mcp.ServerOptions{
		Instructions: serverInstructions,
		Capabilities: &mcp.ServerCapabilities{},
	})
	mcp.AddTool(server, tool("book_search", "Search books", "Search books, textbooks, manuals and standards by title, author or topic, optionally filtered by format, language, content type, source library or year and sorted. Returns one page with hash, format, year, fast_download and metadata. Pass a selected hash to book_download, or to book_details for ISBNs, edition and file problems.", true),
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
	mcp.AddTool(server, tool("article_search", "Search articles", "Find a paper by DOI (including DOI URLs), or search journal articles by keywords, author, title or year. DOI lookup returns one paper; keyword search returns one page. Compare metadata before downloading by hash or DOI.", true),
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
	mcp.AddTool(server, tool("metadata_search", "Search catalogs", "Search library catalogs (Open Library, WorldCat, ISBNdb, Goodreads, Google Books and others) for books, including ones with no downloadable file. Use it to find ISBNs, editions and publication details, then search book_search by ISBN or title for files. Pass an id to book_details for identifiers.", true),
		func(ctx context.Context, req *mcp.CallToolRequest, params MetadataSearchParams) (*mcp.CallToolResult, searchResult[*anna.MetadataRecord], error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, searchTimeout)
			if err != nil {
				return toolFailure[searchResult[*anna.MetadataRecord]](err)
			}
			result, err := svc.metadataSearch(ctx, params, timeout, defaultSearchLimit)
			if err != nil {
				return toolFailure[searchResult[*anna.MetadataRecord]](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("book_details", "Book details", "Get the full record for a search hit: ISBNs, DOI, OCLC and other identifiers, edition, year, full description, subjects, alternative titles, problems reported for the file, and download and quality counts. Accepts a hash from book_search or article_search, or an id from metadata_search.", true),
		func(ctx context.Context, req *mcp.CallToolRequest, params DetailsParams) (*mcp.CallToolResult, *anna.Details, error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, searchTimeout)
			if err != nil {
				return toolFailure[*anna.Details](err)
			}
			result, err := svc.details(ctx, params, timeout)
			if err != nil {
				return toolFailure[*anna.Details](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("account_status", "Account status", "Show the account's membership and how many fast downloads it has left in its rolling window. Check it before downloading many files.", true),
		func(ctx context.Context, req *mcp.CallToolRequest, params AccountStatusParams) (*mcp.CallToolResult, *anna.AccountStatus, error) {
			timeout, err := timeoutFromSeconds(params.TimeoutSeconds, searchTimeout)
			if err != nil {
				return toolFailure[*anna.AccountStatus](err)
			}
			result, err := svc.accountStatus(ctx, timeout)
			if err != nil {
				return toolFailure[*anna.AccountStatus](err)
			}
			return nil, result, nil
		})
	mcp.AddTool(server, tool("book_download", "Download book", "Download the selected book using its hash from search. Saves a verified file under ANNAS_DOWNLOAD_PATH and returns path, bytes and the fast downloads left. Format sets the extension; it does not convert the file.", false),
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
	mcp.AddTool(server, tool("article_download", "Download article", "Download a selected paper by exactly one of doi or hash. Saves the file under ANNAS_DOWNLOAD_PATH and returns path and bytes. Optional title and format control its filename.", false),
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
	// Configuration is local and mirror selection is lazy: the transport can
	// negotiate and list tools without contacting the archive.
	return newMCPServer(svc, searchTimeout, downloadTimeout).Run(ctx, &mcp.StdioTransport{MaxLineLength: 1 << 20})
}
