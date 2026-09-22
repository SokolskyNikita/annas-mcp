package modes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iosifache/annas-mcp/internal/anna"
	"github.com/iosifache/annas-mcp/internal/env"
	"github.com/iosifache/annas-mcp/internal/logger"
	"github.com/iosifache/annas-mcp/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/zap"
)

const serverInstructions = "Search before downloading. book_search and article_search return one page of JSON results; pass the hash to book_download, or the doi to article_download. Increase page when you need more hits. language is an ISO 639-1 code such as en. Book content filters include book_any, book_fiction, book_nonfiction, magazine, and standards_document. Article search defaults to journal. Downloads are written under ANNAS_DOWNLOAD_PATH and the tool result includes the file path. A missing DOI is an error."

var (
	searchTimeout       = anna.DefaultSearchTimeout
	downloadTimeout     = anna.DefaultDownloadTimeout
	languageCodePattern = regexp.MustCompile(`^[a-z]{2,3}$`)
	contentTokenPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
)

func timeoutFromSeconds(seconds int, fallback time.Duration) (time.Duration, error) {
	if seconds == 0 {
		return fallback, nil
	}
	if seconds < 0 {
		return 0, fmt.Errorf("timeout_seconds must be greater than zero")
	}
	return time.Duration(seconds) * time.Second, nil
}

func normalizeSearch(content, language string, page int) (string, string, int, error) {
	if page == 0 {
		page = 1
	}
	if page < 1 {
		return "", "", 0, errors.New("page must be 1 or greater")
	}
	language = strings.ToLower(strings.TrimSpace(language))
	if language != "" && !languageCodePattern.MatchString(language) {
		return "", "", 0, errors.New("language must be an ISO 639-1 code such as en")
	}
	content = strings.ToLower(strings.TrimSpace(content))
	if content != "" && !contentTokenPattern.MatchString(content) {
		return "", "", 0, errors.New("content must contain only lowercase letters, digits, and underscores")
	}
	return content, language, page, nil
}

func BookSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookSearchParams]) (*mcp.CallToolResultFor[any], error) {
	content, language, page, err := normalizeSearch(params.Arguments.Content, params.Arguments.Language, params.Arguments.Page)
	if err != nil {
		return toolError(err)
	}
	timeout, err := timeoutFromSeconds(params.Arguments.TimeoutSeconds, searchTimeout)
	if err != nil {
		return toolError(err)
	}

	books, err := anna.FindBook(ctx, params.Arguments.Query, anna.SearchOptions{
		Content:  content,
		Language: language,
		Page:     page,
	}, timeout)
	if err != nil {
		return toolError(err)
	}
	if content == "" {
		content = "book_any"
	}
	return jsonResult(map[string]any{
		"page":     page,
		"content":  content,
		"language": language,
		"results":  books,
	})
}

func BookDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[BookDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	timeout, err := timeoutFromSeconds(params.Arguments.TimeoutSeconds, downloadTimeout)
	if err != nil {
		return toolError(err)
	}
	config, err := env.GetEnv()
	if err != nil {
		return toolError(err)
	}

	book := &anna.Book{
		Hash:   params.Arguments.BookHash,
		Title:  params.Arguments.Title,
		Format: params.Arguments.Format,
	}
	result, err := book.Download(ctx, config.SecretKey, config.DownloadPath, timeout, progressNotifier(ctx, cc, params.GetProgressToken()))
	if err != nil {
		return toolError(err)
	}
	return jsonResult(map[string]any{
		"path":  result.Path,
		"bytes": result.Bytes,
	})
}

func ArticleSearchTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleSearchParams]) (*mcp.CallToolResultFor[any], error) {
	query := strings.TrimSpace(params.Arguments.Query)
	timeout, err := timeoutFromSeconds(params.Arguments.TimeoutSeconds, searchTimeout)
	if err != nil {
		return toolError(err)
	}

	if strings.HasPrefix(query, "10.") {
		paper, err := anna.LookupDOI(ctx, query, timeout)
		if err != nil {
			return toolError(err)
		}
		return jsonResult(paper)
	}

	content, language, page, err := normalizeSearch(params.Arguments.Content, params.Arguments.Language, params.Arguments.Page)
	if err != nil {
		return toolError(err)
	}
	papers, err := anna.FindArticle(ctx, query, anna.SearchOptions{
		Content:  content,
		Language: language,
		Page:     page,
	}, timeout)
	if err != nil {
		return toolError(err)
	}
	if content == "" {
		content = "journal"
	}
	return jsonResult(map[string]any{
		"page":     page,
		"content":  content,
		"language": language,
		"results":  papers,
	})
}

func ArticleDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	timeout, err := timeoutFromSeconds(params.Arguments.TimeoutSeconds, downloadTimeout)
	if err != nil {
		return toolError(err)
	}
	config, err := env.GetEnv()
	if err != nil {
		return toolError(err)
	}

	paper, err := anna.LookupDOI(ctx, params.Arguments.DOI, timeout)
	if err != nil {
		return toolError(err)
	}

	progress := progressNotifier(ctx, cc, params.GetProgressToken())
	if paper.Hash != "" && config.SecretKey != "" {
		book := &anna.Book{
			Hash:  paper.Hash,
			Title: paper.Title,
		}
		result, downloadErr := book.Download(ctx, config.SecretKey, config.DownloadPath, timeout, progress)
		if downloadErr == nil {
			return jsonResult(map[string]any{
				"path":  result.Path,
				"bytes": result.Bytes,
			})
		}
		logger.GetLogger().Warn("Fast download failed, trying SciDB download",
			zap.String("doi", params.Arguments.DOI),
			zap.Error(downloadErr),
		)
	}

	result, err := paper.Download(ctx, config.DownloadPath, timeout, progress)
	if err != nil {
		return toolError(err)
	}
	return jsonResult(map[string]any{
		"path":  result.Path,
		"bytes": result.Bytes,
	})
}

func toolError(err error) (*mcp.CallToolResultFor[any], error) {
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		IsError: true,
	}, nil
}

func jsonResult(value any) (*mcp.CallToolResultFor[any], error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return toolError(err)
	}
	return &mcp.CallToolResultFor[any]{
		Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}},
	}, nil
}

func progressNotifier(ctx context.Context, session *mcp.ServerSession, token any) anna.ProgressFunc {
	if session == nil || token == nil {
		return nil
	}
	return func(done, total int64) {
		_ = session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: token,
			Progress:      float64(done),
			Total:         float64(total),
			Message:       "Downloading",
		})
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func annotate(tool *mcp.ServerTool, readOnly bool) {
	tool.Tool.Annotations = &mcp.ToolAnnotations{
		ReadOnlyHint:    readOnly,
		OpenWorldHint:   boolPtr(true),
		DestructiveHint: boolPtr(false),
	}
}

func newMCPServer() *mcp.Server {
	server := mcp.NewServer("annas-mcp", version.GetVersion(), &mcp.ServerOptions{
		Instructions: serverInstructions,
	})

	bookSearch := mcp.NewServerTool("book_search", "Search for books by title, author, or topic. Returns one page of JSON results, including the MD5 hash used for downloading.", BookSearchTool, mcp.Input(
		mcp.Property("query", mcp.Description("Search query for books (title, author, or topic)")),
		mcp.Property("content", mcp.Description("Optional content filter: book_any, book_fiction, book_nonfiction, magazine, or standards_document")),
		mcp.Property("language", mcp.Description("Optional ISO 639-1 language code, for example en")),
		mcp.Property("page", mcp.Description("Result page, starting at 1")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 60")),
	))
	bookDownload := mcp.NewServerTool("book_download", "Download a book by its MD5 hash into ANNAS_DOWNLOAD_PATH. Returns the file path. Requires ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH.", BookDownloadTool, mcp.Input(
		mcp.Property("hash", mcp.Description("MD5 hash of the book to download")),
		mcp.Property("title", mcp.Description("Book title, used for the filename")),
		mcp.Property("format", mcp.Description("Optional file extension, for example pdf or epub. Detected from the download when omitted")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 1800")),
	))
	articleSearch := mcp.NewServerTool("article_search", "Search for articles by DOI or keywords. A DOI returns one paper. Keywords return one page of JSON results.", ArticleSearchTool, mcp.Input(
		mcp.Property("query", mcp.Description("DOI or search keywords")),
		mcp.Property("content", mcp.Description("Optional content filter. Defaults to journal")),
		mcp.Property("language", mcp.Description("Optional ISO 639-1 language code, for example en")),
		mcp.Property("page", mcp.Description("Result page, starting at 1")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 60")),
	))
	articleDownload := mcp.NewServerTool("article_download", "Download an article by DOI into ANNAS_DOWNLOAD_PATH. Returns the file path. Requires ANNAS_SECRET_KEY and ANNAS_DOWNLOAD_PATH.", ArticleDownloadTool, mcp.Input(
		mcp.Property("doi", mcp.Description("DOI of the article to download")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 1800")),
	))

	annotate(bookSearch, true)
	annotate(articleSearch, true)
	annotate(bookDownload, false)
	annotate(articleDownload, false)
	server.AddTools(bookSearch, bookDownload, articleSearch, articleDownload)
	return server
}

func StartMCPServer(searchDefault, downloadDefault time.Duration) {
	if searchDefault > 0 {
		searchTimeout = searchDefault
	}
	if downloadDefault > 0 {
		downloadTimeout = downloadDefault
	}

	l := logger.GetLogger()
	defer l.Sync()

	l.Info("Starting MCP server",
		zap.String("name", "annas-mcp"),
		zap.String("version", version.GetVersion()),
		zap.String("annasBaseURL", env.GetAnnasBaseURL()),
	)

	server := newMCPServer()
	if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
		l.Fatal("MCP server failed", zap.Error(err))
	}
}
