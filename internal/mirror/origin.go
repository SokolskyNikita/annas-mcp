package mirror

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var officialCandidateHosts = map[string]struct{}{
	"annas-archive.gl": {},
	"annas-archive.pk": {},
	"annas-archive.gd": {},
}

// ParseBaseURL validates and canonicalizes a configured archive base URL. A
// bare hostname is interpreted as HTTPS for backwards-compatible environment
// configuration. Explicit HTTP URLs, credentials, paths, queries, fragments,
// and malformed hosts are rejected.
func ParseBaseURL(raw string) (string, error) {
	return parseBaseURL(raw, true)
}

// ParseCandidateURL validates a URL discovered in SLUM or returned by an
// automatic resolver. Only Anna's currently documented official mirror
// origins are trusted. Explicitly configured fallback URLs are validated by
// ParseBaseURL instead.
func ParseCandidateURL(raw string) (string, error) {
	value, err := parseBaseURL(raw, false)
	if err != nil {
		return "", err
	}
	if _, ok := officialCandidateHosts[value]; !ok {
		return "", fmt.Errorf("mirror host %q is not an official Anna's Archive origin", value)
	}
	return value, nil
}

func parseBaseURL(raw string, allowPort bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("base URL must use HTTPS")
	}
	if parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("base URL must be an HTTPS origin without credentials")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("base URL must not include a path, query, or fragment")
	}
	port := parsed.Port()
	if !allowPort && port != "" {
		return "", errors.New("discovered mirror must not include a port")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "\\/\x00\r\n") {
		return "", errors.New("base URL has an invalid hostname")
	}
	if port != "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		host += ":" + port
	}
	return host, nil
}

func validHTTPSURL(raw string) bool {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	return true
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
