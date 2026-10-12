package anna

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/mirror"
)

const liveDedupeTimeout = 5 * time.Minute

type liveDedupeBookCase struct {
	Query  string `json:"query"`
	Title  string `json:"title"`
	Author string `json:"author"`
}

type liveDedupeBookCases struct {
	Books []liveDedupeBookCase `json:"books"`
}

type liveDedupeRequestCounts struct {
	total   atomic.Int64
	archive atomic.Int64
	api     atomic.Int64
	file    atomic.Int64
}

type liveDedupeRequestSnapshot struct {
	total   int64
	archive int64
	api     int64
	file    int64
}

func (c *liveDedupeRequestCounts) snapshot() liveDedupeRequestSnapshot {
	return liveDedupeRequestSnapshot{
		total: c.total.Load(), archive: c.archive.Load(), api: c.api.Load(), file: c.file.Load(),
	}
}

type liveDedupeRoundTripper struct {
	next   http.RoundTripper
	counts *liveDedupeRequestCounts
	secret string
	cookie string
}

func (r liveDedupeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Hostname())
	if req.URL.Scheme != "https" || host == "" || req.URL.User != nil {
		return nil, fmt.Errorf("live dedupe test blocked an unsafe or non-HTTPS request")
	}
	archive := slices.Contains(mirror.OfficialHosts(), host)
	secretInURL := liveDedupeURLContains(req, r.secret)
	allowedAPIKey := archive && req.URL.Path == "/dyn/api/fast_download.json" && req.URL.Query().Get("key") == r.secret
	cookieValue := strings.TrimPrefix(normalizeCookieHeader(r.cookie), accountCookieName+"=")
	if (secretInURL && !allowedAPIKey) || liveDedupeURLContains(req, cookieValue) || !archive && req.Header.Get("Cookie") != "" {
		return nil, fmt.Errorf("live dedupe test blocked account credentials outside the official Client auth path")
	}
	r.counts.total.Add(1)
	if archive {
		r.counts.archive.Add(1)
		if req.URL.Path == "/dyn/api/fast_download.json" {
			r.counts.api.Add(1)
		}
	} else {
		r.counts.file.Add(1)
	}
	return r.next.RoundTrip(req)
}

