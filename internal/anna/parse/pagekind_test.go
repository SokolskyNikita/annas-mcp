package parse

import "testing"

func TestIsArchivePage(t *testing.T) {
	t.Parallel()

	if !IsArchivePage(pageDocument(t, "search_empty.html.gz")) || !IsArchivePage(pageDocument(t, "record_book.html.gz")) {
		t.Fatal("Anna's own pages were not recognized")
	}
	if !IsArchivePage(snippetDocument(t, "search_form.html")) {
		t.Fatal("a page with Anna's search form was not recognized")
	}
	if IsArchivePage(accountDocument(t, "challenge.html")) || IsArchivePage(snippetDocument(t, "other_site.html")) || IsArchivePage(nil) {
		t.Fatal("a browser check or another site was taken for Anna's page")
	}
}
