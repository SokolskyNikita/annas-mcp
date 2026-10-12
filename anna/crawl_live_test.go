package anna

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// crawlDelay spaces the crawl's searches. Anna's code allows members 180
// searches an hour; the crawl stays well under that.
const crawlDelay = 20 * time.Second

// crawlSearches are the searches in testdata/crawl_searches.json.
func crawlSearches(t *testing.T) []crawlSearch {
	t.Helper()
	var file struct {
		Comment  string
		Searches []crawlSearch
	}
	decodeFixture(t, "crawl_searches.json", &file)
	return file.Searches
}

// crawlSearch is one search of the crawl.
type crawlSearch struct {
	Query   string
	Options SearchOptions
}

// TestCrawlSearchesLoad keeps the crawl's search list readable, since the
// crawl itself only runs by hand.
func TestCrawlSearchesLoad(t *testing.T) {
	t.Parallel()

	for _, search := range crawlSearches(t) {
		if search.Query == "" {
			t.Errorf("search without a query: %+v", search)
		}
	}
}

// TestCrawlCorpus adds the raw text of real search cards to
// internal/parse/testdata/corpus, for TestCorpus in package parse. It signs in with
// ANNAS_SECRET_KEY and searches about 100 times, one search every 20
// seconds, so it takes about 40 minutes; it downloads nothing. Afterwards,
// run TestCorpus with -update and review what the parsers make of the new
// lines.
func TestCrawlCorpus(t *testing.T) {
	if os.Getenv("ANNAS_MCP_CRAWL") != "1" {
		t.Skip("set ANNAS_MCP_CRAWL=1 to add real search cards to internal/parse/testdata/corpus")
	}
	key := liveSetting("ANNAS_SECRET_KEY")
	if key == "" {
		t.Fatal("provide ANNAS_SECRET_KEY, in the environment or the repository .env, to crawl")
	}
	client := mustClient(Config{BaseURL: "https://annas-archive.gl", AutoBaseURL: true, SecretKey: key})
	ctx := context.Background()
	if err := client.requireArchiveAccess(ctx); err != nil {
		t.Fatal(err)
	}
	corpus := map[string]map[string]bool{"metadata_lines": {}, "authors": {}, "publishers": {}, "titles": {}}
	searches := crawlSearches(t)
	for i, search := range searches {
		if i > 0 {
			time.Sleep(crawlDelay)
		}
		var texts []parse.CardText
		err := client.onMirror(ctx, func(base string) error {
			doc, _, err := client.fetchDocument(ctx, buildSearchURL(base, search.Query, ResolveSearch(search.Options, "")))
			if err == nil {
				texts = parse.CardTexts(doc)
			}
			return err
		})
		if err != nil {
			t.Logf("search %d of %d (%q %+v): %v", i+1, len(searches), search.Query, search.Options, err)
			continue
		}
		for _, text := range texts {
			addCorpusLine(corpus["metadata_lines"], text.MetadataLine)
			addCorpusLine(corpus["authors"], text.Authors)
			if line := oneLine(text.PublisherLine); line != "" {
				pair, _ := json.Marshal([2]string{line, text.PublisherLink})
				corpus["publishers"][string(pair)] = true
			}
			// Titles are only interesting when they carry entities or markup.
			if strings.ContainsAny(text.Title, "&<") {
				addCorpusLine(corpus["titles"], text.Title)
			}
		}
		t.Logf("search %d of %d: %d cards", i+1, len(searches), len(texts))
	}
	for name, lines := range corpus {
		if err := mergeCorpus(filepath.Join("internal", "parse", "testdata", "corpus", name+".txt.gz"), lines); err != nil {
			t.Fatal(err)
		}
	}
}

func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func addCorpusLine(lines map[string]bool, text string) {
	if line := oneLine(text); line != "" {
		lines[line] = true
	}
}

// mergeCorpus adds lines to a gzipped corpus file, one per line, sorted,
// keeping the lines already there.
func mergeCorpus(path string, lines map[string]bool) error {
	if existing, err := os.ReadFile(path); err == nil {
		reader, err := gzip.NewReader(bytes.NewReader(existing))
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(reader)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			if line := scanner.Text(); line != "" {
				lines[line] = true
			}
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	sorted := make([]string, 0, len(lines))
	for line := range lines {
		sorted = append(sorted, line)
	}
	sort.Strings(sorted)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeGzip(path, []byte(strings.Join(sorted, "\n")+"\n"))
}
