package modes

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/papers"
)

func TestCLIPaperHelpWorksWithoutConfiguration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"article-search", "article-details", "article-download", "article-read"} {
		t.Run(name, func(t *testing.T) {
			var output, diagnostic bytes.Buffer
			code := executeCLI(t.Context(), []string{name, "--help"}, &output, &diagnostic, func() (*service, error) {
				t.Fatal("help loaded configuration")
				return nil, nil
			})
			if code != 0 || !strings.Contains(output.String(), "OpenAlex") || diagnostic.Len() != 0 {
				t.Fatalf("help failed: code=%d output=%s diagnostic=%s", code, output.String(), diagnostic.String())
			}
		})
	}
}

func TestCLIJSONErrorsKeepStdoutEmpty(t *testing.T) {
	t.Parallel()
	var output, diagnostic bytes.Buffer
	archive := &fakeArchive{}
	// literal: the command itself is the regression input.
	args := []string{"book-search", "example", "--page=-1", "--json-errors"}
	code := executeCLI(t.Context(), args, &output, &diagnostic, func() (*service, error) {
		return &service{archive: archive}, nil
	})
	var failure struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(diagnostic.Bytes(), &failure); err != nil {
		t.Fatalf("error output was not JSON: %s, %v", diagnostic.String(), err)
	}
	if code != 1 || output.Len() != 0 || failure.Code != anna.InvalidArgument || failure.Message == "" || archive.calls != 0 {
		t.Fatalf("incorrect failure: code=%d stdout=%s failure=%+v calls=%d", code, output.String(), failure, archive.calls)
	}
}

func TestCLIWorkRenderingProvidesReplayableSelection(t *testing.T) {
	t.Parallel()
	work := fixtureJSON[papers.Work](t, "cli-paper-work.json")
	var output bytes.Buffer
	if err := printResult(&output, &work, false); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{work.Title, work.OpenAlexID, "article-download", "--location-id"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("work output omitted %q: %s", expected, output.String())
		}
	}
	var encoded bytes.Buffer
	if err := printResult(&encoded, &work, true); err != nil {
		t.Fatal(err)
	}
	var decoded papers.Work
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil || decoded.DownloadArgs["openalex_id"] != work.OpenAlexID {
		t.Fatalf("selection did not survive JSON: %s, %v", encoded.String(), err)
	}
}

func TestCLIPaperOutputPreservesWriterFailures(t *testing.T) {
	t.Parallel()
	work := fixtureJSON[papers.Work](t, "cli-paper-work.json")
	writeErr := errors.New("output unavailable")
	if err := printResult(failingWriter{writeErr}, &work, false); !errors.Is(err, writeErr) {
		t.Fatalf("lost work writer failure: %v", err)
	}
	page := &papers.SearchResult{Provider: "openalex", Results: []*papers.Work{&work}}
	if err := printResult(failingWriter{writeErr}, page, false); !errors.Is(err, writeErr) {
		t.Fatalf("lost page writer failure: %v", err)
	}
}

func TestCLIExampleArgumentsCannotExpandMetadata(t *testing.T) {
	t.Parallel()
	value := "a'$(unsafe)"
	if quoted := shellArgument(value); quoted != "'a'\"'\"'$(unsafe)'" {
		t.Fatalf("unsafe command example: %s", quoted)
	}
}

func TestCLIJSONErrorsWorkBeforeCommandLookup(t *testing.T) {
	t.Parallel()
	var output, diagnostic bytes.Buffer
	args := []string{"--json-errors", "unknown-command"}
	code := executeCLI(t.Context(), args, &output, &diagnostic, func() (*service, error) {
		t.Fatal("unknown command initialized providers")
		return nil, nil
	})
	var failure map[string]any
	if err := json.Unmarshal(diagnostic.Bytes(), &failure); err != nil || code != 1 || output.Len() != 0 {
		t.Fatalf("command lookup error was not machine readable: %s, %v", diagnostic.String(), err)
	}
}
