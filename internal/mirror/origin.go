package mirror

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

var officialHosts = []string{"annas-archive.gl", "annas-archive.pk", "annas-archive.gd"}

// OfficialHosts returns Anna's documented mirror hosts, primary first.
func OfficialHosts() []string {
	return slices.Clone(officialHosts)
}

// Candidates lists the mirrors automatic selection chooses from, preferred
// first: the configured host, then the official mirrors. Only these hosts
// are ever contacted with the account's credentials. The configured host is
// the user's explicit choice; nothing discovered at run time is added.
func Candidates(configured string) []string {
	hosts := make([]string, 0, len(officialHosts)+1)
	if configured != "" {
		hosts = append(hosts, configured)
	}
	for _, host := range officialHosts {
		if host != configured {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// ParseBaseURL validates and canonicalizes a configured archive base URL. A
// bare hostname is interpreted as HTTPS for backwards-compatible environment
// configuration. Explicit HTTP URLs, credentials, paths, queries, fragments,
// and malformed hosts are rejected.
func ParseBaseURL(raw string) (string, error) {
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
		return "", errors.New("base URL must not include a path, query or fragment")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "\\/\x00\r\n") {
		return "", errors.New("base URL has an invalid hostname")
	}
	if port := parsed.Port(); port != "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		host += ":" + port
	}
	return host, nil
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