// TestLiveDownloadDeduplicationAcrossFiveBooks exercises hash-based local
// reuse against five real English EPUBs. It downloads exactly five distinct
// hashes, then checks reuse with changed metadata and after renaming each file.
// The test is opt-in because the initial downloads use the account's allowance.
func TestLiveDownloadDeduplicationAcrossFiveBooks(t *testing.T) {
	if os.Getenv("ANNAS_MCP_TEST_DEDUPE_LIVE") != "1" {
		t.Skip("set ANNAS_MCP_TEST_DEDUPE_LIVE=1 to download five books and check local deduplication against Anna's Archive")
	}

	key, cookie := liveSetting("ANNAS_SECRET_KEY"), liveSetting("ANNAS_ACCOUNT_COOKIE")
	if key == "" {
		t.Fatal("provide ANNAS_SECRET_KEY, in the environment or repository .env, for the live dedupe check")
	}
	baseURL := liveSetting("ANNAS_BASE_URL")
	if baseURL == "" {
		baseURL = defaultClientBaseURL
	}
	host, err := NormalizeBaseURL(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	configuration := Config{BaseURL: host, SessionFile: liveSetting("ANNAS_SESSION_FILE")}
	if !slices.Contains(mirror.OfficialHosts(), configuration.BaseURL) {
		t.Fatalf("the live dedupe check requires ANNAS_BASE_URL to name an official Anna's Archive mirror")
	}

	var cases liveDedupeBookCases
	decodeFixture(t, "cases/download_dedupe_live.json", &cases)
	if len(cases.Books) != 5 {
		t.Fatalf("live dedupe cases contain %d books, want exactly five", len(cases.Books))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	downloadDir := t.TempDir()
	counts := &liveDedupeRequestCounts{}
	transport := liveDedupeRoundTripper{next: http.DefaultTransport, counts: counts, secret: key, cookie: cookie}
	searchClient := liveDedupeClient(configuration.BaseURL, key, cookie, configuration.SessionFile, downloadDir, transport)
	options := SearchOptions{Format: []string{"epub"}, Language: []string{"en"}, Sort: "smallest"}

	type downloadedBook struct {
		caseInfo  liveDedupeBookCase
		book      *Book
		path      string
		hash      string
		bytes     int64
		apiCalls  int64
		fileCalls int64
	}
	selectedHashes := make(map[string]string, len(cases.Books))
	downloads := make([]downloadedBook, 0, len(cases.Books))
	const sameDownloadTitle = "Five Book Deduplication Validation"

	for _, bookCase := range cases.Books {
		var book *Book
		inspected := 0
		for pageNumber := 1; pageNumber <= 5; pageNumber++ {
			pageOptions := options
			pageOptions.Page = pageNumber
			pageOptions.Fields = []SearchField{{Name: "author", Value: bookCase.Author}}
			lookupCtx, lookupCancel := context.WithTimeout(ctx, 90*time.Second)
			page, lookupErr := searchClient.FindBook(lookupCtx, bookCase.Query, pageOptions, 90*time.Second)
			lookupCancel()
			if lookupErr != nil {
				t.Fatalf("search for %q failed (error type %T)", bookCase.Title, lookupErr)
			}
			inspected += len(page.Results)
			book = chooseLiveDedupeBook(page.Results, bookCase)
			if book != nil || !page.MorePages {
				break
			}
		}
		if book == nil {
			t.Fatalf("no English EPUB fast-download result matched exact title %q by %q among %d inspected results", bookCase.Title, bookCase.Author, inspected)
		}
		hash, hashErr := normalizeHash(book.Hash)
		if hashErr != nil {
			t.Fatalf("search result for %q had an invalid MD5 (error type %T)", bookCase.Title, hashErr)
		}
		if previous, duplicate := selectedHashes[hash]; duplicate {
			t.Fatalf("%q and %q resolved to the same MD5; the five selected books must have distinct file hashes", previous, bookCase.Title)
		}
		selectedHashes[hash] = bookCase.Title
		t.Logf("selected %q by %q: MD5 %s, English EPUB (%s)", bookCase.Title, bookCase.Author, hash, book.Size)

		// Use one title and extension across all five unique hashes to exercise
		// collision-safe unique naming while keeping every download in a temp dir.
		requestBook := *book
		requestBook.Title = sameDownloadTitle
		requestBook.Format = "epub"
		beforeFiles := snapshotLiveDedupeFiles(t, downloadDir)
		beforeRequests := counts.snapshot()
		downloadCtx, downloadCancel := context.WithTimeout(ctx, liveDedupeTimeout)
		result, downloadErr := liveDedupeClient(configuration.BaseURL, key, cookie, configuration.SessionFile, downloadDir, transport).DownloadBook(downloadCtx, &requestBook, liveDedupeTimeout, nil)
		downloadCancel()
		if downloadErr != nil {
			t.Fatalf("initial download for %q failed (error type %T)", bookCase.Title, downloadErr)
		}
		afterRequests := counts.snapshot()
		if afterRequests.api == beforeRequests.api || afterRequests.file == beforeRequests.file {
			t.Fatalf("initial download for %q did not exercise the fast API and file transfer (API requests=%d, file requests=%d)", bookCase.Title, afterRequests.api-beforeRequests.api, afterRequests.file-beforeRequests.file)
		}
		if _, existed := beforeFiles[result.Path]; existed {
			t.Fatalf("initial download for %q reused an existing filename %q", bookCase.Title, filepath.Base(result.Path))
		}
		verifyLiveDedupeFile(t, result.Path, result.Bytes, hash)
		assertLiveDedupeFilesUnchanged(t, beforeFiles)
		t.Logf("fresh download %q: MD5 %s, %d bytes; requests API=%d file=%d", bookCase.Title, hash, result.Bytes, afterRequests.api-beforeRequests.api, afterRequests.file-beforeRequests.file)

		// A local hash hit must ignore caller title/format changes and make no
		// HTTP calls at all, including no account lookup or fast-download API hit.
		secondRequest := *book
		secondRequest.Title = "Changed title reuse check"
		secondRequest.Format = "mobi"
		beforeReuse := counts.snapshot()
		reused, reuseErr := liveDedupeClient(configuration.BaseURL, key, cookie, configuration.SessionFile, downloadDir, transport).DownloadBook(ctx, &secondRequest, liveDedupeTimeout, nil)
		if reuseErr != nil {
			t.Fatalf("same-hash reuse for %q failed (error type %T)", bookCase.Title, reuseErr)
		}
		afterReuse := counts.snapshot()
		assertLiveDedupeNoRequests(t, bookCase.Title, beforeReuse, afterReuse)
		if reused.Path != result.Path || reused.Bytes != result.Bytes || reused.DownloadsLeft != nil {
			t.Fatalf("same-hash reuse for %q returned path/size/quota mismatch: same path=%t, bytes=%d/%d, downloads-left-is-nil=%t", bookCase.Title, reused.Path == result.Path, reused.Bytes, result.Bytes, reused.DownloadsLeft == nil)
		}
		verifyLiveDedupeFile(t, reused.Path, reused.Bytes, hash)

		downloads = append(downloads, downloadedBook{
			caseInfo: bookCase, book: book, path: result.Path, hash: hash, bytes: result.Bytes,
			apiCalls:  afterRequests.api - beforeRequests.api,
			fileCalls: afterRequests.file - beforeRequests.file,
		})
	}

	// Five books used the same filename stem and EPUB extension. Confirm that
	// each got its own path and that every previously downloaded file stayed intact.
	paths := make(map[string]bool, len(downloads))
	for _, book := range downloads {
		if paths[book.path] {
			t.Fatalf("two different hashes share output path %q", filepath.Base(book.path))
		}
		paths[book.path] = true
		if filepath.Ext(book.path) != ".epub" {
			t.Fatalf("same-title collision download has unexpected extension %q", filepath.Ext(book.path))
		}
		verifyLiveDedupeFile(t, book.path, book.bytes, book.hash)
	}

	// Move each result to an arbitrary name and extension, then use a fresh
	// Client to prove the directory scan finds the same bytes without network IO.
	for index := range downloads {
		book := &downloads[index]
		renamedPath := filepath.Join(downloadDir, fmt.Sprintf("renamed-live-copy-%02d.data", index+1))
		if err := os.Rename(book.path, renamedPath); err != nil {
			t.Fatalf("could not rename the local copy for %q (error type %T)", book.caseInfo.Title, err)
		}
		beforeReuse := counts.snapshot()
		renamedRequest := *book.book
		renamedRequest.Title = "Reuse after local rename"
		renamedRequest.Format = "txt"
		freshClient := liveDedupeClient(configuration.BaseURL, key, cookie, configuration.SessionFile, downloadDir, transport)
		result, reuseErr := freshClient.DownloadBook(ctx, &renamedRequest, liveDedupeTimeout, nil)
		if reuseErr != nil {
			t.Fatalf("renamed-file reuse for %q failed (error type %T)", book.caseInfo.Title, reuseErr)
		}
		afterReuse := counts.snapshot()
		assertLiveDedupeNoRequests(t, book.caseInfo.Title, beforeReuse, afterReuse)
		if result.Path != renamedPath || result.Bytes != book.bytes || result.DownloadsLeft != nil {
			t.Fatalf("renamed-file reuse for %q returned path/size/quota mismatch: same path=%t, bytes=%d/%d, downloads-left-is-nil=%t", book.caseInfo.Title, result.Path == renamedPath, result.Bytes, book.bytes, result.DownloadsLeft == nil)
		}
		verifyLiveDedupeFile(t, result.Path, result.Bytes, book.hash)
		book.path = renamedPath
		t.Logf("verified %q by %q: MD5 %s, %d bytes; fresh download requests API=%d file=%d; same-hash and renamed reuse requests=0", book.caseInfo.Title, book.caseInfo.Author, book.hash, book.bytes, book.apiCalls, book.fileCalls)
	}
}

func liveDedupeClient(baseURL, key, cookie, sessionFile, downloadDir string, transport http.RoundTripper) *Client {
	return mustClient(Config{
		BaseURL: baseURL, AutoBaseURL: true, SecretKey: key, AccountCookie: cookie,
		SessionFile: sessionFile, DownloadPath: downloadDir,
		HTTPClient: &http.Client{Transport: transport},
	})
}

func liveDedupeURLContains(req *http.Request, secret string) bool {
	if secret == "" {
		return false
	}
	if strings.Contains(req.URL.Path, secret) {
		return true
	}
	for _, values := range req.URL.Query() {
		for _, value := range values {
			if strings.Contains(value, secret) {
				return true
			}
		}
	}
	return false
}

func chooseLiveDedupeBook(results []*Book, want liveDedupeBookCase) *Book {
	wantedTitle := compactLiveDedupeText(want.Title)
	wantedAuthorWords := strings.Fields(strings.ToLower(want.Author))
	for _, book := range results {
		if book == nil || !book.FastDownload || book.PossibleIssues || !strings.EqualFold(book.Format, "EPUB") || compactLiveDedupeText(book.Title) != wantedTitle {
			continue
		}
		authorWords := make(map[string]bool)
		for _, word := range strings.FieldsFunc(strings.ToLower(book.Authors), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			authorWords[word] = true
		}
		matchesAuthor := len(wantedAuthorWords) > 0
		for _, word := range wantedAuthorWords {
			word = strings.Trim(word, ".,;:()[]{}'’\"")
			if word != "" && !authorWords[word] {
				matchesAuthor = false
				break
			}
		}
		if matchesAuthor {
			return book
		}
	}
	return nil
}

func compactLiveDedupeText(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

func snapshotLiveDedupeFiles(t *testing.T, folder string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(folder)
	if err != nil {
		t.Fatalf("could not inspect isolated download folder (error type %T)", err)
	}
	snapshot := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		path := filepath.Join(folder, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("could not read an earlier downloaded file (error type %T)", err)
		}
		snapshot[path] = data
	}
	return snapshot
}

func assertLiveDedupeFilesUnchanged(t *testing.T, before map[string][]byte) {
	t.Helper()
	for path, want := range before {
		got, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("a previously downloaded file changed or disappeared after another book was saved (error type %T)", err)
		}
	}
}

func verifyLiveDedupeFile(t *testing.T, path string, resultBytes int64, expectedHash string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read a downloaded or reused file (error type %T)", err)
	}
	hash := md5.Sum(data)
	if int64(len(data)) != resultBytes || hex.EncodeToString(hash[:]) != expectedHash {
		t.Fatalf("independent file check failed: bytes=%d, expected=%d, MD5 matches=%t", len(data), resultBytes, hex.EncodeToString(hash[:]) == expectedHash)
	}
}

func assertLiveDedupeNoRequests(t *testing.T, title string, before, after liveDedupeRequestSnapshot) {
	t.Helper()
	if after != before {
		t.Fatalf("local reuse for %q made HTTP requests: total=%d, archive=%d, fast API=%d, file=%d", title, after.total-before.total, after.archive-before.archive, after.api-before.api, after.file-before.file)
	}
}
