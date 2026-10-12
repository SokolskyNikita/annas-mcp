package anna_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(name, ".gz") {
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		data, err = io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
	}
	return data
}
func response(req *http.Request, body []byte, contentType string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}
func TestSDKConstructionAndErrors(t *testing.T) {
	calls := 0
	transport := transportFunc(func(r *http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") })
	for _, config := range []anna.Config{{BaseURL: "http://unsafe.example"}, {AccountCookie: "other=value"}, {DownloadPath: "relative"}, {SessionFile: "relative"}} {
		config.HTTPClient = &http.Client{Transport: transport}
		client, err := anna.NewClient(config)
		var coded *anna.Error
		if client != nil || !errors.As(err, &coded) || anna.CodeOf(err) != anna.CodeConfig || coded.Field == "" {
			t.Fatalf("configuration error: %v", err)
		}
	}
	client, err := anna.NewClient(anna.Config{HTTPClient: &http.Client{Transport: transport}})
	if err != nil || calls != 0 {
		t.Fatalf("construction: calls=%d err=%v", calls, err)
	}
	_, err = client.FindBook(context.Background(), "example", anna.SearchOptions{}, 0)
	if anna.CodeOf(err) != anna.CodeConfig || calls != 0 {
		t.Fatalf("access: calls=%d err=%v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.FindBook(ctx, "example", anna.SearchOptions{}, 0)
	if !errors.Is(err, context.Canceled) || anna.CodeOf(err) != anna.RequestTimeout {
		t.Fatalf("cancellation: %v", err)
	}
	wrapped := anna.WrapError(anna.Upstream, "lookup", context.DeadlineExceeded)
	if !errors.Is(wrapped, context.DeadlineExceeded) || anna.CodeOf(wrapped) != anna.RequestTimeout {
		t.Fatal(wrapped)
	}
}
func TestSDKSearchContractAndTransportOwnership(t *testing.T) {
	// literal: this is the path to an existing captured fixture, not inline test data.
	body := fixture(t, "internal/parse/testdata/pages/search_filtered_partial.html.gz")
	calls := 0
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if _, ok := req.Context().Deadline(); ok {
			t.Error("zero timeout added an operation deadline")
		}
		if req.URL.Query().Get("page") != "2" || req.URL.Query().Get("lang") != "zh-Hant" {
			t.Errorf("query: %s", req.URL.RawQuery)
		}
		return response(req, body, "text/html"), nil
	})
	httpClient := &http.Client{Transport: transport}
	client, err := anna.NewClient(anna.Config{BaseURL: "annas.test", AccountCookie: "test", HTTPClient: httpClient})
	if err != nil {
		t.Fatal(err)
	}
	// Changing the supplied client after construction must not change SDK settings.
	httpClient.Timeout = time.Nanosecond
	options := anna.SearchOptions{Page: 2, Language: []string{"ZH-hant"}, Source: []string{"lgli"}}
	page, err := client.FindBook(context.Background(), "example", options, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Results) != 9 || len(page.Partial) != 41 || page.Total != 9 || page.TotalIsLowerBound || page.MorePages {
		t.Fatalf("page contract: %+v", page)
	}
	if !reflect.DeepEqual(options.Language, []string{"ZH-hant"}) || httpClient.Timeout != time.Nanosecond || httpClient.CheckRedirect != nil || calls != 1 {
		t.Fatal("caller-owned configuration was changed")
	}
	if page.Results[0].Stats != nil {
		t.Fatal("unknown statistics became known zero")
	}
}
func TestSDKVerifiedDownloadAndLocalReuse(t *testing.T) {
	body := fixture(t, "testdata/paper.pdf")
	grant := fixture(t, "testdata/fast_download.json")
	digest := md5.Sum(body)
	hash := hex.EncodeToString(digest[:])
	calls := 0
	transport := transportFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.URL.Host {
		case "annas.test":
			return response(req, grant, "application/json"), nil
		case "downloads.example":
			if req.Header.Get("Cookie") != "" || req.URL.Query().Has("key") {
				t.Error("credentials leaked to file host")
			}
			return response(req, body, "application/pdf"), nil
		default:
			return nil, fmt.Errorf("unexpected host %s", req.URL.Host)
		}
	})
	dir := t.TempDir()
	client, err := anna.NewClient(anna.Config{BaseURL: "annas.test", SecretKey: "test-key", AccountCookie: "test", DownloadPath: dir, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	progress := int64(0)
	result, err := client.DownloadBook(context.Background(), &anna.Book{Hash: hash, Title: "Example", Format: "pdf"}, 0, func(done, total int64) { progress = done })
	if err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(saved, body) || md5.Sum(saved) != digest || result.Bytes != int64(len(body)) || progress != int64(len(body)) || calls != 2 {
		t.Fatalf("download: %+v calls=%d", result, calls)
	}
	local, err := anna.NewClient(anna.Config{DownloadPath: dir, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	reused, err := local.DownloadBook(context.Background(), &anna.Book{Hash: hash}, 0, nil)
	if err != nil || reused.Path != result.Path || calls != 2 || reused.DownloadsLeft != nil {
		t.Fatalf("local reuse: %+v %v", reused, err)
	}
}
func TestSDKValidationBeforeAccess(t *testing.T) {
	client, err := anna.NewClient(anna.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// literal: each typed option independently exercises one validation rule.
	for _, options := range []anna.SearchOptions{{Page: -1}, {Index: "bad"}, {Sort: "bad"}, {Content: []string{"bad"}}, {Language: []string{"bad code"}}, {Format: []string{"../pdf"}}, {Source: []string{"bad source"}}, {Access: []string{"bad"}}, {Fields: []anna.SearchField{{Name: "bad", Value: "x"}}}, {Fields: []anna.SearchField{{Name: "year", Value: "yesterday"}}}, {Index: "meta", Sort: "largest"}} {
		_, err := client.FindBook(context.Background(), "example", options, 0)
		if anna.CodeOf(err) != anna.InvalidArgument {
			t.Fatalf("validation %+v: %v", options, err)
		}
	}
	_, err = client.DownloadArticle(context.Background(), anna.ArticleDownloadOptions{DOI: "10.1000/example", Hash: "hash"}, 0, nil)
	if anna.CodeOf(err) != anna.InvalidArgument {
		t.Fatal(err)
	}
}
