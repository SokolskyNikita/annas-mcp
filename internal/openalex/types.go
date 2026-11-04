// Package openalex implements the documented OpenAlex Works API.
package openalex

import (
	"sort"
	"strings"
)

type Entity struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	ORCID       string `json:"orcid,omitempty"`
}
type Source struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name"`
	Type             string `json:"type"`
	HostOrganization string `json:"host_organization,omitempty"`
}
type Authorship struct {
	Author         Entity   `json:"author"`
	AuthorPosition string   `json:"author_position"`
	Institutions   []Entity `json:"institutions,omitempty"`
	RawAuthorName  string   `json:"raw_author_name,omitempty"`
}
type Location struct {
	ID             string  `json:"id"`
	DOI            string  `json:"doi,omitempty"`
	IsOA           bool    `json:"is_oa"`
	LandingPageURL string  `json:"landing_page_url"`
	PDFURL         string  `json:"pdf_url"`
	Version        string  `json:"version"`
	License        string  `json:"license"`
	Source         *Source `json:"source"`
}
type ContentFlags struct {
	PDF       bool `json:"pdf"`
	GrobidXML bool `json:"grobid_xml"`
}
type ContentURLs struct {
	PDF       string `json:"pdf"`
	GrobidXML string `json:"grobid_xml"`
}
type Work struct {
	ID                    string            `json:"id"`
	DOI                   string            `json:"doi"`
	DisplayName           string            `json:"display_name"`
	Type                  string            `json:"type"`
	PublicationYear       int               `json:"publication_year"`
	PublicationDate       string            `json:"publication_date"`
	Language              string            `json:"language"`
	CitedByCount          int               `json:"cited_by_count"`
	Authorships           []Authorship      `json:"authorships"`
	Locations             []Location        `json:"locations"`
	PrimaryLocation       *Location         `json:"primary_location"`
	BestOALocation        *Location         `json:"best_oa_location"`
	HasContent            ContentFlags      `json:"has_content"`
	ContentURLs           ContentURLs       `json:"content_urls"`
	AbstractInvertedIndex map[string][]int  `json:"abstract_inverted_index"`
	ReferencedWorks       []string          `json:"referenced_works"`
	IDs                   map[string]string `json:"ids"`
}

func (w *Work) ShortID() string { return strings.TrimPrefix(w.ID, "https://openalex.org/") }
func (w *Work) HasDOI(doi string) bool {
	expected := NormalizeDOI(doi)
	if expected == "" {
		return false
	}
	if NormalizeDOI(w.DOI) == expected {
		return true
	}
	for _, location := range w.Locations {
		if NormalizeDOI(location.DOI) == expected {
			return true
		}
	}
	return false
}
func (w *Work) LandingURL() string {
	if w.PrimaryLocation != nil && w.PrimaryLocation.LandingPageURL != "" {
		return w.PrimaryLocation.LandingPageURL
	}
	for _, location := range w.Locations {
		if location.LandingPageURL != "" {
			return location.LandingPageURL
		}
	}
	return w.ID
}
func (w *Work) Abstract() string {
	// Keep malformed or unexpectedly large upstream indexes from allocating an unbounded slice.
	positions := map[int]string{}
	for word, indexes := range w.AbstractInvertedIndex {
		for _, index := range indexes {
			if index >= 0 && index < 100000 {
				positions[index] = word
			}
		}
	}
	ordered := make([]int, 0, len(positions))
	for index := range positions {
		ordered = append(ordered, index)
	}
	sort.Ints(ordered)
	words := make([]string, 0, len(ordered))
	for _, index := range ordered {
		words = append(words, positions[index])
	}
	return strings.Join(words, " ")
}

type SearchOptions struct {
	Query            string
	Title            string
	Author           string
	AuthorID         string
	InstitutionID    string
	RepositoryID     string
	Year             string
	Language         string
	WorkTypes        []string
	Sort             string
	Page             int
	PerPage          int
	Cursor           string
	DownloadableOnly bool
	Relation         string
	WorkID           string
}
type SearchResult struct {
	Results []Work `json:"results"`
	Meta    struct {
		Count      int    `json:"count"`
		Page       int    `json:"page"`
		PerPage    int    `json:"per_page"`
		NextCursor string `json:"next_cursor"`
	} `json:"meta"`
}

// Status keeps the provider's documented usage fields without assuming their units.
type Status map[string]any
