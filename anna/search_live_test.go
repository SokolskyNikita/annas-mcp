package anna

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// Opt-in integration check of the parsers against the live archive: search
// filters and cards, catalog search, record details and the account page.
// It downloads nothing, so it uses no fast downloads.
func TestLiveSearchDetailsAndAccount(t *testing.T) {
	if os.Getenv("ANNAS_MCP_TEST_LIVE") != "1" {
		t.Skip("set ANNAS_MCP_TEST_LIVE=1 to check search, details and account parsing against Anna's Archive")
	}
	if liveSetting("ANNAS_SECRET_KEY") == "" {
		t.Fatal("provide ANNAS_SECRET_KEY, in the environment or the repository .env, for the live check")
	}
	// Pick a mirror the way the server does, so one mirror's 502s do not fail the check.
	client := mustClient(Config{BaseURL: "https://annas-archive.gl", AutoBaseURL: true, SecretKey: liveSetting("ANNAS_SECRET_KEY"), AccountCookie: liveSetting("ANNAS_ACCOUNT_COOKIE")})
	ctx := context.Background()
	const timeout = 90 * time.Second

	books, err := client.FindBook(ctx, "against intellectual monopoly", SearchOptions{Format: []string{"epub"}, Language: []string{"en"}}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(books.Results) == 0 || books.Total < len(books.Results) {
		t.Fatalf("filtered search found %d results of %d", len(books.Results), books.Total)
	}
	for _, book := range books.Results {
		if book.Format != "EPUB" || !strings.Contains(book.Language, "English") || book.Year == "" || book.Content == "" || len(book.Sources) == 0 {
			t.Errorf("card not fully parsed: %+v", book)
		}
	}
	if !slices.ContainsFunc(books.Results, func(book *Book) bool { return book.FastDownload }) {
		t.Error("no result offers a fast download; the 🚀 marker may have moved")
	}

	byAuthor, err := client.FindBook(ctx, "", SearchOptions{Fields: []SearchField{{Name: "author", Value: "Boldrin"}, {Name: "year", Value: "2008"}}}, timeout)
	if err != nil || len(byAuthor.Results) == 0 {
		t.Fatalf("field search: %d results, %v", len(byAuthor.Results), err)
	}

	_, err = client.FindBook(ctx, "against intellectual monopoly", SearchOptions{Source: []string{"libgen"}}, timeout)
	if CodeOf(err) != InvalidArgument || !strings.Contains(err.Error(), "lgli") {
		t.Fatalf("unknown source was not rejected with choices: %v", err)
	}

	catalog, err := client.FindMetadata(ctx, "9780521879286", SearchOptions{}, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(catalog.Results, func(record *MetadataRecord) bool { return record.ID == "isbndb:9780521879286" }) {
		t.Fatalf("catalog search lacks the ISBNdb record: %d results", len(catalog.Results))
	}

	details, err := client.Details(ctx, "8336332bf5877e3adbfb60ac70720cd5", timeout)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(details.Identifiers["isbn13"], "9780521879286") || details.Year != "2008" || details.Stats == nil || details.Stats.Downloads == 0 || !details.FastDownload {
		t.Fatalf("details = %+v", details)
	}
	record, err := client.Details(ctx, "isbndb:9780521879286", timeout)
	if err != nil || record.Title == "" || record.Hash != "" {
		t.Fatalf("catalog details = %+v, %v", record, err)
	}

	status, err := client.AccountStatus(ctx, timeout)
	if err != nil {
		t.Fatal(err)
	}
	if status.Member && (status.DownloadsLimit == 0 || status.WindowHours == 0 || len(status.Memberships) == 0) {
		t.Fatalf("account page not fully parsed: %+v", status)
	}
	t.Logf("live check passed: %d filtered results of %d, %d catalog records, %d fast downloads left", len(books.Results), books.Total, len(catalog.Results), status.DownloadsLeft)
}
