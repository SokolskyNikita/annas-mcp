package anna

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
)

func buildSearchURL(base, query string, options SearchOptions) string {
	if options.Page < 1 {
		options.Page = 1
	}
	values := url.Values{}
	values.Set("q", query)
	if options.Index != "" {
		values.Set("index", options.Index)
	}
	if options.Content != "" {
		values.Set("content", options.Content)
	}
	if options.Language != "" {
		values.Set("lang", options.Language)
	}
	if options.Page > 1 {
		values.Set("page", strconv.Itoa(options.Page))
	}
	return buildPathURL(base, "/search") + "?" + values.Encode()
}

func buildPathURL(base, path string) string {
	base = strings.TrimSpace(base)
	if !strings.Contains(base, "://") {
		base = "https://" + strings.TrimSuffix(base, "/")
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base + path
	}
	ref, err := url.Parse(path)
	if err != nil {
		return base + path
	}
	// Treat the supplied value as an endpoint relative to the configured
	// mirror path while retaining a query string such as /scidb?doi=....
	u.Path = strings.TrimRight(u.Path, "/") + "/" + strings.TrimLeft(ref.Path, "/")
	u.RawPath = ""
	u.RawQuery = ref.RawQuery
	u.Fragment = ref.Fragment
	return u.String()
}

func resolvePaperDownloadURL(base, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return "", apperr.New(apperr.InvalidArgument, "download URL is empty")
	}
	base = strings.TrimSpace(base)
	if !strings.Contains(base, "://") {
		base = "https://" + strings.TrimSuffix(base, "/")
	}
	baseURL, err := url.Parse(base)
	if err != nil || baseURL.Scheme != "https" || baseURL.Host == "" || baseURL.User != nil {
		return "", apperr.New(apperr.InvalidArgument, "download base URL must use HTTPS without credentials")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", apperr.Wrap(apperr.InvalidArgument, "invalid download URL", err)
	}
	resolved := u
	if !u.IsAbs() {
		resolved = baseURL.ResolveReference(u)
		if !sameOrigin(baseURL, resolved) {
			return "", apperr.New(apperr.InvalidArgument, "relative download URL must stay on the configured mirror")
		}
	}
	if resolved.Scheme != "https" || resolved.Host == "" || resolved.User != nil {
		return "", apperr.New(apperr.InvalidArgument, "download URL must use HTTPS without credentials")
	}
	return resolved.String(), nil
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}

func absoluteURL(pageURL, href string) string {
	page, err := url.Parse(pageURL)
	if err != nil {
		return href
	}
	ref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return page.ResolveReference(ref).String()
}

type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }

func (e *redactedError) Unwrap() error { return e.cause }

func redactErr(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	message := err.Error()
	for _, needle := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
		if needle != "" {
			message = strings.ReplaceAll(message, needle, "REDACTED")
		}
	}
	if message == err.Error() {
		return err
	}
	// Keep the original chain so errors.Is/errors.As still identify context,
	// net.Error, and apperr causes while exposing only sanitized text.
	return &redactedError{message: message, cause: err}
}
