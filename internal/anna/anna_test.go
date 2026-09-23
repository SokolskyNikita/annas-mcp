package anna

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestBuildSearchURLIncludesFiltersAndPage(t *testing.T) {
	t.Parallel()

	got := buildSearchURL("annas-archive.gl", "machine learning", SearchOptions{
		Content:  "book_nonfiction",
		Language: "en",
		Page:     2,
	})
	for _, part := range []string{
		"https://annas-archive.gl/search?",
		"q=machine+learning",
		"content=book_nonfiction",
		"lang=en",
		"page=2",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("search URL %q does not contain %q", got, part)
		}
	}
}

func TestResolveSearchDropsLegacyFilters(t *testing.T) {
	t.Parallel()

	books := ResolveSearch(SearchOptions{Content: "book_any", Page: 2}, "book_any")
	if books.Content != "" || books.Index != "" || books.Page != 2 {
		t.Fatalf("book_any should not be sent: %+v", books)
	}
	articles := ResolveSearch(SearchOptions{}, "journal")
	if articles.Index != "journals" || articles.Content != "" {
		t.Fatalf("articles should use the journals index: %+v", articles)
	}
	got := buildSearchURL("annas-archive.gl", "transformers", articles)
	if strings.Contains(got, "content=") || !strings.Contains(got, "index=journals") {
		t.Fatalf("article URL = %s", got)
	}
}

func TestSelectDOIHitIgnoresUnrelatedFilenames(t *testing.T) {
	t.Parallel()

	books := []*Book{
		{Hash: "aaa", Title: "1t86xnb7cd6x29mqzpy17os8m.pdf"},
		{Hash: "bbb", Title: "Attention Is All You Need", Description: "arXiv:1706.03762v7 Abstract The dominant sequence transduction models"},
	}
	hit := selectDOIHit(books, "10.48550/arXiv.1706.03762")
	if hit == nil || hit.Hash != "bbb" {
		t.Fatalf("expected the arXiv paper, got %+v", hit)
	}
	if arxivID("10.48550/arXiv.1706.03762v3") != "1706.03762" {
		t.Fatal("arxiv id was not stripped")
	}
	hit = selectTitleHit([]*Book{
		{Hash: "ccc", Title: "Attention Is All You Need 中文翻译"},
		{Hash: "ddd", Title: "Attention Is All You Need"},
		{Hash: "eee", Title: "1t86xnb7cd6x29mqzpy17os8m.pdf"},
	}, "Attention Is All You Need")
	if hit == nil || hit.Hash != "ddd" {
		t.Fatalf("expected the exact title, got %+v", hit)
	}
}

func TestParseBooksReadsSearchCard(t *testing.T) {
	t.Parallel()

	html := `
<div>
  <a class="custom-a block" href="/md5/abc123def456"></a>
  <div class="max-w-full">
    <a href="/md5/abc123def456">Example Title</a>
    <a href="/search?q=author"><span class="icon-[mdi--user-edit]"></span>Ada Lovelace</a>
    <a href="/search?q=pub"><span class="icon-[mdi--company]"></span>Example Press</a>
    <div class="text-gray-800">✅ English [en] · EPUB · 1.2MB · 2020</div>
    <div class="line-clamp-[2] text-sm text-gray-600">A short description. DOI 10.1000/example.123.</div>
  </div>
</div>`
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}

	books := parseBooks(doc, "https://annas-archive.gl/search?q=example")
	if len(books) != 1 {
		t.Fatalf("expected 1 book, got %d", len(books))
	}
	book := books[0]
	if book.Hash != "abc123def456" || book.Title != "Example Title" || book.Format != "EPUB" {
		t.Fatalf("unexpected book: %+v", book)
	}
	if book.Authors != "Ada Lovelace" || book.Publisher != "Example Press" || book.Language != "English" {
		t.Fatalf("unexpected metadata: %+v", book)
	}
	if book.URL != "https://annas-archive.gl/md5/abc123def456" {
		t.Fatalf("unexpected URL %q", book.URL)
	}
	if book.Description != "A short description. DOI 10.1000/example.123." || book.DOI != "10.1000/example.123" {
		t.Fatalf("unexpected description: %+v", book)
	}
}

func TestReserveDownloadPathDoesNotReplaceExistingFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	existing := filepath.Join(dir, "Example.pdf")
	if err := os.WriteFile(existing, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := reserveDownloadPath(dir, "Example", "pdf", "abc123def456")
	if err != nil {
		t.Fatal(err)
	}
	if path == existing {
		t.Fatal("reserved the existing file")
	}
	body, err := os.ReadFile(existing)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "keep" {
		t.Fatalf("existing file was changed to %q", body)
	}
}

func TestCopyWithHashRejectsMismatchedChecksum(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path, err := reserveDownloadPath(dir, "Example", "bin", "abc123def456")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	sum := md5.Sum([]byte("hello"))
	_, err = copyWithHash(context.Background(), file, bytes.NewReader([]byte("hello")), -1, hex.EncodeToString(sum[:]), nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = copyWithHash(context.Background(), file, bytes.NewReader([]byte("other")), -1, hex.EncodeToString(sum[:]), nil)
	if err == nil {
		t.Fatal("expected a checksum mismatch")
	}
}

func TestRedactErrHidesSecret(t *testing.T) {
	t.Parallel()

	got := redactErr(errors.New("request failed for secret-key"), "secret-key")
	if strings.Contains(got.Error(), "secret-key") || !strings.Contains(got.Error(), "REDACTED") {
		t.Fatalf("secret leaked: %v", got)
	}
}

func TestFetchDocumentReportsForbidden(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	_, _, err := fetchDocument(context.Background(), time.Second, server.URL)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected a 403 error, got %v", err)
	}
}
