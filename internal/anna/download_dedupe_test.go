package anna

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

type dedupeRequestCounts struct {
	fastAPI atomic.Int32
	file    atomic.Int32
}

func newDedupeFixtureClient(t testing.TB, dir string, requests *dedupeRequestCounts) *Client {
	t.Helper()
	return NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  dir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "annas.test":
				if req.URL.Path != "/dyn/api/fast_download.json" {
					return nil, fmt.Errorf("unexpected archive URL: %s", req.URL)
				}
				requests.fastAPI.Add(1)
				return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
			case "downloads.example":
				requests.file.Add(1)
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
			default:
				return nil, fmt.Errorf("unexpected download host: %s", req.URL.Host)
			}
		})},
	})
}

func TestDownloadBookReusesMatchingFileInFreshClient(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	for _, tc := range []struct {
		name     string
		filename string
	}{
		{name: "renamed", filename: "renamed-copy.data"},
		{name: "hidden", filename: ".annas-mcp-notes.pdf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			existing := filepath.Join(dir, tc.filename)
			if err := os.WriteFile(existing, body, 0o644); err != nil {
				t.Fatal(err)
			}

			var requests dedupeRequestCounts
			client := NewClient(Config{
				DownloadPath: dir,
				HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					requests.fastAPI.Add(1)
					return nil, errors.New("a local match should not make a request")
				})},
			})
			result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Different title", Format: "epub"}, callTimeout, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Path != existing || result.Bytes != int64(len(body)) {
				t.Fatalf("unexpected reused file: %+v", result)
			}
			if result.DownloadsLeft != nil {
				t.Fatalf("reused file reported downloads left: %v", result.DownloadsLeft)
			}
			if requests.fastAPI.Load() != 0 || requests.file.Load() != 0 {
				t.Fatalf("reuse made requests: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
			}
		})
	}
}

func TestDownloadBookIgnoresSubdirectoriesSymlinksAndTemporaryFiles(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(nested, "matching.pdf")
	if err := os.WriteFile(nestedFile, body, 0o644); err != nil {
		t.Fatal(err)
	}
	tempFile := filepath.Join(dir, ".annas-mcp-download-part.part")
	if err := os.WriteFile(tempFile, body, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "matching.pdf")
	if err := os.WriteFile(outside, body, 0o644); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "linked.pdf")
	symlinkCreated := os.Symlink(outside, symlink) == nil

	var requests dedupeRequestCounts
	client := newDedupeFixtureClient(t, dir, &requests)
	result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Selected", Format: "pdf"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.Path) != dir || filepath.Base(result.Path) != "Selected.pdf" {
		t.Fatalf("did not download after ignoring incomplete locations: %q", result.Path)
	}
	if requests.fastAPI.Load() != 1 || requests.file.Load() != 1 {
		t.Fatalf("ignored locations did not trigger a download: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
	}
	if result.Bytes != int64(len(body)) {
		t.Fatalf("downloaded bytes = %d, want %d", result.Bytes, len(body))
	}
	if data, err := os.ReadFile(nestedFile); err != nil || !bytes.Equal(data, body) {
		t.Fatalf("nested file changed: %v", err)
	}
	if data, err := os.ReadFile(tempFile); err != nil || !bytes.Equal(data, body) {
		t.Fatalf("temporary file changed: %v", err)
	}
	if symlinkCreated {
		info, err := os.Lstat(symlink)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("matching symlink changed: %v", err)
		}
	}
}

