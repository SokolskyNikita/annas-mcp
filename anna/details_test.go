package anna

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

const detailsHash = "8336332bf5877e3adbfb60ac70720cd5"

func TestParseRecordIDAcceptsHashesCatalogIDsAndURLs(t *testing.T) {
	t.Parallel()

	var cases struct {
		Comment string
		Valid   []struct{ Input, Prefix, Value string }
		Invalid []string
	}
	loadCases(t, "record_ids", &cases)
	for _, tc := range cases.Valid {
		prefix, value, err := ParseRecordID(tc.Input)
		if err != nil || prefix != tc.Prefix || value != tc.Value {
			t.Errorf("ParseRecordID(%q) = %q, %q, %v; want %q, %q", tc.Input, prefix, value, err, tc.Prefix, tc.Value)
		}
	}
	for _, input := range cases.Invalid {
		if _, _, err := ParseRecordID(input); CodeOf(err) != InvalidArgument {
			t.Errorf("ParseRecordID(%q) err = %v, want INVALID_ARGUMENT", input, err)
		}
	}
}

// detailsClient serves record and statistics fixtures and records requests.
func detailsClient(t *testing.T, routes map[string]func(*http.Request) *http.Response) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var requests []string
	client := mustClient(Config{
		BaseURL:       "https://annas.test",
		AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			requests = append(requests, req.URL.RequestURI())
			mu.Unlock()
			if req.Header.Get("Cookie") != "aa_account_id2=test" {
				t.Errorf("%s was sent without the member session", req.URL)
			}
			if route, ok := routes[req.URL.Path]; ok {
				return route(req), nil
			}
			return fixtureResponse(t, req, http.StatusNotFound, "record_not_found.json", "text/json", nil), nil
		})},
	})
	return client, &requests
}

