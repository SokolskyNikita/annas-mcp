package parse

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestDownloadFormatUsesFilenameMediaTypeAndURL(t *testing.T) {
	t.Parallel()

	withFilename := &http.Response{Header: http.Header{"Content-Disposition": {`attachment; filename="paper.epub"`}, "Content-Type": {"application/octet-stream"}}}
	if got := DownloadFormat(withFilename.Header, requestURLOf(withFilename)); got != "epub" {
		t.Fatalf("filename format = %q", got)
	}
	withMediaType := &http.Response{Header: http.Header{"Content-Type": {"application/pdf; charset=binary"}}}
	if got := DownloadFormat(withMediaType.Header, requestURLOf(withMediaType)); got != "pdf" {
		t.Fatalf("media type format = %q", got)
	}
	// Anna's file servers: bare "attachment", generic type, filename in the URL.
	request := httptest.NewRequest(http.MethodGet, "https://cdn.example/d3/y/abc/Pride%20--%20Anna%E2%80%99s%20Archive.azw3", nil)
	withURLPath := &http.Response{Header: http.Header{"Content-Disposition": {"attachment"}, "Content-Type": {"application/octet-stream"}}, Request: request}
	if got := DownloadFormat(withURLPath.Header, requestURLOf(withURLPath)); got != "azw3" {
		t.Fatalf("URL path format = %q", got)
	}
	withoutHints := &http.Response{Header: http.Header{"Content-Type": {"application/octet-stream"}}, Request: httptest.NewRequest(http.MethodGet, "https://cdn.example/file", nil)}
	if got := DownloadFormat(withoutHints.Header, requestURLOf(withoutHints)); got != "bin" {
		t.Fatalf("fallback format = %q", got)
	}
}

func requestURLOf(response *http.Response) *url.URL {
	if response.Request == nil {
		return nil
	}
	return response.Request.URL
}

func TestContentRangeStart(t *testing.T) {
	t.Parallel()

	for value, want := range map[string]int64{"bytes 1000-1999/2000": 1000, "bytes 0-9/*": 0} {
		if got, ok := ContentRangeStart(value); !ok || got != want {
			t.Errorf("ContentRangeStart(%q) = %d, %v", value, got, ok)
		}
	}
	for _, value := range []string{"", "items 0-9/10", "bytes -5/10", "bytes x-9/10", "bytes 9"} {
		if _, ok := ContentRangeStart(value); ok {
			t.Errorf("ContentRangeStart(%q) accepted a malformed range", value)
		}
	}
}

func TestLooksLikeHTML(t *testing.T) {
	t.Parallel()

	if !LooksLikeHTML("text/html; charset=utf-8", nil) || !LooksLikeHTML("application/xhtml+html", nil) {
		t.Fatal("an HTML media type was not recognized")
	}
	if !LooksLikeHTML("application/octet-stream", []byte("  <!DOCTYPE html><html>")) || !LooksLikeHTML("", []byte("<html lang=en>")) {
		t.Fatal("an HTML body was not recognized")
	}
	if LooksLikeHTML("application/pdf", []byte("%PDF-1.7")) || LooksLikeHTML("invalid;;", nil) {
		t.Fatal("a PDF was taken for HTML")
	}
}

func TestExtensionFromMediaType(t *testing.T) {
	t.Parallel()

	for contentType, want := range map[string]string{
		"application/pdf": "pdf", "application/octet-stream": "", "binary/octet-stream": "",
		"application/x-not-a-real-type": "", "invalid;;": "", "": "",
	} {
		if got := extensionFromMediaType(contentType); got != want {
			t.Errorf("extensionFromMediaType(%q) = %q, want %q", contentType, got, want)
		}
	}
}
