package parse

import (
	"net/url"
	"regexp"
	"strings"
)

var validDOI = regexp.MustCompile(`(?i)^10\.\d{4,9}/\S+$`)

// NormalizeDOI removes common DOI URL/label prefixes and enclosing wrappers.
// Suffix punctuation is significant and must not be guessed away as prose.
func NormalizeDOI(doi string) string {
	doi = unwrapDOI(doi)
	if strings.HasPrefix(strings.ToLower(doi), "doi:") {
		doi = unwrapDOI(doi[len("doi:"):])
	}
	lower := strings.ToLower(doi)
	for _, prefix := range []string{"https://doi.org/", "http://doi.org/", "https://dx.doi.org/", "http://dx.doi.org/"} {
		if strings.HasPrefix(lower, prefix) {
			if parsed, err := url.Parse(doi); err == nil && parsed.Host != "" {
				// URL.Path is decoded by net/url, so an escaped slash in a
				// DOI is treated as the DOI's normal slash separator. Query
				// parameters and fragments are tracking metadata, not DOI text.
				doi = strings.TrimPrefix(parsed.Path, "/")
			} else {
				doi = doi[len(prefix):]
			}
			break
		}
	}
	return strings.TrimSpace(doi)
}

func unwrapDOI(doi string) string {
	doi = strings.TrimSpace(doi)
	for len(doi) >= 2 {
		first, last := doi[0], doi[len(doi)-1]
		if !(first == '<' && last == '>' || first == '(' && last == ')' ||
			first == '[' && last == ']' || first == '{' && last == '}' ||
			first == '"' && last == '"' || first == '\'' && last == '\'') {
			break
		}
		doi = strings.TrimSpace(doi[1 : len(doi)-1])
	}
	return doi
}

// IsDOIQuery reports whether a query is explicitly trying to address a DOI.
// It intentionally returns true for malformed DOI-shaped input so callers can
// report an invalid DOI instead of silently treating it as keyword search.
func IsDOIQuery(query string) bool {
	query = strings.ToLower(unwrapDOI(query))
	return strings.HasPrefix(query, "10.") ||
		strings.HasPrefix(query, "doi:") ||
		strings.HasPrefix(query, "https://doi.org/") ||
		strings.HasPrefix(query, "http://doi.org/") ||
		strings.HasPrefix(query, "https://dx.doi.org/") ||
		strings.HasPrefix(query, "http://dx.doi.org/")
}

// ParseDOI normalizes and validates a DOI query.
func ParseDOI(query string) (string, bool) {
	doi := NormalizeDOI(query)
	return doi, validDOI.MatchString(doi)
}

// ValidDOI reports whether doi has a DOI's shape: 10.<registrant>/<suffix>.
func ValidDOI(doi string) bool {
	return validDOI.MatchString(doi)
}

// ArxivID is the identifier in an arXiv DOI (10.48550/arXiv.<id>), in
// lower case and without its version, or "" for any other DOI.
func ArxivID(doi string) string {
	const prefix = "10.48550/arxiv."
	doi = NormalizeDOI(doi)
	if !strings.HasPrefix(strings.ToLower(doi), prefix) {
		return ""
	}
	return strings.ToLower(stripArxivVersion(doi[len(prefix):]))
}

func stripArxivVersion(identifier string) string {
	if i := strings.LastIndex(strings.ToLower(identifier), "v"); i > 0 && i+1 < len(identifier) {
		version := identifier[i+1:]
		if strings.Trim(version, "0123456789") == "" {
			return identifier[:i]
		}
	}
	return identifier
}

// NormalizeArxivID reads an arXiv identifier written as an arXiv DOI,
// "arxiv:<id>" or the bare id, in lower case and without its version.
func NormalizeArxivID(value string) string {
	value = strings.TrimSpace(value)
	if extracted := ArxivID(value); extracted != "" {
		return extracted
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{"arxiv:", "arxiv."} {
		if strings.HasPrefix(lower, prefix) {
			return strings.ToLower(stripArxivVersion(value[len(prefix):]))
		}
	}
	return strings.ToLower(stripArxivVersion(value))
}
