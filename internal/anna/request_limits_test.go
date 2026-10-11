package anna

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

// hang answers only when the request is cancelled, like a server that
// accepted the connection and went quiet.
func hang(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}

// stallingBody sends prefix and then nothing until its request is cancelled.
type stallingBody struct {
	ctx    context.Context
	prefix *strings.Reader
}

func (b *stallingBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stallingBody) Close() error { return nil }

func TestQuietMirrorFailsBeforeTheDeadlineAndIsReplaced(t *testing.T) {
	t.Parallel()
	resolves := 0
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", Resolver: func(context.Context) (string, error) {
		resolves++
		if resolves == 1 {
			return "annas-archive.gl", nil
		}
		return "annas-archive.pk", nil
	}, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "annas-archive.gl" {
			return hang(req)
		}
		return fixtureResponse(t, req, http.StatusOK, "archive_page.html", "text/html", nil), nil
	})}})
	client.headerTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute)
	if apperr.CodeOf(err) != apperr.RequestTimeout || !strings.Contains(err.Error(), "annas-archive.gl did not answer within 50ms") {
		t.Fatalf("quiet mirror: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s for a quiet mirror", elapsed)
	}
	// Unlike the operation's own deadline, the quiet mirror counts against
	// it, so the next call selects again.
	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if resolves != 2 {
		t.Fatalf("quiet mirror was kept: %d selections", resolves)
	}
}

func TestCallerDeadlineDoesNotCountAgainstTheMirror(t *testing.T) {
	t.Parallel()
	resolves := 0
	client := NewClient(Config{AutoBaseURL: true, AccountCookie: "aa_account_id2=test", Resolver: func(context.Context) (string, error) {
		resolves++
		return "annas-archive.gl", nil
	}, HTTPClient: &http.Client{Transport: roundTripFunc(hang)}})

	if _, err := client.FindBook(context.Background(), "example", SearchOptions{}, 50*time.Millisecond); apperr.CodeOf(err) != apperr.RequestTimeout {
		t.Fatalf("deadline: %v", err)
	}
	if _, err := client.baseURLFor(context.Background()); err != nil || resolves != 1 {
		t.Fatalf("the caller's deadline replaced the mirror: %d selections, %v", resolves, err)
	}
}

func TestStalledDownloadIsAbandoned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", SecretKey: "test-secret", DownloadPath: dir, HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "annas.test" {
			return fixtureResponse(t, req, http.StatusOK, "fast_download.json", "application/json", nil), nil
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/pdf"}}, ContentLength: 1 << 20,
			Body: &stallingBody{ctx: req.Context(), prefix: strings.NewReader("%PDF-1.7 partial")}, Request: req}, nil
	})}})
	client.idleTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := client.DownloadBook(context.Background(), &Book{Hash: "0123456789abcdef0123456789abcdef", Format: "pdf"}, time.Minute, nil)
	if apperr.CodeOf(err) != apperr.RequestTimeout || !strings.Contains(err.Error(), "downloads.example stopped sending data for 50ms") {
		t.Fatalf("stalled download: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("waited %s for a stalled download", elapsed)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("stalled download left files behind: %v", entries)
	}
}

func TestIdleLimitResetsWhileDataArrives(t *testing.T) {
	t.Parallel()
	reader, writer := io.Pipe()
	go func() {
		// Each pause is well under the limit; all of them together are not.
		for range 8 {
			time.Sleep(25 * time.Millisecond)
			writer.Write([]byte("chunk"))
		}
		writer.Close()
	}()
	body := newIdleBody(reader, func() { reader.CloseWithError(errors.New("cancelled")) }, 100*time.Millisecond, "files.example")
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil || string(data) != strings.Repeat("chunk", 8) {
		t.Fatalf("slow but steady body: %q %v", data, err)
	}
}

func TestHangingDOIOrgDoesNotEndTheLookup(t *testing.T) {
	t.Parallel()
	client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Host == "doi.org":
			return hang(req)
		case req.URL.Path == "/scidb/"+outageDOI:
			return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
		case req.URL.Path == "/search":
			return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
		case req.URL.Path == "/md5/0123456789abcdef0123456789abcdef":
			return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", map[string]string{"<head>": `<head><meta name="citation_doi" content="` + outageDOI + `">`}), nil
		}
		return nil, errors.New("unexpected request: " + req.URL.String())
	})}})
	client.doiTimeout = 50 * time.Millisecond

	paper, err := client.LookupDOI(context.Background(), outageDOI, time.Minute)
	if err != nil || paper.Hash != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("lookup without DOI.org: %+v %v", paper, err)
	}
}