func TestDownloadBookDownloadsWhenFolderDoesNotExist(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	dir := filepath.Join(t.TempDir(), "downloads")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("download folder already exists: %v", err)
	}
	var requests dedupeRequestCounts
	client := newDedupeFixtureClient(t, dir, &requests)
	result, err := client.DownloadBook(context.Background(), &Book{Hash: fixtureHash(t, "paper.pdf"), Title: "Selected", Format: "pdf"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(result.Path) != dir || result.Bytes != int64(len(body)) {
		t.Fatalf("unexpected download result: %+v", result)
	}
	if requests.fastAPI.Load() != 1 || requests.file.Load() != 1 {
		t.Fatalf("new folder did not trigger a download: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
	}
}

func TestDownloadBookDoesNotReuseModifiedOrDeletedFiles(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		delete bool
	}{
		{name: "modified"},
		{name: "deleted", delete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := readFixture(t, "paper.pdf")
			hash := fixtureHash(t, "paper.pdf")
			dir := t.TempDir()
			existing := filepath.Join(dir, "Selected.pdf")
			if err := os.WriteFile(existing, body, 0o644); err != nil {
				t.Fatal(err)
			}
			if tc.delete {
				if err := os.Remove(existing); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(existing, []byte("modified"), 0o644); err != nil {
				t.Fatal(err)
			}

			var requests dedupeRequestCounts
			client := newDedupeFixtureClient(t, dir, &requests)
			result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Selected", Format: "pdf"}, callTimeout, nil)
			if err != nil {
				t.Fatal(err)
			}
			if requests.fastAPI.Load() != 1 || requests.file.Load() != 1 {
				t.Fatalf("changed or missing file was reused: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
			}
			if result.Bytes != int64(len(body)) {
				t.Fatalf("downloaded bytes = %d, want %d", result.Bytes, len(body))
			}
			if !tc.delete {
				if result.Path == existing {
					t.Fatal("download replaced a modified file")
				}
				if data, err := os.ReadFile(existing); err != nil || string(data) != "modified" {
					t.Fatalf("modified file was not preserved: %v", err)
				}
			}
			if data, err := os.ReadFile(result.Path); err != nil || !bytes.Equal(data, body) {
				t.Fatalf("downloaded file has wrong contents: %v", err)
			}
		})
	}
}

func TestDownloadBookFindsFreeNameAfterManyCollisions(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	occupied := []string{filepath.Join(dir, "Selected.pdf"), filepath.Join(dir, "Selected-"+hash[:8]+".pdf")}
	for i := 2; i <= 22; i++ {
		occupied = append(occupied, filepath.Join(dir, fmt.Sprintf("Selected-%s-%d.pdf", hash[:8], i)))
	}
	for _, path := range occupied {
		if err := os.WriteFile(path, []byte("occupied"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var requests dedupeRequestCounts
	client := newDedupeFixtureClient(t, dir, &requests)
	result, err := client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Selected", Format: "pdf"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range occupied {
		if result.Path == path {
			t.Fatalf("download reused an occupied path: %q", result.Path)
		}
		if data, err := os.ReadFile(path); err != nil || string(data) != "occupied" {
			t.Fatalf("occupied file changed: %v", err)
		}
	}
	if data, err := os.ReadFile(result.Path); err != nil || !bytes.Equal(data, body) {
		t.Fatalf("downloaded file has wrong contents: %v", err)
	}
	if requests.fastAPI.Load() != 1 || requests.file.Load() != 1 {
		t.Fatalf("collision download requests = %d/%d", requests.fastAPI.Load(), requests.file.Load())
	}
}

func TestDownloadBookHonorsCancellationWithMatchingFile(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "matching.bin"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	var requests dedupeRequestCounts
	client := newDedupeFixtureClient(t, dir, &requests)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.DownloadBook(ctx, &Book{Hash: fixtureHash(t, "paper.pdf"), Title: "Paper", Format: "pdf"}, callTimeout, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DownloadBook returned %v", err)
	}
	if requests.fastAPI.Load() != 0 || requests.file.Load() != 0 {
		t.Fatalf("canceled request reached network: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
	}
}

func TestConcurrentBookDownloadsForHashShareOneTransfer(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	var requests dedupeRequestCounts
	firstGrant := make(chan struct{})
	secondGrant := make(chan struct{}, 1)
	releaseGrant := make(chan struct{})
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		SecretKey:     "test-secret",
		DownloadPath:  dir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "annas.test":
				if req.URL.Path != "/dyn/api/fast_download.json" {
					return nil, fmt.Errorf("unexpected archive URL: %s", req.URL)
				}
				count := requests.fastAPI.Add(1)
				if count == 1 {
					close(firstGrant)
					<-releaseGrant
				} else {
					select {
					case secondGrant <- struct{}{}:
					default:
					}
				}
				return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
			case "downloads.example":
				requests.file.Add(1)
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
			default:
				return nil, fmt.Errorf("unexpected download host: %s", req.URL.Host)
			}
		})},
	})

	start := make(chan struct{})
	results := make([]DownloadResult, 2)
	errs := make([]error, 2)
	var wait sync.WaitGroup
	for i := range results {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			<-start
			results[i], errs[i] = client.DownloadBook(context.Background(), &Book{Hash: hash, Title: "Concurrent", Format: "pdf"}, callTimeout, nil)
		}(i)
	}
	close(start)
	<-firstGrant
	select {
	case <-secondGrant:
	case <-time.After(100 * time.Millisecond):
	}
	close(releaseGrant)
	wait.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if requests.fastAPI.Load() != 1 || requests.file.Load() != 1 {
		t.Fatalf("same-hash calls made multiple transfers: fast API=%d file=%d", requests.fastAPI.Load(), requests.file.Load())
	}
	if results[0].Path != results[1].Path || results[0].Bytes != int64(len(body)) || results[1].Bytes != int64(len(body)) {
		t.Fatalf("calls did not share the completed file: %+v / %+v", results[0], results[1])
	}
}

