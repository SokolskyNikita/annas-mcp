package modes

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/spf13/cobra"
)

func newSearchCommand(options *cliOptions, article bool) *cobra.Command {
	params := SearchParams{}
	name, description := "book-search", "Search by title, author, or topic"
	if article {
		name, description = "article-search", "Find an article by DOI, DOI URL, or keywords"
	}
	cmd := &cobra.Command{Use: name + " [query]", Short: description, Args: argumentCount(1, 1)}
	cmd.Flags().IntVar(&params.Page, "page", 1, "Result page starting at 1")
	cmd.Flags().IntVar(&params.Limit, "limit", 0, "Maximum results from this page; 0 prints the whole page")
	cmd.Flags().StringVar(&params.Language, "language", "", "Two-letter language code, for example en")
	cmd.Flags().StringVar(&params.Content, "content", "", "Content filter, for example book_nonfiction")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		params.Query = args[0]
		timeout := options.operationTimeout(anna.DefaultSearchTimeout)
		var result any
		var err error
		if article {
			result, err = options.service.articleSearch(cmd.Context(), params, timeout, 0)
		} else {
			result, err = options.service.bookSearch(cmd.Context(), params, timeout, 0)
		}
		if err != nil {
			return err
		}
		if options.json {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return printSearchResult(cmd.OutOrStdout(), result)
	}
	return cmd
}

func newBookDownloadCommand(options *cliOptions) *cobra.Command {
	return &cobra.Command{Use: "book-download [hash] [filename]", Short: "Download a book by its MD5 hash", Args: argumentCount(2, 2), RunE: func(cmd *cobra.Command, args []string) error {
		ext := filepath.Ext(args[1])
		if ext == "" {
			return apperr.New(apperr.InvalidArgument, "filename must include an extension, for example .pdf or .epub")
		}
		params := BookDownloadParams{Hash: args[0], Title: strings.TrimSuffix(filepath.Base(args[1]), ext), Format: strings.TrimPrefix(ext, ".")}
		result, err := options.service.bookDownload(cmd.Context(), params, options.operationTimeout(anna.DefaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		return printDownloadResult(cmd.OutOrStdout(), result, "Book", options.json)
	}}
}

func newArticleDownloadCommand(options *cliOptions) *cobra.Command {
	var params ArticleDownloadParams
	cmd := &cobra.Command{Use: "article-download [doi]", Short: "Download an article by DOI, DOI URL, or --hash", Args: argumentCount(0, 1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			params.DOI = args[0]
		}
		result, err := options.service.articleDownload(cmd.Context(), params, options.operationTimeout(anna.DefaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		return printDownloadResult(cmd.OutOrStdout(), result, "Article", options.json)
	}}
	cmd.Flags().StringVar(&params.Hash, "hash", "", "32-character hash from search; use instead of a DOI")
	cmd.Flags().StringVar(&params.Title, "title", "", "Optional filename title")
	cmd.Flags().StringVar(&params.Format, "format", "", "Optional filename extension; does not convert files")
	return cmd
}

func printSearchResult(w io.Writer, result any) error {
	switch result := result.(type) {
	case *anna.Paper:
		_, err := fmt.Fprintln(w, result)
		return err
	case searchResult[*anna.Paper]:
		return printSearchPage(w, result.Results, "Article", "No articles found.")
	case searchResult[*anna.Book]:
		return printSearchPage(w, result.Results, "Book", "No books found.")
	default:
		return fmt.Errorf("unsupported search result %T", result)
	}
}

func printSearchPage[T fmt.Stringer](w io.Writer, items []T, label, empty string) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(w, empty)
		return err
	}
	for i, item := range items {
		if _, err := fmt.Fprintf(w, "%s %d:\n%s\n\n", label, i+1, item); err != nil {
			return err
		}
	}
	return nil
}

func printDownloadResult(w io.Writer, result anna.DownloadResult, label string, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(result)
	}
	_, err := fmt.Fprintf(w, "%s downloaded successfully to: %s\n", label, result.Path)
	return err
}
