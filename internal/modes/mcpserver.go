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

const serverInstructions = "Use these tools when the user needs the full text of a specific book, paper, standard, or citation. Search before downloading. article_search with keywords searches journal articles and returns hash, description, and doi when the page includes one. book_search searches books. Pass hash to book_download or article_download, or pass doi to article_download. Results default to 10 hits; raise limit when the match is not among them, and increase page only when that page is exhausted. language is an ISO 639-1 code such as en. Book content filters include book_fiction, book_nonfiction, magazine, and standards_document. Downloads are written under ANNAS_DOWNLOAD_PATH and the tool result includes the file path. Errors start with a code such as [NOT_FOUND] or [INVALID_ARGUMENT]. A DOI that does not resolve is an error."

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

	resolved := anna.ResolveSearch(anna.SearchOptions{
		Content:  content,
		Language: language,
		Page:     page,
	}, "book_any")
	books, err := anna.FindBook(ctx, params.Arguments.Query, resolved, timeout)
	if err != nil {
		return toolError(err)
	}
	limited, matched, limit := limitResults(books, params.Arguments.Limit)
	payload := map[string]any{
		"page":     resolved.Page,
		"language": resolved.Language,
		"matched":  matched,
		"limit":    limit,
		"results":  limited,
	}
	if resolved.Content != "" {
		payload["content"] = resolved.Content
	}
	return jsonResult(payload)
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
	resolved := anna.ResolveSearch(anna.SearchOptions{
		Content:  content,
		Language: language,
		Page:     page,
	}, "journal")
	papers, err := anna.FindArticle(ctx, query, resolved, timeout)
	if err != nil {
		return toolError(err)
	}
	limited, matched, limit := limitResults(papers, params.Arguments.Limit)
	payload := map[string]any{
		"page":     resolved.Page,
		"language": resolved.Language,
		"matched":  matched,
		"limit":    limit,
		"results":  limited,
	}
	if resolved.Index != "" {
		payload["index"] = resolved.Index
	}
	if resolved.Content != "" {
		payload["content"] = resolved.Content
	}
	return jsonResult(payload)
}

func ArticleDownloadTool(ctx context.Context, cc *mcp.ServerSession, params *mcp.CallToolParamsFor[ArticleDownloadParams]) (*mcp.CallToolResultFor[any], error) {
	timeout, err := timeoutFromSeconds(params.Arguments.TimeoutSeconds, downloadTimeout)
	if err != nil {
		return toolError(err)
	}
	hash := strings.TrimSpace(params.Arguments.Hash)
	doi := strings.TrimSpace(params.Arguments.DOI)
	if hash == "" && doi == "" {
		return toolError(errors.New("pass doi or hash"))
	}
	config, err := env.GetEnv()
	if err != nil {
		return toolError(err)
	}
	if hash != "" {
		book := &anna.Book{
			Hash:   hash,
			Title:  params.Arguments.Title,
			Format: params.Arguments.Format,
		}
		result, downloadErr := book.Download(ctx, config.SecretKey, config.DownloadPath, timeout, progressNotifier(ctx, cc, params.GetProgressToken()))
		if downloadErr != nil {
			return toolError(downloadErr)
		}
		return jsonResult(map[string]any{
			"path":  result.Path,
			"bytes": result.Bytes,
		})
	}

	paper, err := anna.LookupDOI(ctx, doi, timeout)
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
			zap.String("doi", doi),
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
		Content: []mcp.Content{&mcp.TextContent{Text: codedError(err)}},
		IsError: true,
	}, nil
}

func jsonResult(value any) (*mcp.CallToolResultFor[any], error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return toolError(err)
	}
	return &mcp.CallToolResultFor[any]{
		Content:           []mcp.Content{&mcp.TextContent{Text: string(raw)}},
		StructuredContent: value,
	}, nil
}

const defaultSearchLimit = 10

func limitResults[T any](items []T, limit int) ([]T, int, int) {
	matched := len(items)
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	if limit > matched {
		limit = matched
	}
	return items[:limit], matched, limit
}

