package filetransfer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func response(r *http.Request, body []byte) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: r}
}

func clientFor(t *testing.T, transport testTransport) *Client {
	t.Helper()
	c, err := New(Config{Directory: t.TempDir(), CacheDir: t.TempDir(), APIKey: "test-key", HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func requestFor(artifact string) Request {
	return Request{Key: "W123-hosted-" + artifact, URL: "https://content.openalex.org/works/W123.pdf", Title: "Research paper", Artifact: artifact, Hosted: true}
}

func TestConcurrentDownloadAndRestartReuse(t *testing.T) {
	body := fixture(t, "sample.pdf")
	var calls atomic.Int32
	c := clientFor(t, func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("hosted content missing credential")
		}
		return response(r, body), nil
	})
	var wg sync.WaitGroup
	results := make(chan Result, 6)
	for range 6 {
		wg.Go(func() {
			result, err := c.Download(context.Background(), requestFor("pdf"))
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		})
	}
	wg.Wait()
	close(results)
	var filename string
	for result := range results {
		if filename != "" && filename != result.Path {
			t.Error("duplicate finalized files")
		}
		filename = result.Path
		if result.Bytes != int64(len(body)) || result.SHA256 == "" {
			t.Error("missing artifact evidence")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("got %d content calls", calls.Load())
	}
	restarted, err := New(c.config)
	if err != nil {
		t.Fatal(err)
	}
	result, err := restarted.Download(context.Background(), requestFor("pdf"))
	if err != nil || !result.Reused || result.Path != filename {
		t.Fatalf("restart result=%+v error=%v", result, err)
	}
	if calls.Load() != 1 {
		t.Error("restart downloaded content")
	}
	if err := os.WriteFile(filename, bytes.Repeat([]byte("x"), len(body)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Cached(context.Background(), requestFor("pdf").Key); ok {
		t.Error("accepted modified local bytes")
	}
	newResult, err := c.Download(context.Background(), requestFor("pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if newResult.Path == filename || calls.Load() != 2 {
		t.Error("modified file should cause a unique new download")
	}
}

func TestStructuredXMLValidation(t *testing.T) {
	for _, name := range []string{"sample.xml", "legacy.xml", "rejected.xml"} {
		t.Run(name, func(t *testing.T) {
			body := fixture(t, name)
			c := clientFor(t, func(r *http.Request) (*http.Response, error) { return response(r, body), nil })
			result, err := c.Download(context.Background(), requestFor("tei_xml"))
			if name == "rejected.xml" {
				if err == nil {
					t.Error("accepted external entity")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if result.MIMEType != "application/xml" || filepath.Ext(result.Path) != ".xml" {
				t.Errorf("wrong XML result %+v", result)
			}
		})
	}
}

func TestRejectedResponsesLeaveNoArtifact(t *testing.T) {
	var cases []struct {
		Name, Body, Artifact string
		Status               int
	}
	if err := json.Unmarshal(fixture(t, "response_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			c := clientFor(t, func(r *http.Request) (*http.Response, error) {
				resp := response(r, []byte(tc.Body))
				resp.StatusCode = tc.Status
				return resp, nil
			})
			if _, err := c.Download(context.Background(), requestFor(tc.Artifact)); err == nil {
				t.Fatal("accepted invalid response")
			}
			files, err := os.ReadDir(c.config.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 0 {
				t.Error("failed download left a visible artifact")
			}
		})
	}
}

func TestPublicURLAndAddressValidation(t *testing.T) {
	var addresses []struct {
		Address string
		Public  bool
	}
	if err := json.Unmarshal(fixture(t, "address_cases.json"), &addresses); err != nil {
		t.Fatal(err)
	}
	for _, tc := range addresses {
		if got := publicIP(netip.MustParseAddr(tc.Address)); got != tc.Public {
			t.Errorf("%s public=%v", tc.Address, got)
		}
	}
	var urls []struct {
		URL    string
		Hosted bool
	}
	if err := json.Unmarshal(fixture(t, "url_cases.json"), &urls); err != nil {
		t.Fatal(err)
	}
	for _, tc := range urls {
		if validateURL(tc.URL, tc.Hosted) == nil {
			t.Errorf("accepted %s", tc.URL)
		}
	}
	if validateURL(requestFor("pdf").URL, true) != nil {
		t.Error("rejected official content URL")
	}
}

func TestCrossOriginRedirectStripsCredentials(t *testing.T) {
	body := fixture(t, "sample.pdf")
	c := clientFor(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "content.openalex.org" {
			resp := response(r, nil)
			resp.StatusCode = http.StatusFound
			resp.Header.Set("Location", "https://repository.example/paper.pdf")
			return resp, nil
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Error("credentials reached repository")
		}
		return response(r, body), nil
	})
	if _, err := c.Download(context.Background(), requestFor("pdf")); err != nil {
		t.Fatal(err)
	}
}

func TestConfigCancellationAndTruncation(t *testing.T) {
	c, err := New(Config{})
	if err != nil {
		t.Fatal("metadata initialization should allow empty destination")
	}
	if _, err := c.Download(context.Background(), requestFor("pdf")); err == nil {
		t.Error("empty destination downloaded")
	}
	if _, ok := c.Cached(context.Background(), "key"); ok {
		t.Error("empty destination cache")
	}
	body := fixture(t, "sample.pdf")
	c = clientFor(t, func(r *http.Request) (*http.Response, error) {
		resp := response(r, body)
		resp.ContentLength++
		return resp, nil
	})
	if _, err := c.Download(context.Background(), requestFor("pdf")); err == nil {
		t.Error("accepted truncated body")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Download(ctx, requestFor("pdf")); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation=%v", err)
	}
	if _, err := c.Download(context.Background(), requestFor("invalid")); err == nil {
		t.Error("accepted invalid artifact")
	}
	c.config.APIKey = ""
	if _, err := c.Download(context.Background(), requestFor("pdf")); err == nil {
		t.Error("hosted GET without key")
	}
}

func TestCacheSymlinkAndLeaseOwnership(t *testing.T) {
	body := fixture(t, "sample.pdf")
	c := clientFor(t, func(r *http.Request) (*http.Response, error) { return response(r, body), nil })
	result, err := c.Download(context.Background(), requestFor("pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(result.Path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "paper.pdf")
	if err := os.WriteFile(outside, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, result.Path); err == nil {
		if _, ok := c.Cached(context.Background(), requestFor("pdf").Key); ok {
			t.Error("reused symlink")
		}
	}
	unlock, owned, err := c.lock(context.Background(), "lease")
	if err != nil {
		t.Fatal(err)
	}
	lock := c.entryPath("lease") + ".lock"
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	if owned() {
		t.Error("lease accepted replacement owner")
	}
	unlock()
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("old owner removed replacement lease")
	}
}

func TestCopyBoundsAndCancellation(t *testing.T) {
	var output bytes.Buffer
	if _, err := Copy(context.Background(), &output, strings.NewReader("abcdef"), 3, nil); err == nil {
		t.Error("copy exceeded limit")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if _, err := Copy(ctx, &output, strings.NewReader("data"), 4, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Error(err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrPermission }

func TestFailedStreamAndLocalTarget(t *testing.T) {
	c := clientFor(t, func(r *http.Request) (*http.Response, error) {
		resp := response(r, nil)
		resp.Body = io.NopCloser(failingReader{})
		resp.ContentLength = -1
		return resp, nil
	})
	_, err := c.Download(context.Background(), requestFor("tei_xml"))
	var coded *Error
	if !errors.As(err, &coded) || coded.ErrorCode() != "UPSTREAM" || coded.Error() == "" || coded.Unwrap() == nil {
		t.Errorf("source error=%v", err)
	}
	if _, err := Copy(context.Background(), failingWriter{}, strings.NewReader("data"), 4, nil); !errors.Is(err, os.ErrPermission) {
		t.Error("write error lost", err)
	}
	file := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c.config.Directory = file
	_, err = c.Download(context.Background(), requestFor("tei_xml"))
	if !errors.As(err, &coded) || coded.Code != "LOCAL_IO" {
		t.Errorf("target error=%v", err)
	}
}

func TestLeaseWaitCancellationAndCrashRecovery(t *testing.T) {
	c := clientFor(t, func(r *http.Request) (*http.Response, error) { return response(r, fixture(t, "sample.pdf")), nil })
	unlock, _, err := c.lock(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := c.lock(ctx, "key"); !errors.Is(err, context.DeadlineExceeded) {
		t.Error("lease wait ignored deadline")
	}
	unlock()
	lock := c.entryPath("key") + ".lock"
	if err := os.Mkdir(lock, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-5 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	unlock, owned, err := c.lock(context.Background(), "key")
	if err != nil {
		t.Fatal(err)
	}
	if !owned() {
		t.Error("failed to recover abandoned writer")
	}
	unlock()
}

func TestDefaultTransportRejectsPrivateDial(t *testing.T) {
	c, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.http.Transport == nil || c.http.Jar != nil {
		t.Error("missing isolated production transport")
	}
	if _, err := publicDial(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Error("dialed loopback")
	}
	if _, err := publicDial(context.Background(), "tcp", "bad-address"); err == nil {
		t.Error("accepted invalid address")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := publicDial(ctx, "tcp", "8.8.8.8:443"); err == nil {
		t.Error("ignored cancelled dial")
	}
}
