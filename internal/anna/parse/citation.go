package parse

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Citation is DOI.org's description of a work, from its CSL JSON.
type Citation struct {
	DOI     string   `json:"doi"`
	Title   string   `json:"title"`
	Authors []string `json:"authors"`
	Year    string   `json:"year"`
}

// ParseCitation reads DOI.org's CSL JSON. A work without a title is an
// error: there is nothing to check an archive record against.
func ParseCitation(body io.Reader) (Citation, error) {
	var payload map[string]json.RawMessage
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&payload); err != nil {
		return Citation{}, err
	}
	metadata := Citation{
		DOI:     jsonString(payload["DOI"]),
		Title:   jsonStringOrArray(payload["title"]),
		Authors: cslAuthors(payload["author"]),
		Year:    cslYear(payload),
	}
	if metadata.DOI == "" {
		metadata.DOI = jsonString(payload["doi"])
	}
	if metadata.Title == "" {
		return metadata, fmt.Errorf("doi.org metadata had no title")
	}
	return metadata, nil
}

func jsonString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func jsonStringOrArray(raw json.RawMessage) string {
	if value := jsonString(raw); value != "" {
		return value
	}
	var values []string
	if json.Unmarshal(raw, &values) == nil && len(values) > 0 {
		return strings.TrimSpace(values[0])
	}
	return ""
}

func cslAuthors(raw json.RawMessage) []string {
	var authors []struct {
		Given   string `json:"given"`
		Family  string `json:"family"`
		Literal string `json:"literal"`
	}
	if json.Unmarshal(raw, &authors) != nil {
		return nil
	}
	names := make([]string, 0, len(authors))
	for _, author := range authors {
		name := strings.TrimSpace(author.Literal)
		if name == "" {
			name = strings.TrimSpace(strings.TrimSpace(author.Given) + " " + strings.TrimSpace(author.Family))
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cslYear(payload map[string]json.RawMessage) string {
	for _, key := range []string{"issued", "published-print", "published-online", "published"} {
		var date struct {
			DateParts [][]int `json:"date-parts"`
		}
		if json.Unmarshal(payload[key], &date) == nil && len(date.DateParts) > 0 && len(date.DateParts[0]) > 0 && date.DateParts[0][0] > 0 {
			return fmt.Sprint(date.DateParts[0][0])
		}
	}
	return ""
}
