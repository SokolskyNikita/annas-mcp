package anna

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// failingReader fails every read, like a connection that dropped.
type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// resumableFile serves a file that each request may cut off after cut
// bytes, and answers Range requests as rangeStatus says.
type resumableFile struct {
	t           *testing.T
	body        []byte
	cut         func(request int, rangeStart int64) (int, bool)
	rangeStatus int
	mu          sync.Mutex
	requests    []string
}

func newResumableFile(t *testing.T) *resumableFile {
	body := make([]byte, 64<<10)
	for i := range body {
		body[i] = byte(rand.IntN(256))
	}
	return &resumableFile{t: t, body: body, rangeStatus: http.StatusPartialContent}
}

func (f *resumableFile) hash() string {
	sum := md5.Sum(f.body)
	return hex.EncodeToString(sum[:])
}

func (f *resumableFile) serve(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req.Header.Get("Range"))
	request := len(f.requests)
	f.mu.Unlock()
	header := http.Header{"Content-Type": {"application/octet-stream"}}
	status, start := http.StatusOK, int64(0)
	if spec := req.Header.Get("Range"); spec != "" && f.rangeStatus == http.StatusPartialContent {
		if _, err := fmt.Sscanf(spec, "bytes=%d-", &start); err != nil {
			f.t.Errorf("bad Range %q", spec)
		}
		status = http.StatusPartialContent
		header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(f.body)-1, len(f.body)))
	}
	rest := f.body[start:]
	var body io.Reader = bytes.NewReader(rest)
	if f.cut != nil {
		if n, ok := f.cut(request, start); ok {
			body = io.MultiReader(bytes.NewReader(rest[:n]), failingReader{errors.New("connection reset by peer")})
		}
	}
	return &http.Response{StatusCode: status, Header: header, ContentLength: int64(len(rest)), Body: io.NopCloser(body), Request: req}, nil
}

func (f *resumableFile) ranges() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.requests...)
}

func (f *resumableFile) client(t *testing.T, dir string) *Client {
	return mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: dir, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "annas.test" {
			return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
		}
		return f.serve(req)
	})}})
}

func TestDroppedDownloadContinuesWhereItStopped(t *testing.T) {
	t.Parallel()
	file := newResumableFile(t)
	// The first request drops after 20 KiB and the second after 10 more.
	file.cut = func(request int, _ int64) (int, bool) {
		switch request {
		case 1:
			return 20 << 10, true
		case 2:
			return 10 << 10, true
		}
		return 0, false
	}
	dir := t.TempDir()
	result, err := file.client(t, dir).DownloadBook(context.Background(), &Book{Hash: file.hash(), Title: "resumed", Format: "pdf"}, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"", "bytes=20480-", "bytes=30720-"}; strings.Join(file.ranges(), ",") != strings.Join(want, ",") {
		t.Fatalf("requests: %q, want %q", file.ranges(), want)
	}
	saved, err := os.ReadFile(result.Path)
	if err != nil || !bytes.Equal(saved, file.body) {
		t.Fatalf("resumed file differs: %d bytes, %v", len(saved), err)
	}
}

func TestStalledDownloadContinuesWhereItStopped(t *testing.T) {
	t.Parallel()
	// Advance the idle timer only while the fake response is stalled. Wall
	// clock delays on busy race-enabled runners must not interrupt the resume.
	synctest.Test(t, func(t *testing.T) {
		file := newResumableFile(t)
		dir := t.TempDir()
		client := mustClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: dir, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "annas.test" {
				return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
			}
			response, err := file.serve(req)
			if req.Header.Get("Range") == "" {
				response.Body = io.NopCloser(&stallingBody{ctx: req.Context(), prefix: strings.NewReader(string(file.body[:4096]))})
			}
			return response, err
		})}})
		client.idleTimeout = 50 * time.Millisecond
		result, err := client.DownloadBook(context.Background(), &Book{Hash: file.hash(), Format: "pdf"}, time.Minute, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := file.ranges(); len(got) != 2 || got[1] != "bytes=4096-" {
			t.Fatalf("requests: %q", got)
		}
		if saved, err := os.ReadFile(result.Path); err != nil || !bytes.Equal(saved, file.body) {
			t.Fatalf("resumed file differs: %d bytes, %v", len(saved), err)
		}
	})
}

func TestDownloadIsNotContinuedFromTheWrongPlace(t *testing.T) {
	t.Parallel()
	file := newResumableFile(t)
	// The server ignores Range and sends the whole file again, which must
	// not be appended to what was already written.
	file.rangeStatus = http.StatusOK
	file.cut = func(request int, _ int64) (int, bool) { return 20 << 10, request == 1 }
	result, err := file.client(t, t.TempDir()).DownloadBook(context.Background(), &Book{Hash: file.hash(), Format: "pdf"}, time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	// The resume is refused and the next server's download is used.
	if got := file.ranges(); len(got) != 3 || got[1] != "bytes=20480-" || got[2] != "" {
		t.Fatalf("requests: %q", got)
	}
	if saved, _ := os.ReadFile(result.Path); !bytes.Equal(saved, file.body) {
		t.Fatal("saved file differs")
	}
}

func TestDownloadResumesAtMostTwicePerServer(t *testing.T) {
	t.Parallel()
	file := newResumableFile(t)
	file.cut = func(int, int64) (int, bool) { return 1 << 10, true }
	dir := t.TempDir()
	_, err := file.client(t, dir).DownloadBook(context.Background(), &Book{Hash: file.hash(), Format: "pdf"}, time.Minute, nil)
	if err == nil || !strings.Contains(err.Error(), "download stopped after 3072 bytes: connection reset by peer") {
		t.Fatalf("wrong error: %v", err)
	}
	// Every server of every collection gets the request and two resumes.
	if got, want := len(file.ranges()), maxDownloadPaths*len(domainIndexes)*(1+maxResumes); got != want {
		t.Fatalf("%d file requests, want %d", got, want)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("failed download left files behind: %v", entries)
	}
}
