package modes

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/spf13/cobra"
)

func newSearchCommand(options *cliOptions, article bool) *cobra.Command {
	params := SearchParams{}
	name, description := "book-search", "Search by title, author or topic"
	if article {
		name, description = "article-search", "Find an article by DOI, DOI URL or keywords"
	}
	cmd := &cobra.Command{Use: name + " [query]", Short: description, Args: argumentCount(0, 1)}
	addSearchFlags(cmd, &params.Title, &params.Author, &params.Publisher, &params.Year, &params.Language, &params.Source, &params.Sort, &params.Page, &params.Limit)
	cmd.Flags().StringVar(&params.Format, "format", "", "File extensions, comma-separated, for example epub,pdf; prefix one with - to exclude it")
	cmd.Flags().StringVar(&params.Content, "content", "", "Content types, comma-separated, for example book_nonfiction")
	cmd.Flags().BoolVar(&params.DownloadableOnly, "downloadable-only", false, "Only list files the download commands can fetch")
	cmd.Flags().BoolVar(&params.SearchDescriptions, "search-descriptions", false, "Also match the query against descriptions")
	cmd.Flags().BoolVar(&params.IncludeStats, "include-stats", false, "Include public downloads, quality, report, list and comment counts")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			params.Query = args[0]
		}
		timeout := options.operationTimeout(defaultSearchTimeout)
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
		return printResult(cmd.OutOrStdout(), result, options.json)
	}
	return cmd
}

func newMetadataSearchCommand(options *cliOptions) *cobra.Command {
	params := MetadataSearchParams{}
	cmd := &cobra.Command{Use: "metadata-search [query]", Short: "Search library catalogs for editions, ISBNs and other details", Args: argumentCount(0, 1)}
	addSearchFlags(cmd, &params.Title, &params.Author, &params.Publisher, &params.Year, &params.Language, &params.Source, &params.Sort, &params.Page, &params.Limit)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			params.Query = args[0]
		}
		result, err := options.service.metadataSearch(cmd.Context(), params, options.operationTimeout(defaultSearchTimeout), 0)
		if err != nil {
			return err
		}
		return printResult(cmd.OutOrStdout(), result, options.json)
	}
	return cmd
}

func addSearchFlags(cmd *cobra.Command, title, author, publisher, year, language, source, sort *string, page, limit *int) {
	cmd.Flags().StringVar(title, "title", "", "Phrase the title must contain")
	cmd.Flags().StringVar(author, "author", "", "Phrase the author must contain")
	cmd.Flags().StringVar(publisher, "publisher", "", "Phrase the publisher must contain")
	cmd.Flags().StringVar(year, "year", "", "Publication year, for example 2008")
	cmd.Flags().StringVar(language, "language", "", "Language codes, comma-separated, for example en or en,de")
	cmd.Flags().StringVar(source, "source", "", "Sources, comma-separated, for example lgli,zlib; prefix one with - to exclude it")
	cmd.Flags().StringVar(sort, "sort", "", "relevance, newest, oldest, largest, smallest, newest_added, oldest_added or random")
	cmd.Flags().IntVar(page, "page", 1, "Result page starting at 1")
	cmd.Flags().IntVar(limit, "limit", 0, "Maximum results from this page; 0 prints the whole page")
}

func newBookDetailsCommand(options *cliOptions) *cobra.Command {
	return &cobra.Command{Use: "book-details [hash or id]", Short: "Show identifiers, edition and file problems for a search result", Args: argumentCount(1, 1), RunE: func(cmd *cobra.Command, args []string) error {
		result, err := options.service.details(cmd.Context(), DetailsParams{ID: args[0]}, options.operationTimeout(defaultSearchTimeout))
		if err != nil {
			return err
		}
		return printResult(cmd.OutOrStdout(), result, options.json)
	}}
}

func newAccountStatusCommand(options *cliOptions) *cobra.Command {
	return &cobra.Command{Use: "account-status", Short: "Show the membership and fast downloads left", Args: argumentCount(0, 0), RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := options.service.accountStatus(cmd.Context(), options.operationTimeout(defaultSearchTimeout))
		if err != nil {
			return err
		}
		return printResult(cmd.OutOrStdout(), result, options.json)
	}}
}

