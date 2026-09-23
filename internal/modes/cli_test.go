package modes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/internal/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func TestCLIValidationAndDownloadOptions(t *testing.T) {
	t.Parallel()
	archive := &fakeArchive{}
	for _, args := range [][]string{{"book-search", "--page=-1", "example"}, {"book-search", "--limit=-1", "example"}, {"book-search", "--timeout=25h", "example"}, {"article-download"}} {
		cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
		cmd.SetArgs(args)
		if err := cmd.ExecuteContext(context.Background()); apperr.CodeOf(err) != apperr.InvalidArgument {
			t.Fatalf("args %v: %v", args, err)
		}
	}
	if archive.calls != 0 {
		t.Fatal("invalid CLI input reached upstream")
	}
	var output bytes.Buffer
	cmd := newRootCommand(func() (*service, error) { return &service{archive: archive}, nil })
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"article-download", "--hash", "0123456789abcdef0123456789abcdef", "--title", "Example", "--format", "pdf", "--json"})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if archive.article.Title != "Example" || archive.article.Format != "pdf" || archive.article.Hash == "" {
		t.Fatalf("wrong options: %+v", archive.article)
	}
	var result anna.DownloadResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || result.Bytes != 123 {
		t.Fatalf("invalid download output: %s, %v", output.String(), err)
	}
}

func TestCLIHelpAndVersionDoNotLoadConfiguration(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{nil, {"--help"}, {"--version"}, {"help", "article-search"}} {
		var out, diagnostic bytes.Buffer
		status := executeCLI(t.Context(), args, &out, &diagnostic, func() (*service, error) {
			t.Fatal("help loaded configuration")
			return nil, nil
		})
		if status != 0 || out.Len() == 0 || diagnostic.Len() != 0 {
			t.Fatalf("%v: status=%d stdout=%q stderr=%q", args, status, out.String(), diagnostic.String())
		}
	}
}

type cliCase struct {
	Name     string
	Args     []string
	Contains string
}

func TestCLICommandsPrintResultsAndPreserveErrors(t *testing.T) {
	t.Parallel()
	data := fixtureJSON[records](t, "records.json")
	for _, tc := range fixtureJSON[[]cliCase](t, "cli-cases.json") {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			for _, upstreamFailure := range []bool{false, true} {
				archive := &fakeArchive{books: data.Books, papers: data.Papers}
				if upstreamFailure {
					archive.failure = apperr.New(apperr.UpstreamBlocked, "access denied")
				}
				var out, diagnostic bytes.Buffer
				status := executeCLI(t.Context(), tc.Args, &out, &diagnostic, func() (*service, error) {
					return &service{archive: archive}, nil
				})
				if upstreamFailure {
					if status != 1 || !strings.HasPrefix(diagnostic.String(), "[UPSTREAM_BLOCKED]") || out.Len() != 0 {
						t.Fatalf("failure: status=%d stdout=%q stderr=%q", status, out.String(), diagnostic.String())
					}
				} else if status != 0 || !strings.Contains(out.String(), tc.Contains) || diagnostic.Len() != 0 {
					t.Fatalf("success: status=%d stdout=%q stderr=%q", status, out.String(), diagnostic.String())
				}
			}
		})
	}
}

func TestCLIReportsInvalidCommandInputs(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"unknown-command"},
		{"book-search"},
		{"book-search", "example", "--nonexistent"},
		{"book-search", "example", "--timeout", "not-a-duration"},
		{"book-search", "example", "--timeout", "0"},
		{"book-download", "hash", "filename-without-extension"},
	}
	for _, args := range cases {
		var diagnostic bytes.Buffer
		archive := &fakeArchive{}
		status := executeCLI(t.Context(), args, io.Discard, &diagnostic, func() (*service, error) {
			return &service{archive: archive}, nil
		})
		if status != 1 || !strings.HasPrefix(diagnostic.String(), "[INVALID_ARGUMENT]") || archive.calls != 0 {
			t.Fatalf("%v: status=%d stderr=%q calls=%d", args, status, diagnostic.String(), archive.calls)
		}
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCLIPropagatesOutputFailures(t *testing.T) {
	t.Parallel()
	data := fixtureJSON[records](t, "records.json")
	writeErr := errors.New("output unavailable")
	for _, tc := range fixtureJSON[[]cliCase](t, "cli-cases.json") {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			command := newRootCommand(func() (*service, error) {
				return &service{archive: &fakeArchive{books: data.Books, papers: data.Papers}}, nil
			})
			command.SetArgs(tc.Args)
			command.SetOut(failingWriter{writeErr})
			if err := command.ExecuteContext(t.Context()); !errors.Is(err, writeErr) {
				t.Fatalf("lost output error: %v", err)
			}
		})
	}
}

func TestCLIEmptySearchResultsAndConfigurationFailure(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"book-search", "article-search"} {
		var out, diagnostic bytes.Buffer
		status := executeCLI(t.Context(), []string{command, "nothing"}, &out, &diagnostic, func() (*service, error) {
			return &service{archive: &fakeArchive{}}, nil
		})
		if status != 0 || !strings.Contains(out.String(), "found.") {
			t.Fatalf("empty result: %s %s", out.String(), diagnostic.String())
		}
	}
	var diagnostic bytes.Buffer
	status := executeCLI(t.Context(), []string{"book-search", "example"}, io.Discard, &diagnostic, func() (*service, error) {
		return nil, apperr.New(apperr.Config, "missing configuration")
	})
	if status != 1 || !strings.HasPrefix(diagnostic.String(), "[CONFIG]") {
		t.Fatalf("configuration: %s", diagnostic.String())
	}
}

func TestLoadCLIServiceHandlesEnvironmentFiles(t *testing.T) {
	invalid := fixture(t, "invalid.env")
	t.Chdir(t.TempDir())
	for _, key := range []string{"ANNAS_SECRET_KEY", "ANNAS_DOWNLOAD_PATH", "ANNAS_BASE_URL", "ANNAS_AUTO_BASE_URL", "ANNAS_ACCOUNT_COOKIE"} {
		t.Setenv(key, "")
	}
	t.Setenv("ANNAS_AUTO_BASE_URL", "false")
	if svc, err := loadCLIService(); err != nil || svc == nil {
		t.Fatalf("missing optional .env: %v", err)
	}
	t.Setenv("ANNAS_BASE_URL", "http://unsafe.example")
	if _, err := loadCLIService(); apperr.CodeOf(err) != apperr.Config {
		t.Fatalf("invalid config: %v", err)
	}
	if err := os.WriteFile(".env", invalid, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadCLIService(); apperr.CodeOf(err) != apperr.Config {
		t.Fatalf("malformed .env: %v", err)
	}
}