func TestDetailsCombinesRecordAndStatistics(t *testing.T) {
	t.Parallel()

	// literal: the routes are the requests Details must make.
	client, requests := detailsClient(t, map[string]func(*http.Request) *http.Response{
		"/db/aarecord_elasticsearch/md5:" + detailsHash + ".json": func(req *http.Request) *http.Response {
			return fixtureResponse(t, req, http.StatusOK, "record_md5.json", "text/json", nil)
		},
		"/dyn/md5/summary/" + detailsHash: func(req *http.Request) *http.Response {
			if req.URL.Query().Get("cachebust") == "" {
				t.Error("statistics depend on the account, so they must bypass the CDN cache")
			}
			return fixtureResponse(t, req, http.StatusOK, "md5_summary.json", "text/css", nil)
		},
	})
	details, err := client.Details(context.Background(), detailsHash, callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 2 {
		t.Fatalf("requests = %v", *requests)
	}
	if details.ID != "md5:"+detailsHash || details.Hash != detailsHash || details.URL != "https://annas.test/md5/"+detailsHash {
		t.Fatalf("identity = %q %q %q", details.ID, details.Hash, details.URL)
	}
	if details.Title != "Against intellectual monopoly" || details.Authors != "Michele Boldrin, David K. Levine" ||
		details.Edition != "1. publ, Cambridge, 2008" || details.Year != "2008" || details.Format != "pdf" ||
		details.SizeBytes != 4283327 || details.Content != "book_nonfiction" || !details.FastDownload ||
		!reflect.DeepEqual(details.Languages, []string{"en"}) || details.AddedDate != "2009-08-06" {
		t.Fatalf("details = %+v", details)
	}
	// The best title is not repeated, and alternatives are de-duplicated case-insensitively.
	if !reflect.DeepEqual(details.AlternativeTitles, []string{"Against intellectual monopoly 1/1"}) {
		t.Fatalf("alternative titles = %q", details.AlternativeTitles)
	}
	if !reflect.DeepEqual(details.Identifiers["isbn13"], []string{"9780521879286", "9780521127264"}) || details.Identifiers["doi"][0] != "10.1017/cbo9780511510854" {
		t.Fatalf("identifiers = %v", details.Identifiers)
	}
	for _, internal := range []string{"md5", "sha256", "filepath", "server_path"} {
		if _, ok := details.Identifiers[internal]; ok {
			t.Errorf("internal identifier %q leaked into details", internal)
		}
	}
	if !reflect.DeepEqual(details.Classifications["ddc"], []string{"346.048", "346.04/8"}) || details.Classifications["year"] != nil {
		t.Fatalf("classifications = %v", details.Classifications)
	}
	for _, want := range []string{"Monopolies", "Competition", "LAW / Business & Financial", "Business & Economics"} {
		if !slices.Contains(details.Subjects, want) {
			t.Errorf("subjects %q lack %q", details.Subjects, want)
		}
	}
	wantProblems := []Problem{
		{Type: "lgli_broken", Description: "Marked broken in Libgen.li", BetterHash: "224c5b1195fd2fb4650c3b75c2908d65"},
		{Type: "lgrsnf_visible", Description: "Not visible in Libgen.rs Non-Fiction (ban)"},
	}
	if !reflect.DeepEqual(details.Problems, wantProblems) {
		t.Fatalf("problems = %+v", details.Problems)
	}
	if details.Stats == nil || *details.Stats != (RecordStats{Downloads: 3248, GreatQuality: 3, Lists: 19}) || !details.RecentlyDownloaded {
		t.Fatalf("stats = %+v recently=%v", details.Stats, details.RecentlyDownloaded)
	}
}

func TestDetailsOfACatalogRecordSkipStatistics(t *testing.T) {
	t.Parallel()

	client, requests := detailsClient(t, map[string]func(*http.Request) *http.Response{
		"/db/aarecord_elasticsearch/isbndb:9780521879286.json": func(req *http.Request) *http.Response {
			return fixtureResponse(t, req, http.StatusOK, "record_isbndb.json", "text/json", nil)
		},
	})
	details, err := client.Details(context.Background(), "isbndb:9780521879286", callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 || details.Hash != "" || details.Stats != nil || details.FastDownload || details.SizeBytes != 0 {
		t.Fatalf("catalog details = %+v, requests %v", details, *requests)
	}
	if details.URL != "https://annas.test/isbndb/9780521879286" || details.Identifiers["isbn10"][0] != "0521879280" {
		t.Fatalf("catalog details = %+v", details)
	}
}

func TestDetailsSurviveMissingStatistics(t *testing.T) {
	t.Parallel()

	// literal: the routes are the requests Details must make.
	client, _ := detailsClient(t, map[string]func(*http.Request) *http.Response{
		"/db/aarecord_elasticsearch/md5:" + detailsHash + ".json": func(req *http.Request) *http.Response {
			return fixtureResponse(t, req, http.StatusOK, "record_md5.json", "text/json", nil)
		},
		"/dyn/md5/summary/" + detailsHash: func(req *http.Request) *http.Response {
			return htmlResponse(req, http.StatusBadGateway, errorPage(t, "bad gateway"))
		},
	})
	details, err := client.Details(context.Background(), detailsHash, callTimeout)
	if err != nil || details.Stats != nil || details.Title == "" {
		t.Fatalf("details = %+v, err = %v", details, err)
	}
}

func TestDetailsErrors(t *testing.T) {
	t.Parallel()

	var cases struct {
		Comment string
		Cases   []struct {
			Name, Answer, Code, Message string
			Status                      int
		}
	}
	loadCases(t, "details_errors", &cases)
	for _, tc := range cases.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			contentType := "text/json"
			if strings.HasSuffix(tc.Answer, ".html") {
				contentType = "text/html"
			}
			client, _ := detailsClient(t, map[string]func(*http.Request) *http.Response{
				"/db/aarecord_elasticsearch/md5:" + detailsHash + ".json": func(req *http.Request) *http.Response {
					return fixtureResponse(t, req, tc.Status, tc.Answer, contentType, nil)
				},
				"/dyn/md5/summary/" + detailsHash: func(req *http.Request) *http.Response {
					return fixtureResponse(t, req, http.StatusOK, "md5_summary.json", "text/css", nil)
				},
			})
			_, err := client.Details(context.Background(), detailsHash, callTimeout)
			if CodeOf(err) != tc.Code || !strings.Contains(err.Error(), tc.Message) {
				t.Fatalf("err = %v, want %s containing %q", err, tc.Code, tc.Message)
			}
		})
	}
}

