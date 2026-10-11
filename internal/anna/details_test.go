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
	"time"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

const detailsHash = "8336332bf5877e3adbfb60ac70720cd5"

func TestParseRecordIDAcceptsHashesCatalogIDsAndURLs(t *testing.T) {
	t.Parallel()

	valid := map[string][2]string{
		strings.ToUpper(detailsHash):                         {"md5", detailsHash},
		"md5:" + detailsHash:                                 {"md5", detailsHash},
		" isbndb:9780521879286 ":                             {"isbndb", "9780521879286"},
		"OL:OL18017946M":                                     {"ol", "OL18017946M"},
		"https://annas-archive.gl/md5/" + detailsHash:        {"md5", detailsHash},
		"https://annas-archive.gl/isbndb/9780521879286":      {"isbndb", "9780521879286"},
		"/ol/OL18017946M":                                    {"ol", "OL18017946M"},
		"doi:10.1000/example/part":                           {"doi", "10.1000/example/part"},
		"https://annas-archive.gl/doi/10.1000%2Fexample%2Fx": {"doi", "10.1000/example/x"},
	}
	for input, want := range valid {
		prefix, value, err := ParseRecordID(input)
		if err != nil || prefix != want[0] || value != want[1] {
			t.Errorf("ParseRecordID(%q) = %q, %q, %v; want %q, %q", input, prefix, value, err, want[0], want[1])
		}
	}
	for _, input := range []string{"", "hello", "isbndb:", "md5:xyz", "two words:x", "isbndb:978 052", "ol:OL1M?x=1", ":value"} {
		if _, _, err := ParseRecordID(input); apperr.CodeOf(err) != apperr.InvalidArgument {
			t.Errorf("ParseRecordID(%q) err = %v, want INVALID_ARGUMENT", input, err)
		}
	}
}

// detailsClient serves record and statistics fixtures and records requests.
func detailsClient(t *testing.T, routes map[string]func(*http.Request) *http.Response) (*Client, *[]string) {
	t.Helper()
	var mu sync.Mutex
	var requests []string
	client := NewClient(Config{
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
			return testDownloadResponse(req, http.StatusNotFound, "text/json", `"{\"error\":\"Record not found\"}"`), nil
		})},
	})
	return client, &requests
}

func TestDetailsCombinesRecordAndStatistics(t *testing.T) {
	t.Parallel()

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
	details, err := client.Details(context.Background(), detailsHash, time.Second)
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
	if text := details.String(); !strings.Contains(text, "ISBN-13: 9780521879286, 9780521127264") || !strings.Contains(text, "Problem: Marked broken in Libgen.li") {
		t.Fatalf("text view:\n%s", text)
	}
}

func TestDetailsOfACatalogRecordSkipStatistics(t *testing.T) {
	t.Parallel()

	client, requests := detailsClient(t, map[string]func(*http.Request) *http.Response{
		"/db/aarecord_elasticsearch/isbndb:9780521879286.json": func(req *http.Request) *http.Response {
			return fixtureResponse(t, req, http.StatusOK, "record_isbndb.json", "text/json", nil)
		},
	})
	details, err := client.Details(context.Background(), "isbndb:9780521879286", time.Second)
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

	client, _ := detailsClient(t, map[string]func(*http.Request) *http.Response{
		"/db/aarecord_elasticsearch/md5:" + detailsHash + ".json": func(req *http.Request) *http.Response {
			return fixtureResponse(t, req, http.StatusOK, "record_md5.json", "text/json", nil)
		},
		"/dyn/md5/summary/" + detailsHash: func(req *http.Request) *http.Response {
			return testDownloadResponse(req, http.StatusBadGateway, "text/html", "<html>bad gateway</html>")
		},
	})
	details, err := client.Details(context.Background(), detailsHash, time.Second)
	if err != nil || details.Stats != nil || details.Title == "" {
		t.Fatalf("details = %+v, err = %v", details, err)
	}
}

func TestDetailsErrors(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		response func(*http.Request) *http.Response
		code     string
		message  string
	}{
		"missing": {func(req *http.Request) *http.Response {
			return testDownloadResponse(req, http.StatusNotFound, "text/json", `"{\"error\":\"Record not found\"}"`)
		}, apperr.NotFound, "has no record md5:" + detailsHash},
		"not a member": {func(req *http.Request) *http.Response {
			return testDownloadResponse(req, http.StatusForbidden, "text/json", `{"error":"Not a member"}`)
		}, apperr.UpstreamBlocked, "members only"},
		"browser check": {func(req *http.Request) *http.Response {
			return testDownloadResponse(req, http.StatusOK, "text/html", "  <!doctype html><title>DDoS-Guard</title>")
		}, apperr.UpstreamBlocked, "web page instead of record details"},
		"broken JSON": {func(req *http.Request) *http.Response {
			return testDownloadResponse(req, http.StatusOK, "text/json", `{"id":`)
		}, apperr.Upstream, "invalid record details"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := detailsClient(t, map[string]func(*http.Request) *http.Response{
				"/db/aarecord_elasticsearch/md5:" + detailsHash + ".json": tc.response,
				"/dyn/md5/summary/" + detailsHash: func(req *http.Request) *http.Response {
					return fixtureResponse(t, req, http.StatusOK, "md5_summary.json", "text/css", nil)
				},
			})
			_, err := client.Details(context.Background(), detailsHash, time.Second)
			if apperr.CodeOf(err) != tc.code || !strings.Contains(err.Error(), tc.message) {
				t.Fatalf("err = %v, want %s containing %q", err, tc.code, tc.message)
			}
		})
	}
}

func TestLooseJSONValuesTolerateNumbersAndNulls(t *testing.T) {
	t.Parallel()

	var record parse.Record
	body := `{"id":"md5:x","file_unified_data":{"year_best":2008,"filesize_best":12,"has_aa_downloads":true,"title_additional":["a",null,3],"identifiers_unified":{"isbn13":"9780000000002"}}}`
	if err := json.Unmarshal([]byte(body), &record); err != nil {
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
	client := NewClient(Config{
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
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: req,
					Body: io.NopCloser(strings.NewReader(`{"downloads_left":0,"is_member":0,"download_still_active":0}`))}, nil
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
	status, err := client.AccountStatus(context.Background(), time.Second)
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
	if text := status.String(); !strings.Contains(text, "Fast download limit: 75 per 18 hours") {
		t.Fatal(text)
	}
}

func TestAccountStatusWithoutMembershipUsesStatistics(t *testing.T) {
	t.Parallel()

	client, summaries := accountClient(t, "account_no_membership.html")
	status, err := client.AccountStatus(context.Background(), time.Second)
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
	status, err := client.AccountStatus(context.Background(), time.Second)
	if err != nil || !status.Member || client.sessionCookie() != "aa_account_id2=new" {
		t.Fatalf("status = %+v, err = %v, cookie %q", status, err, client.sessionCookie())
	}
	signedOut, _ := accountClient(t, "account_signed_out.html")
	signedOut.config.SecretKey = ""
	if _, err := signedOut.AccountStatus(context.Background(), time.Second); apperr.CodeOf(err) != apperr.UpstreamBlocked || !strings.Contains(err.Error(), "signed out") {
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