func codedError(err error) string {
	if err == nil {
		return "[UPSTREAM] unknown error"
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "[") {
		return msg
	}
	lower := strings.ToLower(msg)
	code := "UPSTREAM"
	switch {
	case strings.Contains(lower, "403"),
		strings.Contains(lower, "annas_account_cookie"):
		code = "UPSTREAM_BLOCKED"
	case strings.Contains(lower, "annas_secret_key"),
		strings.Contains(lower, "annas_download_path"),
		strings.Contains(lower, "environment variable"):
		code = "CONFIG"
	case strings.Contains(lower, "no paper found"),
		strings.Contains(lower, "no books found"),
		strings.Contains(lower, "no articles found"):
		code = "NOT_FOUND"
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "context deadline"),
		strings.Contains(lower, "context canceled"):
		code = "REQUEST_TIMEOUT"
	case strings.Contains(lower, "must be"),
		strings.Contains(lower, "must contain"),
		strings.Contains(lower, "pass doi"),
		strings.Contains(lower, "is empty"),
		strings.Contains(lower, "greater than"):
		code = "INVALID_ARGUMENT"
	}
	return fmt.Sprintf("[%s] %s", code, msg)
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

	bookSearch := mcp.NewServerTool("book_search", "Use when the user needs a book, textbook, manual, or standard by title, author, or topic. Returns one page of JSON with hash, format, and a short description. Pass hash to book_download.", BookSearchTool, mcp.Input(
		mcp.Property("query", mcp.Description("Search query for books (title, author, or topic)")),
		mcp.Property("content", mcp.Description("Optional content filter: book_fiction, book_nonfiction, magazine, or standards_document")),
		mcp.Property("language", mcp.Description("Optional ISO 639-1 language code, for example en")),
		mcp.Property("page", mcp.Description("Result page, starting at 1")),
		mcp.Property("limit", mcp.Description("Maximum hits to return from this page. Defaults to 10")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 60")),
	))
	bookDownload := mcp.NewServerTool("book_download", "Download a book the user asked for. Pass hash from book_search. Writes the file under ANNAS_DOWNLOAD_PATH and returns its path.", BookDownloadTool, mcp.Input(
		mcp.Property("hash", mcp.Description("MD5 hash of the book to download")),
		mcp.Property("title", mcp.Description("Book title, used for the filename")),
		mcp.Property("format", mcp.Description("Optional file extension, for example pdf or epub. Detected from the download when omitted")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 1800")),
	))
	articleSearch := mcp.NewServerTool("article_search", "Use when the user needs a paper or article, by DOI or by title. A DOI returns one paper. Keywords search journal articles and return hash, description, and doi when the page includes one. Pass hash or doi to article_download.", ArticleSearchTool, mcp.Input(
		mcp.Property("query", mcp.Description("DOI or search keywords")),
		mcp.Property("content", mcp.Description("Optional file-type filter. Keywords use the journals index unless a book content filter is set")),
		mcp.Property("language", mcp.Description("Optional ISO 639-1 language code, for example en")),
		mcp.Property("page", mcp.Description("Result page, starting at 1")),
		mcp.Property("limit", mcp.Description("Maximum hits to return from this page. Defaults to 10")),
		mcp.Property("timeout_seconds", mcp.Description("Optional HTTP timeout in seconds. Defaults to 60")),
	))
	articleDownload := mcp.NewServerTool("article_download", "Download a paper the user asked for. Pass doi, or hash from article_search. Writes the file under ANNAS_DOWNLOAD_PATH and returns its path.", ArticleDownloadTool, mcp.Input(
		mcp.Property("doi", mcp.Description("DOI of the article to download. Optional when hash is set")),
		mcp.Property("hash", mcp.Description("MD5 hash from article_search. Optional when doi is set")),
		mcp.Property("title", mcp.Description("Optional title, used for the filename when downloading by hash")),
		mcp.Property("format", mcp.Description("Optional file extension, for example pdf. Detected from the download when omitted")),
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
