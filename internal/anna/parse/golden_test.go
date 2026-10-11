package parse

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the expected outputs in testdata/pages from the current parsers")

// pageBase is the mirror captured pages are read as coming from.
const pageBase = "https://annas-archive.gl"

// capturedPage is one page in testdata/pages/manifest.json.
type capturedPage struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	Path string `json:"path,omitempty"`
	DOI  string `json:"doi,omitempty"`
	Page int    `json:"page,omitempty"`
}

func (p capturedPage) file() string {
	if p.Kind == "csl" {
		return p.Name + ".json"
	}
	return p.Name + ".html.gz"
}

func readManifest(t testing.TB) []capturedPage {
	t.Helper()
	var manifest struct {
		Pages []capturedPage `json:"pages"`
	}
	if err := json.Unmarshal(readPage(t, "manifest.json"), &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.Pages
}

// TestCapturedPages reads every captured page and compares what the parsers
// make of it with the reviewed output in <name>.golden.json. After a capture
// or a parser change, run with -update and review the diff.
func TestCapturedPages(t *testing.T) {
	for _, page := range readManifest(t) {
		t.Run(page.Name, func(t *testing.T) {
			got, err := json.MarshalIndent(pageOutput(t, page), "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, '\n')
			name := page.Name + ".golden.json"
			if *update {
				if err := os.WriteFile(filepath.Join("testdata", "pages", name), got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			if want := readPage(t, name); !bytes.Equal(got, want) {
				t.Errorf("%s changed; if the change is right, rerun with -update and review the diff:\n%s", name, firstDifference(got, want))
			}
		})
	}
}

func pageOutput(t *testing.T, page capturedPage) any {
	t.Helper()
	if page.Kind == "csl" {
		citation, err := ParseCitation(bytes.NewReader(readPage(t, page.file())))
		if err != nil {
			t.Fatal(err)
		}
		return citation
	}
	doc := pageDocument(t, page.file())
	switch page.Kind {
	case "books", "articles", "metadata":
		total, lowerBound, found := ResultsTotal(doc)
		// literal: the field names of the expected output.
		return map[string]any{
			"total":         map[string]any{"total": total, "lower_bound": lowerBound, "found": found},
			"shows_results": ShowsResults(doc, max(page.Page, 1)),
			"facets":        map[string][]string{"src": FacetValues(doc, "src"), "content": FacetValues(doc, "content")},
			"cards":         SearchCards(doc),
		}
	case "record":
		return map[string]any{"identity": RecordIdentity(doc), "details": ReadRecordDetails(doc)}
	case "scidb":
		downloadURL, hash, err := SciDBFile(doc, pageBase+page.Path)
		output := map[string]string{"download_url": downloadURL, "hash": hash}
		if err != nil {
			output["error"] = err.Error()
		}
		return output
	}
	t.Fatalf("unknown page kind %q", page.Kind)
	return nil
}

// firstDifference shows the first differing line of two outputs.
func firstDifference(got, want []byte) string {
	gotLines, wantLines := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := 0; i < max(len(gotLines), len(wantLines)); i++ {
		var g, w string
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			// literal: a message format.
			return fmt.Sprintf("line %d\n got %s\nwant %s", i+1, g, w)
		}
	}
	return ""
}
