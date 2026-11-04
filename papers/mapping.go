package papers

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/openalex"
)

func mapWork(raw *openalex.Work, details bool) *Work {
	w := &Work{Provider: "openalex", OpenAlexID: raw.ShortID(), DOI: openalex.NormalizeDOI(raw.DOI), Title: raw.DisplayName, WorkType: raw.Type, PublicationDate: raw.PublicationDate, Language: raw.Language, CitationCount: raw.CitedByCount, PageURL: publicURL(raw.LandingURL()), URL: publicURL(raw.ID), Access: []string{}}
	if raw.PublicationYear > 0 {
		w.Year = strconv.Itoa(raw.PublicationYear)
	}
	names := make([]string, 0, len(raw.Authorships))
	for _, a := range raw.Authorships {
		if details || len(names) < 5 {
			names = append(names, a.Author.DisplayName)
		}
		if details {
			w.Authorships = append(w.Authorships, Author{ID: a.Author.ID, Name: a.Author.DisplayName, ORCID: a.Author.ORCID})
		}
	}
	w.Authors = strings.Join(names, ", ")
	if !details && len(raw.Authorships) > 5 {
		w.Authors += " et al."
	}
	if raw.PrimaryLocation != nil && raw.PrimaryLocation.Source != nil {
		w.Journal = raw.PrimaryLocation.Source.DisplayName
	}
	if raw.HasContent.PDF && raw.ContentURLs.PDF != "" {
		w.ContentPDFURL = raw.ContentURLs.PDF
		w.Access = append(w.Access, "hosted_pdf")
	}
	if raw.HasContent.GrobidXML && raw.ContentURLs.GrobidXML != "" {
		w.ContentXMLURL = raw.ContentURLs.GrobidXML
		w.Access = append(w.Access, "structured_text")
	}
	external := false
	routes := map[string]bool{}
	for _, r := range raw.Locations {
		l := Location{DownloadURL: r.PDFURL, ID: r.ID, DOI: openalex.NormalizeDOI(r.DOI), LandingPageURL: publicURL(r.LandingPageURL), PDFURL: publicURL(r.PDFURL), IsOA: r.IsOA, Version: r.Version, License: r.License}
		if w.PageURL == "" {
			w.PageURL = l.LandingPageURL
		}
		if r.Source != nil {
			l.Source = r.Source.DisplayName
			l.SourceID = r.Source.ID
			l.SourceType = r.Source.Type
		}
		if l.IsOA && publicArtifactURL(l.DownloadURL) {
			external = true
			kind := "external_pdf"
			if l.SourceType == "repository" {
				kind = "repository_pdf"
			} else if l.SourceType == "journal" || l.SourceType == "conference" {
				kind = "publisher_pdf"
			}
			routes[kind] = true
			if l.ID != "" {
				l.DownloadArgs = map[string]any{"openalex_id": w.OpenAlexID, "provider": "openalex", "location_id": l.ID}
			}
		}
		if details {
			w.Locations = append(w.Locations, l)
		}
	}
	if external {
		for _, kind := range []string{"repository_pdf", "publisher_pdf", "external_pdf"} {
			if routes[kind] {
				w.Access = append(w.Access, kind)
			}
		}
	}
	if w.PageURL == "" {
		w.PageURL = w.URL
	}
	if w.PageURL != "" {
		w.Access = append(w.Access, "landing_page")
	}
	if details {
		w.Abstract = raw.Abstract()
		w.LocationTotal = len(w.Locations)
	}
	w.DetailsArgs = map[string]any{"id": w.OpenAlexID, "provider": "openalex"}
	if w.ContentPDFURL != "" || external {
		w.DownloadArgs = map[string]any{"openalex_id": w.OpenAlexID}
	}
	return w
}
func mapArchive(raw *anna.Paper) *Work {
	w := &Work{Provider: "anna", DOI: raw.DOI, Title: raw.Title, Authors: raw.Authors, Journal: raw.Journal, Year: raw.Year, Format: raw.Format, Size: raw.Size, Sources: raw.Sources, FastDownload: raw.FastDownload, PossibleIssues: raw.PossibleIssues, Hash: raw.Hash, Description: raw.Description, PageURL: raw.PageURL, URL: raw.PageURL, Stats: raw.Stats, Access: []string{}}
	if raw.FastDownload || raw.DownloadURL != "" {
		w.Access = append(w.Access, "anna_download")
	}
	if raw.Hash != "" {
		w.DetailsArgs = map[string]any{"id": raw.Hash, "provider": "anna"}
		if raw.FastDownload || raw.DOI == "" {
			w.DownloadArgs = map[string]any{"hash": raw.Hash}
		}
	}
	if w.DownloadArgs == nil && raw.DOI != "" {
		w.DownloadArgs = map[string]any{"doi": raw.DOI, "provider": "anna"}
	}
	if w.PageURL != "" {
		w.Access = append(w.Access, "landing_page")
	}
	return w
}

// publicURL retains useful public addresses without exposing token query strings.
func publicURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return ""
	}
	for key := range u.Query() {
		name := strings.ToLower(key)
		if strings.Contains(name, "key") || strings.Contains(name, "token") || strings.Contains(name, "signature") || strings.Contains(name, "credential") || name == "sig" || name == "auth" || name == "authorization" {
			return ""
		}
	}
	return u.String()
}

func publicArtifactURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
