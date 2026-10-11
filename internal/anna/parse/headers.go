package parse

import (
	"bytes"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var extensionPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,16}$`)

// LooksLikeHTML reports whether a download is a web page, such as a browser
// check, by its media type or its first bytes.
func LooksLikeHTML(contentType string, prefix []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && (mediaType == "text/html" || strings.HasSuffix(mediaType, "+html")) {
		return true
	}
	lower := bytes.ToLower(bytes.TrimSpace(prefix))
	return bytes.HasPrefix(lower, []byte("<!doctype html")) || bytes.HasPrefix(lower, []byte("<html"))
}

// DownloadFormat is a downloaded file's extension: from its
// Content-Disposition filename, its media type or, for Anna's file servers,
// the URL path. Without any of them it is "bin".
func DownloadFormat(header http.Header, requestURL *url.URL) string {
	if cd := header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if filename, ok := params["filename"]; ok {
				if ext := strings.TrimPrefix(filepath.Ext(filename), "."); ext != "" {
					return ext
				}
			}
		}
	}
	if ext := extensionFromMediaType(header.Get("Content-Type")); ext != "" {
		return ext
	}
	// Anna's file servers send "attachment" without a filename and a generic
	// octet-stream type, but the signed URL ends in the archived filename.
	if requestURL != nil {
		if ext := strings.TrimPrefix(path.Ext(requestURL.Path), "."); extensionPattern.MatchString(ext) {
			return ext
		}
	}
	return "bin"
}

func extensionFromMediaType(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType == "" || mediaType == "application/octet-stream" || mediaType == "binary/octet-stream" {
		return ""
	}
	extensions, _ := mime.ExtensionsByType(mediaType)
	if len(extensions) == 0 {
		return ""
	}
	return strings.TrimPrefix(extensions[0], ".")
}

// ContentRangeStart reads the first byte of a Content-Range such as
// "bytes 1000-1999/2000".
func ContentRangeStart(value string) (int64, bool) {
	spec, ok := strings.CutPrefix(strings.TrimSpace(value), "bytes ")
	if !ok {
		return 0, false
	}
	first, _, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, false
	}
	start, err := strconv.ParseInt(first, 10, 64)
	return start, err == nil && start >= 0
}
