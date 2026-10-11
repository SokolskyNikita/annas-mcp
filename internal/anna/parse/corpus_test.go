package parse

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// corpora are the real strings collected by TestCrawlCorpus in package anna,
// with the rule each goes through.
//
// literal: the corpus file names, each with its rule.
var corpora = []struct {
	name string
	read func(line string) (any, error)
}{
	{"metadata_lines", func(line string) (any, error) { return ParseCardMeta(line), nil }},
	{"authors", func(line string) (any, error) { return CleanAuthors(CleanText(line)), nil }},
	{"publishers", func(line string) (any, error) {
		var pair [2]string
		if err := json.Unmarshal([]byte(line), &pair); err != nil {
			return nil, err
		}
		publisher, edition := publisherAndEdition(pair[0], pair[1])
		return map[string]string{"publisher": publisher, "edition": edition}, nil
	}},
	{"titles", func(line string) (any, error) { return CleanText(line), nil }},
}

// TestCorpus runs the text rules over thousands of real strings and compares
// the results with the reviewed ones in corpus/<name>.golden.jsonl.gz, one
// {"in", "out"} object per line. After changing a rule, run with -update and
// review the diff.
func TestCorpus(t *testing.T) {
	t.Parallel()

	for _, corpus := range corpora {
		t.Run(corpus.name, func(t *testing.T) {
			t.Parallel()
			lines := corpusLines(t, corpus.name+".txt.gz")
			if lines == nil {
				t.Skip("no corpus yet; see TestCrawlCorpus in package anna")
			}
			var got bytes.Buffer
			encoder := json.NewEncoder(&got)
			encoder.SetEscapeHTML(false)
			for _, line := range lines {
				out, err := corpus.read(line)
				if err != nil {
					t.Fatalf("%q: %v", line, err)
				}
				err = encoder.Encode(struct {
					In  string `json:"in"`
					Out any    `json:"out"`
				}{line, out})
				if err != nil {
					t.Fatal(err)
				}
			}
			name := corpus.name + ".golden.jsonl.gz"
			if *update {
				if err := writeGolden(filepath.Join("testdata", "corpus", name), got.Bytes()); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := readTestdata("corpus/" + name)
			if err != nil {
				t.Fatalf("%v; run with -update", err)
			}
			if !bytes.Equal(got.Bytes(), want) {
				t.Errorf("%s changed; if the change is right, rerun with -update and review the diff:\n%s", name, firstDifference(got.Bytes(), want))
			}
		})
	}
}

// TestCorpusHasNothingUnread checks that every part of every real metadata
// line is recognized.
func TestCorpusHasNothingUnread(t *testing.T) {
	t.Parallel()

	var report Report
	for _, line := range corpusLines(t, "metadata_lines.txt.gz") {
		for _, part := range ParseCardMeta(line).Unparsed {
			report.Add(ProblemUnparsedMeta, part)
		}
	}
	if !report.Empty() {
		t.Fatal(report)
	}
}

func corpusLines(t *testing.T, name string) []string {
	t.Helper()
	lines, err := corpusFileLines(name)
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

// writeGolden saves a corpus's expected output gzipped. A file whose content is
// unchanged is left alone, so -update does not rewrite every file.
func writeGolden(path string, content []byte) error {
	if old, err := os.ReadFile(path); err == nil {
		if reader, err := gzip.NewReader(bytes.NewReader(old)); err == nil {
			if current, err := io.ReadAll(reader); err == nil && bytes.Equal(current, content) {
				return nil
			}
		}
	}
	var buffer bytes.Buffer
	writer, err := gzip.NewWriterLevel(&buffer, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := writer.Write(content); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buffer.Bytes(), 0o644)
}
