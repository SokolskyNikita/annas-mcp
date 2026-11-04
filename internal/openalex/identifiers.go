package openalex

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var workIDPattern = regexp.MustCompile(`(?i)^W[0-9]+$`)
var doiPattern = regexp.MustCompile(`(?i)^10\.[0-9]{4,9}/\S+$`)

func NormalizeDOI(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "doi:") {
		raw = strings.TrimSpace(raw[4:])
	}
	if parsed, err := url.Parse(raw); err == nil && (strings.EqualFold(parsed.Hostname(), "doi.org") || strings.EqualFold(parsed.Hostname(), "dx.doi.org")) {
		raw = strings.TrimPrefix(parsed.Path, "/")
	}
	if !doiPattern.MatchString(raw) {
		return ""
	}
	return strings.ToLower(raw)
}
func CanonicalID(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if parsed, err := url.Parse(raw); err == nil && strings.EqualFold(parsed.Hostname(), "openalex.org") && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
		raw = strings.Trim(parsed.Path, "/")
	}
	if workIDPattern.MatchString(raw) {
		return strings.ToUpper(raw), nil
	}
	if doi := NormalizeDOI(raw); doi != "" {
		return "https://doi.org/" + doi, nil
	}
	return "", &Error{Code: "INVALID_ARGUMENT", Reason: "invalid_identifier", Message: "pass a DOI, OpenAlex W ID or OpenAlex work URL"}
}
func IsWorkID(raw string) bool {
	id, err := CanonicalID(raw)
	return err == nil && workIDPattern.MatchString(id)
}
func entityID(raw, letter string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "https://openalex.org/")
	if len(raw) < 2 || !strings.EqualFold(raw[:1], letter) {
		return "", fmt.Errorf("expected an OpenAlex %s ID", letter)
	}
	for _, r := range raw[1:] {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("expected an OpenAlex %s ID", letter)
		}
	}
	return strings.ToUpper(raw), nil
}
