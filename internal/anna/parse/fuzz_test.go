package parse

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// Seeds live in testdata/fuzz/<FuzzName>, where go test finds them, and in
// the corpus files.

// corpusSeeds adds the lines of a corpus file, when there is one, as seeds.
func corpusSeeds(f *testing.F, name string) {
	lines, err := corpusFileLines(name)
	if err != nil {
		f.Fatal(err)
	}
	for _, line := range lines {
		f.Add(line)
	}
}

func FuzzNormalizeDOI(f *testing.F) {
	f.Fuzz(func(t *testing.T, input string) {
		once := NormalizeDOI(input)
		if twice := NormalizeDOI(once); twice != once {
			t.Fatalf("NormalizeDOI is not stable: %q -> %q -> %q", input, once, twice)
		}
		if doi, valid := ParseDOI(input); valid && !ValidDOI(doi) {
			t.Fatalf("ParseDOI(%q) = %q, valid, but ValidDOI says no", input, doi)
		}
	})
}

func FuzzCleanAuthors(f *testing.F) {
	corpusSeeds(f, "authors.txt.gz")
	f.Fuzz(func(t *testing.T, input string) {
		once := CleanAuthors(input)
		if twice := CleanAuthors(once); twice != once {
			t.Fatalf("CleanAuthors is not stable: %q -> %q -> %q", input, once, twice)
		}
		if strings.Contains(once, "  ") || strings.TrimSpace(once) != once {
			t.Fatalf("CleanAuthors(%q) = %q leaves stray spaces", input, once)
		}
	})
}

func FuzzCleanText(f *testing.F) {
	corpusSeeds(f, "titles.txt.gz")
	f.Fuzz(func(t *testing.T, input string) {
		got := CleanText(input)
		if strings.TrimSpace(got) != got || strings.Contains(got, "  ") || strings.ContainsFunc(got, func(r rune) bool { return unicode.IsSpace(r) && r != ' ' }) {
			t.Fatalf("CleanText(%q) = %q leaves stray whitespace", input, got)
		}
		if utf8.ValidString(input) && !utf8.ValidString(got) {
			t.Fatalf("CleanText(%q) = %q is not valid UTF-8", input, got)
		}
	})
}

func FuzzParseCardMeta(f *testing.F) {
	corpusSeeds(f, "metadata_lines.txt.gz")
	f.Fuzz(func(t *testing.T, line string) {
		meta := ParseCardMeta(line)
		parts := map[string]bool{}
		for _, part := range strings.Split(line, "·") {
			parts[strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(part), "✅"))] = true
		}
		for _, part := range meta.Unparsed {
			if part == "" || !parts[part] {
				t.Fatalf("ParseCardMeta(%q) reported %q, which is not a part of the line", line, part)
			}
		}
	})
}

func FuzzRecordPath(f *testing.F) {
	f.Fuzz(func(t *testing.T, href string) {
		prefix, id, ok := RecordPath(href)
		if !ok {
			return
		}
		if !recordPrefixPattern.MatchString(prefix) || id == "" {
			t.Fatalf("RecordPath(%q) = %q, %q", href, prefix, id)
		}
	})
}
