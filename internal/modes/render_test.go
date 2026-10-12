package modes

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

func TestModelStringMethodsIncludeCoreFields(t *testing.T) {
	t.Parallel()

	book := renderBook(&anna.Book{Title: "Example", Authors: "Author", Publisher: "Press", URL: "https://annas.test/md5/hash", Hash: "hash"})
	for _, part := range []string{"Title: Example", "Authors: Author", "Publisher: Press", "URL: https://annas.test/md5/hash"} {
		if !strings.Contains(book, part) {
			t.Errorf("book string %q does not contain %q", book, part)
		}
	}
	const signedLink = "https://cdn.example/paper.pdf~/SIGNATURE/Example.pdf"
	paperValue := &anna.Paper{DOI: "10.1000/example", Title: "Example", Authors: "Author", Journal: "Journal", PageURL: "https://annas.test/scidb", DownloadURL: signedLink}
	encoded, err := json.Marshal(paperValue)
	if err != nil {
		t.Fatal(err)
	}
	paper := renderPaper(paperValue)
	// The SciDB link is a bearer URL issued to the member session.
	for _, output := range []string{paper, string(encoded)} {
		if strings.Contains(output, "SIGNATURE") || strings.Contains(output, "download_url") {
			t.Errorf("paper output exposes the signed download link: %s", output)
		}
	}
	for _, part := range []string{"DOI: 10.1000/example", "Title: Example", "Journal: Journal", "Page: https://annas.test/scidb"} {
		if !strings.Contains(paper, part) {
			t.Errorf("paper string %q does not contain %q", paper, part)
		}
	}
}

func TestDetailsAndAccountRendering(t *testing.T) {
	details := &anna.Details{Identifiers: map[string][]string{"isbn13": {"9780521879286", "9780521127264"}}, Problems: []anna.Problem{{Description: "Marked broken in Libgen.li"}}}
	status := &anna.AccountStatus{DownloadsLimit: 75, WindowHours: 18}
	if text := renderDetails(details); !strings.Contains(text, "ISBN-13: 9780521879286, 9780521127264") || !strings.Contains(text, "Problem: Marked broken in Libgen.li") {
		t.Fatalf("text view:\n%s", text)
	}
	if text := renderAccountStatus(status); !strings.Contains(text, "Fast download limit: 75 per 18 hours") {
		t.Fatal(text)
	}
}