func TestDownloadArticleByDOIReusesLocalFileAfterLookup(t *testing.T) {
	t.Parallel()

	body := readFixture(t, "paper.pdf")
	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	existing := filepath.Join(dir, "local-paper.data")
	if err := os.WriteFile(existing, body, 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		mode := info.Mode().Perm() &^ 0o222
		if err := os.Chmod(dir, mode); err != nil {
			t.Skipf("could not make the download folder read-only: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(dir, info.Mode().Perm()); err != nil {
				t.Errorf("restore download folder permissions: %v", err)
			}
		})
	}
	var lookupCalls, fastAPICalls, fileCalls atomic.Int32
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		DownloadPath:  dir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "annas.test":
				switch req.URL.Path {
				case "/scidb/10.1000/example/":
					lookupCalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
				case "/md5/" + hash:
					lookupCalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, sciDBRecordPage, "text/html", nil), nil
				case "/dyn/api/fast_download.json":
					fastAPICalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
				default:
					return nil, fmt.Errorf("unexpected archive URL: %s", req.URL)
				}
			case "downloads.example":
				fileCalls.Add(1)
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
			default:
				return nil, fmt.Errorf("unexpected download host: %s", req.URL.Host)
			}
		})},
	})

	result, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, callTimeout, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Path != existing || result.Bytes != int64(len(body)) || result.DownloadsLeft != nil {
		t.Fatalf("unexpected local DOI reuse: %+v", result)
	}
	if lookupCalls.Load() != 2 || fastAPICalls.Load() != 0 || fileCalls.Load() != 0 {
		t.Fatalf("DOI lookup/download requests = %d/%d/%d, want 2/0/0", lookupCalls.Load(), fastAPICalls.Load(), fileCalls.Load())
	}
}

func TestDownloadArticleByDOIMissWithoutAPIKeyStopsBeforeDownload(t *testing.T) {
	t.Parallel()

	hash := fixtureHash(t, "paper.pdf")
	dir := t.TempDir()
	var lookupCalls, fastAPICalls, fileCalls atomic.Int32
	client := NewClient(Config{
		BaseURL:       "annas.test",
		AccountCookie: "aa_account_id2=test",
		DownloadPath:  dir,
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			switch req.URL.Host {
			case "annas.test":
				switch req.URL.Path {
				case "/scidb/10.1000/example/":
					lookupCalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, sciDBPage, "text/html", map[string]string{sciDBHash: hash}), nil
				case "/md5/" + hash:
					lookupCalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, sciDBRecordPage, "text/html", nil), nil
				case "/dyn/api/fast_download.json":
					fastAPICalls.Add(1)
					return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
				default:
					return nil, fmt.Errorf("unexpected archive URL: %s", req.URL)
				}
			case "downloads.example":
				fileCalls.Add(1)
				return fixtureResponse(t, req, http.StatusOK, "paper.pdf", "application/pdf", nil), nil
			default:
				return nil, fmt.Errorf("unexpected download host: %s", req.URL.Host)
			}
		})},
	})

	_, err := client.DownloadArticle(context.Background(), ArticleDownloadOptions{DOI: "10.1000/example"}, callTimeout, nil)
	if apperr.CodeOf(err) != apperr.Config {
		t.Fatalf("DownloadArticle returned %v, want config error", err)
	}
	if lookupCalls.Load() != 2 || fastAPICalls.Load() != 0 || fileCalls.Load() != 0 {
		t.Fatalf("DOI miss requests = %d/%d/%d, want 2/0/0", lookupCalls.Load(), fastAPICalls.Load(), fileCalls.Load())
	}
}