func TestLooseJSONValuesTolerateNumbersAndNulls(t *testing.T) {
	t.Parallel()

	var record parse.Record
	if err := json.Unmarshal(readFixture(t, "snippets/record_loose_values.json"), &record); err != nil {
		t.Fatal(err)
	}
	file := record.FileUnifiedData
	if file.YearBest != "2008" || file.FilesizeBest != "12" || file.HasAADownloads != "true" ||
		!reflect.DeepEqual([]string(file.TitleAdditional), []string{"a", "", "3"}) ||
		!reflect.DeepEqual([]string(file.IdentifiersUnified["isbn13"]), []string{"9780000000002"}) {
		t.Fatalf("record = %+v", file)
	}
	if details := detailsFromRecord(&record, "https://annas.test", "md5", detailsHash); !details.FastDownload || details.SizeBytes != 12 {
		t.Fatalf("details = %+v", details)
	}
}

func accountClient(t *testing.T, pages ...string) (*Client, *int) {
	t.Helper()
	var mu sync.Mutex
	summaries := 0
	served := 0
	client := mustClient(Config{
		BaseURL:       "https://annas.test",
		AccountCookie: "aa_account_id2=old",
		SecretKey:     "test-secret",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			switch {
			case req.URL.Path == "/account/" && req.Method == http.MethodPost:
				return fakeSignIn(t, req, "test-secret", "aa_account_id2=new"), nil
			case req.URL.Path == "/account/":
				page := pages[min(served, len(pages)-1)]
				served++
				return fixtureResponse(t, req, http.StatusOK, page, "text/html", nil), nil
			case strings.HasPrefix(req.URL.Path, "/dyn/md5/summary/"):
				summaries++
				return fixtureResponse(t, req, http.StatusOK, "md5_summary_not_member.json", "text/css", nil), nil
			}
			t.Errorf("unexpected request %s %s", req.Method, req.URL)
			return nil, io.EOF
		})},
	})
	return client, &summaries
}

func TestAccountStatusReadsAllowanceAndActiveMemberships(t *testing.T) {
	t.Parallel()

	client, summaries := accountClient(t, "account_page.html")
	status, err := client.AccountStatus(context.Background(), callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	want := &AccountStatus{
		Member: true, DownloadsLeft: 71, DownloadsLimit: 75, WindowHours: 18,
		Memberships: []string{"Lucky Librarian (50) until October 26, 2026", "Brilliant Bookworm (25) until October 4, 2027"},
	}
	if !reflect.DeepEqual(status, want) || *summaries != 0 {
		t.Fatalf("status = %+v (summaries %d)", status, *summaries)
	}
}

func TestAccountStatusWithoutMembershipUsesStatistics(t *testing.T) {
	t.Parallel()

	client, summaries := accountClient(t, "account_no_membership.html")
	status, err := client.AccountStatus(context.Background(), callTimeout)
	if err != nil {
		t.Fatal(err)
	}
	if status.Member || status.DownloadsLeft != 0 || status.Memberships != nil || *summaries != 1 {
		t.Fatalf("status = %+v (summaries %d)", status, *summaries)
	}
}

func TestAccountStatusSignsInAgainWhenTheSessionExpired(t *testing.T) {
	t.Parallel()

	client, _ := accountClient(t, "account_signed_out.html", "account_page.html")
	status, err := client.AccountStatus(context.Background(), callTimeout)
	if err != nil || !status.Member || client.sessionCookie() != "aa_account_id2=new" {
		t.Fatalf("status = %+v, err = %v, cookie %q", status, err, client.sessionCookie())
	}
	signedOut, _ := accountClient(t, "account_signed_out.html")
	signedOut.config.SecretKey = ""
	if _, err := signedOut.AccountStatus(context.Background(), callTimeout); CodeOf(err) != UpstreamBlocked || !strings.Contains(err.Error(), "signed out") {
		t.Fatalf("err = %v", err)
	}
}

func TestTruncateTextCutsLongDescriptionsAtAWord(t *testing.T) {
	t.Parallel()

	if got := truncateText("short", maxDetailDescription); got != "short" {
		t.Fatalf("short text changed: %q", got)
	}
	long := strings.Repeat("Принцип ", 1000)
	got := truncateText(long, maxDetailDescription)
	if len(got) > maxDetailDescription+len("…") || !utf8.ValidString(got) || !strings.HasSuffix(got, "Принцип…") {
		t.Fatalf("truncated to %d bytes, valid=%v, tail %q", len(got), utf8.ValidString(got), got[len(got)-20:])
	}
}
