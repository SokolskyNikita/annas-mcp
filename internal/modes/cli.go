package modes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"github.com/SokolskyNikita/annas-mcp/internal/version"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

func StartCLI() {
	os.Exit(RunCLI())
}

// RunCLI owns process setup. Returning an exit code allows deferred cleanup.
func RunCLI() int {
	defer logger.GetLogger().Sync()
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, codedError(apperr.Wrap(apperr.Config, "could not load .env", err)))
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newRootCommand(loadService).ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, codedError(err))
		return 1
	}
	return 0
}

func newRootCommand(factory func() (*service, error)) *cobra.Command {
	var svc *service
	var timeout time.Duration
	var asJSON bool
	root := &cobra.Command{
		Use: "annas-mcp", Short: "Search and download books and articles from Anna's Archive",
		Version: version.GetVersion(), SilenceErrors: true, SilenceUsage: true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.Flags().Changed("timeout") && (timeout <= 0 || timeout > maxOperationTimeout) {
				return apperr.New(apperr.InvalidArgument, "timeout must be greater than zero and no more than 24h")
			}
			var err error
			svc, err = factory()
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return apperr.Wrap(apperr.InvalidArgument, "invalid command option", err)
	})
	root.PersistentFlags().DurationVar(&timeout, "timeout", 0, "Total operation timeout, including retries (default search: 60s; download: 30m; maximum: 24h)")
	root.PersistentFlags().BoolVar(&asJSON, "json", false, "Print structured JSON results")
	operationTimeout := func(fallback time.Duration) time.Duration {
		if timeout > 0 {
			return timeout
		}
		return fallback
	}
	writeJSON := func(cmd *cobra.Command, value any) error { return json.NewEncoder(cmd.OutOrStdout()).Encode(value) }
	argsCount := func(min, max int) cobra.PositionalArgs {
		return func(cmd *cobra.Command, args []string) error {
			if err := cobra.RangeArgs(min, max)(cmd, args); err != nil {
				return apperr.Wrap(apperr.InvalidArgument, "invalid arguments", err)
			}
			return nil
		}
	}

	for _, article := range []bool{false, true} {
		params := SearchParams{}
		name, label := "book-search", "Book"
		if article {
			name, label = "article-search", "Article"
		}
		cmd := &cobra.Command{Use: name + " [query]", Short: "Search by title, author, or topic", Args: argsCount(1, 1)}
		if article {
			cmd.Short = "Find an article by DOI, DOI URL, or keywords"
		}
		cmd.Flags().IntVar(&params.Page, "page", 1, "Result page starting at 1")
		cmd.Flags().IntVar(&params.Limit, "limit", 0, "Maximum results from this page; 0 prints the whole page")
		cmd.Flags().StringVar(&params.Language, "language", "", "Two-letter language code, for example en")
		cmd.Flags().StringVar(&params.Content, "content", "", "Content filter, for example book_nonfiction")
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			params.Query = args[0]
			var result any
			var err error
			if article {
				result, err = svc.articleSearch(cmd.Context(), params, operationTimeout(anna.DefaultSearchTimeout), 0)
			} else {
				result, err = svc.bookSearch(cmd.Context(), params, operationTimeout(anna.DefaultSearchTimeout), 0)
			}
			if err != nil {
				return err
			}
			if asJSON {
				return writeJSON(cmd, result)
			}
			switch value := result.(type) {
			case *anna.Paper:
				fmt.Fprintln(cmd.OutOrStdout(), value.String())
			case searchResult[*anna.Paper]:
				if len(value.Results) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No articles found.")
				}
				for i, item := range value.Results {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %d:\n%s\n\n", label, i+1, item.String())
				}
			case searchResult[*anna.Book]:
				if len(value.Results) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No books found.")
				}
				for i, item := range value.Results {
					fmt.Fprintf(cmd.OutOrStdout(), "%s %d:\n%s\n\n", label, i+1, item.String())
				}
			}
			return nil
		}
		root.AddCommand(cmd)
	}
	bookDownload := &cobra.Command{Use: "book-download [hash] [filename]", Short: "Download a book by its MD5 hash", Args: argsCount(2, 2), RunE: func(cmd *cobra.Command, args []string) error {
		ext := filepath.Ext(args[1])
		if ext == "" {
			return apperr.New(apperr.InvalidArgument, "filename must include an extension, for example .pdf or .epub")
		}
		result, err := svc.bookDownload(cmd.Context(), BookDownloadParams{Hash: args[0], Title: strings.TrimSuffix(filepath.Base(args[1]), ext), Format: strings.TrimPrefix(ext, ".")}, operationTimeout(anna.DefaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, result)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Book downloaded successfully to: %s\n", result.Path)
		return nil
	}}
	root.AddCommand(bookDownload)

	var articleParams ArticleDownloadParams
	articleDownload := &cobra.Command{Use: "article-download [doi]", Short: "Download an article by DOI, DOI URL, or --hash", Args: argsCount(0, 1), RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			articleParams.DOI = args[0]
		}
		result, err := svc.articleDownload(cmd.Context(), articleParams, operationTimeout(anna.DefaultDownloadTimeout), nil)
		if err != nil {
			return err
		}
		if asJSON {
			return writeJSON(cmd, result)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Article downloaded successfully to: %s\n", result.Path)
		return nil
	}}
	articleDownload.Flags().StringVar(&articleParams.Hash, "hash", "", "32-character hash from search; use instead of a DOI")
	articleDownload.Flags().StringVar(&articleParams.Title, "title", "", "Optional filename title")
	articleDownload.Flags().StringVar(&articleParams.Format, "format", "", "Optional filename extension; does not convert files")
	root.AddCommand(articleDownload)
	root.AddCommand(&cobra.Command{Use: "mcp", Short: "Start the MCP server over stdin/stdout", Args: argsCount(0, 0), RunE: func(cmd *cobra.Command, args []string) error {
		return runMCP(cmd.Context(), svc, operationTimeout(anna.DefaultSearchTimeout), operationTimeout(anna.DefaultDownloadTimeout))
	}})
	return root
}
