package modes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"github.com/SokolskyNikita/annas-mcp/internal/version"
	"github.com/joho/godotenv"
	"github.com/spf13/cobra"
)

type serviceFactory func() (*service, error)

// RunCLI owns process setup and returns an exit code after deferred cleanup.
func RunCLI() int {
	defer logger.GetLogger().Sync()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return executeCLI(ctx, os.Args[1:], os.Stdout, os.Stderr, loadCLIService)
}

func loadCLIService() (*service, error) {
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, apperr.Wrap(apperr.Config, "could not load .env", err)
	}
	return loadService()
}

func executeCLI(ctx context.Context, args []string, stdout, stderr io.Writer, factory serviceFactory) int {
	root := newRootCommand(factory)
	root.SetArgs(append([]string{}, args...))
	root.SetOut(stdout)
	root.SetErr(stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(stderr, codedError(err))
		return 1
	}
	return 0
}

type cliOptions struct {
	timeout time.Duration
	json    bool
	service *service
}

func (o *cliOptions) operationTimeout(fallback time.Duration) time.Duration {
	if o.timeout > 0 {
		return o.timeout
	}
	return fallback
}

func newRootCommand(factory serviceFactory) *cobra.Command {
	options := &cliOptions{}
	root := &cobra.Command{
		Use: "annas-mcp", Short: "Search and download books and articles from Anna's Archive",
		Version: version.GetVersion(), SilenceErrors: true, SilenceUsage: true,
		Args:              argumentCount(0, 0),
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE:              func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		// Help and version commands must work without valid credentials or .env.
		if cmd == root || cmd.Name() == "help" {
			return nil
		}
		if cmd.Flags().Changed("timeout") && (options.timeout <= 0 || options.timeout > maxOperationTimeout) {
			return apperr.New(apperr.InvalidArgument, "timeout must be greater than zero and no more than 24h")
		}
		var err error
		options.service, err = factory()
		return err
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return apperr.Wrap(apperr.InvalidArgument, "invalid command option", err)
	})
	root.PersistentFlags().DurationVar(&options.timeout, "timeout", 0, "Total operation timeout, including retries (default search: 60s; download: 30m; maximum: 24h)")
	root.PersistentFlags().BoolVar(&options.json, "json", false, "Print structured JSON results")
	root.AddCommand(
		newSearchCommand(options, false), newSearchCommand(options, true),
		newBookDownloadCommand(options), newArticleDownloadCommand(options),
		&cobra.Command{Use: "mcp", Short: "Start the MCP server over stdin/stdout", Args: argumentCount(0, 0), RunE: func(cmd *cobra.Command, _ []string) error {
			return runMCP(cmd.Context(), options.service, options.operationTimeout(anna.DefaultSearchTimeout), options.operationTimeout(anna.DefaultDownloadTimeout))
		}},
	)
	return root
}

func argumentCount(min, max int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := cobra.RangeArgs(min, max)(cmd, args); err != nil {
			return apperr.Wrap(apperr.InvalidArgument, "invalid arguments", err)
		}
		return nil
	}
}
