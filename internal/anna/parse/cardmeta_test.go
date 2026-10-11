package parse

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseCardMetaReadsEveryPart(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want CardMeta
	}{
		{
			line: "English [en] · EPUB · 3.8MB · 2008 · 📘 Book (non-fiction) · 🚀/lgli/zlib    · ",
			want: CardMeta{Language: "English", Format: "EPUB", Size: "3.8MB", Year: "2008", Content: "book_nonfiction", Sources: []string{"lgli", "zlib"}, FastDownload: true},
		},
		{
			line: "English [en] · PDF · 0.2MB · 2006 · 📄 Journal article · 🧬/🚀/lgli/scihub",
			want: CardMeta{Language: "English", Format: "PDF", Size: "0.2MB", Year: "2006", Content: "journal_article", Sources: []string{"lgli", "scihub"}, FastDownload: true},
		},
		{
			// Without a language the extension comes first.
			line: "PDF · 1.5MB · 2008 · 📕 Book (fiction) · 🚀/lgli",
			want: CardMeta{Format: "PDF", Size: "1.5MB", Year: "2008", Content: "book_fiction", Sources: []string{"lgli"}, FastDownload: true},
		},
		{
			// No 🚀: the file has no fast download.
			line: "English [en] · German [de] · 7Z · 1.2GB · 💬 Comic book · lgli/lgrs",
			want: CardMeta{Language: "English, German", Format: "7Z", Size: "1.2GB", Content: "book_comic", Sources: []string{"lgli", "lgrs"}},
		},
		{
			// A catalog record: no file, and a record id that looks like an extension.
			line: "Metadata · English [en] · 📗 Book (unknown) · ol · OL1M",
			want: CardMeta{Language: "English", Content: "book_unknown", Sources: []string{"ol"}},
		},
		{
			// A lowercase label before the content type is not a source.
			line: "metadatos · Español [es] · 1999 · 📗\u00a0Libro (desconocido) · isbndb",
			want: CardMeta{Language: "Español", Year: "1999", Content: "book_unknown", Sources: []string{"isbndb"}},
		},
		{
			line: "Metadata · English [en] · 2008 · 📗 Book (unknown) · isbndb · ISBNdb 9780521879286",
			want: CardMeta{Language: "English", Year: "2008", Content: "book_unknown", Sources: []string{"isbndb"}},
		},
	}
	for _, tc := range tests {
		if got := ParseCardMeta(tc.line); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseCardMeta(%q)\n got %+v\nwant %+v", tc.line, got, tc.want)
		}
	}
}

func TestParseCardMetaAndSnippetNormalize(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("long description ", 40)
	if got := Snippet(text); len(got) > 480 || strings.Contains(got, "  ") {
		t.Fatalf("snippet was not normalized: length=%d value=%q", len(got), got)
	}
	if meta := ParseCardMeta("✅ English [en] · EPUB · 1.2MB · 2020"); meta.Language != "English" || meta.Format != "EPUB" || meta.Size != "1.2MB" || meta.Year != "2020" {
		t.Fatalf("metadata = %+v", meta)
	}
	if meta := ParseCardMeta("unstructured metadata"); meta.Language != "" || meta.Format != "" || meta.Size != "" || meta.Sources != nil {
		t.Fatalf("malformed metadata = %+v", meta)
	}

}