func newBookDownloadCommand(options *cliOptions) *cobra.Command {
	return &cobra.Command{Use: "book-download [hash-or-url] [filename]", Short: "Download a book by MD5 hash or Anna's Archive page URL", Args: argumentCount(2, 2), RunE: func(cmd *cobra.Command, args []string) error {
		ext := filepath.Ext(args[1])
		if ext == "" {
			return anna.NewError(anna.InvalidArgument, "filename must include an extension, for example .pdf or .epub")
		}
		params := BookDownloadParams{Hash: args[0], Title: strings.TrimSuffix(filepath.Base(args[1]), ext), Format: strings.TrimPrefix(ext, ".")}
		result, err := options.service.bookDownload(cmd.Context(), params, options.operationTimeout(defaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		return printDownloadResult(cmd.OutOrStdout(), result, "Book", options.json)
	}}
}

func newArticleDownloadCommand(options *cliOptions) *cobra.Command {
	var params ArticleDownloadParams
	cmd := &cobra.Command{Use: "article-download [doi]", Short: "Download an article by DOI, DOI URL or --hash", Args: argumentCount(0, 1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			params.DOI = args[0]
		}
		result, err := options.service.articleDownload(cmd.Context(), params, options.operationTimeout(defaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		return printDownloadResult(cmd.OutOrStdout(), result, "Article", options.json)
	}}
	cmd.Flags().StringVar(&params.Hash, "hash", "", "32-character MD5 or Anna's Archive /md5/ page URL; use instead of a DOI")
	cmd.Flags().StringVar(&params.Title, "title", "", "Optional filename title")
	cmd.Flags().StringVar(&params.Format, "format", "", "Optional filename extension; does not convert files")
	return cmd
}

func printResult(w io.Writer, result any, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(result)
	}
	switch result := result.(type) {
	case *anna.Paper:
		_, err := fmt.Fprintln(w, renderRecord(result))
		return err
	case *anna.Details:
		_, err := fmt.Fprintln(w, renderRecord(result))
		return err
	case *anna.AccountStatus:
		_, err := fmt.Fprintln(w, renderRecord(result))
		return err
	case searchResult[*anna.Paper]:
		return printSearchPage(w, result, "Article", "No articles found.")
	case searchResult[*anna.Book]:
		return printSearchPage(w, result, "Book", "No books found.")
	case searchResult[*anna.MetadataRecord]:
		return printSearchPage(w, result, "Record", "No catalog records found.")
	default:
		return fmt.Errorf("unsupported result %T", result)
	}
}

func printSearchPage[T any](w io.Writer, page searchResult[T], label, empty string) error {
	if len(page.Results) == 0 {
		if _, err := fmt.Fprintln(w, empty); err != nil {
			return err
		}
	}
	for i, item := range page.Results {
		if _, err := fmt.Fprintf(w, "%s %d:\n%s\n\n", label, i+1, renderRecord(item)); err != nil {
			return err
		}
	}
	if len(page.PartialMatches) > 0 {
		if _, err := fmt.Fprintf(w, "Partial matches (looser, and may ignore filters):\n\n"); err != nil {
			return err
		}
		for i, item := range page.PartialMatches {
			if _, err := fmt.Fprintf(w, "Partial %d:\n%s\n\n", i+1, renderRecord(item)); err != nil {
				return err
			}
		}
	}
	if len(page.Results) == 0 {
		return nil
	}
	total := fmt.Sprint(page.Total)
	if page.TotalIsLowerBound {
		total += "+"
	}
	more := ""
	if page.MorePages {
		more = fmt.Sprintf("; use --page %d for more", page.Page+1)
	}
	_, err := fmt.Fprintf(w, "Page %d: showing %d of %s results%s.\n", page.Page, len(page.Results), total, more)
	return err
}

func printDownloadResult(w io.Writer, result anna.DownloadResult, label string, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(result)
	}
	left := ""
	if result.DownloadsLeft != nil {
		left = fmt.Sprintf(" (%d fast downloads left)", *result.DownloadsLeft)
	}
	_, err := fmt.Fprintf(w, "%s downloaded successfully to: %s%s\n", label, result.Path, left)
	return err
}
