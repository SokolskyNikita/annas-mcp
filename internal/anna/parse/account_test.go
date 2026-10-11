package parse

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func accountDocument(t *testing.T, name string) *goquery.Document {
	t.Helper()
	data, err := testdataFS.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestParseAccountPage(t *testing.T) {
	t.Parallel()

	page := ParseAccountPage(accountDocument(t, "account_page.html"))
	want := AccountPage{
		SignedIn: true, Used: 4, Limit: 75, WindowHours: 18,
		// The struck-through membership has expired.
		Memberships: []string{"Lucky Librarian (50) until October 26, 2026", "Brilliant Bookworm (25) until October 4, 2027"},
	}
	if !reflect.DeepEqual(page, want) {
		t.Fatalf("account page\n got %+v\nwant %+v", page, want)
	}
	if page := ParseAccountPage(accountDocument(t, "account_no_membership.html")); !page.SignedIn || page.Limit != 0 || page.Memberships != nil {
		t.Fatalf("account without membership: %+v", page)
	}
	if page := ParseAccountPage(accountDocument(t, "account_signed_out.html")); page.SignedIn {
		t.Fatalf("signed-out page: %+v", page)
	}
	if page := ParseAccountPage(nil); !reflect.DeepEqual(page, AccountPage{}) {
		t.Fatalf("no page: %+v", page)
	}
	odd := htmlDocument(t, `<div>Fast downloads used: <strong>many</strong> <a href="/account/downloaded">which?</a></div>
		<div>Fast downloads used: <strong>1 / 10</strong> <a href="/account/downloaded">which?</a></div>
		<div><strong></strong><a href="/donate?tier=1">x</a></div>`)
	if page := ParseAccountPage(odd); page.Used != 1 || page.Limit != 10 || page.WindowHours != 0 || page.Memberships != nil {
		t.Fatalf("odd allowance lines: %+v", page)
	}
}
