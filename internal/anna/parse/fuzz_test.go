package parse

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

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
	for _, seed := range []string{"10.1000/x", "doi:10.1000/x", "https://doi.org/10.1000%2Fx?utm=1", "<doi: (10.1000/x(02))>", "doi:doi:10.1/x", "https://doi.org/doi:10.1/x", " \"'10.1/x'\" "} {
		f.Add(seed)
	}
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
	for _, seed := range []string{"Kingma, Diederik P. (author); Ba, Jimmy Lei (author)", "Yann Lecun , Yoshua Bengio ;1 FAIR - Facebook AI Research", "Lin Ning(Innovative Institute (Quzhou);University)", "（Institute）", " ; , ;",
		// Measured before its commas were spaced, this block was under the
		// 40 characters of an affiliation, and over them once cleaned.
		"(0000000000 Institut00(0000000,00000000"} {
		f.Add(seed)
	}
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
	for _, seed := range []string{"Smith &amp;amp; Jones", "<i>AlphaFold</i>2", "Vector<T>", "&#x201C;x&#x201D;", "<jats:p>a</jats:p>"} {
		f.Add(seed)
	}
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
	for _, seed := range []string{"English [en] · EPUB · 3.8MB · 2008 · 📘 Book (non-fiction) · 🚀/lgli/zlib", "Metadata · English [en] · 📗 Book (unknown) · ol · OL1M", "· · ✅ ·", "PDF · lgli/Some Label"} {
		f.Add(seed)
	}
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
	for _, seed := range []string{"/md5/0123", "/isbndb/978/", "/doi/10.1000/x?y=1#z", "https://a/md5/x", "/%zz/x", "//x/y", "/a/0//"} {
		f.Add(seed)
	}
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
