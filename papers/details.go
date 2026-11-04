package papers

import (
	"context"
	"regexp"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/internal/openalex"
)

var hashPattern = regexp.MustCompile("^[a-fA-F0-9]{32}$")

func isHash(input string) bool {
	return hashPattern.MatchString(input) || strings.Contains(input, "/md5/")
}

// Details returns a selected work and a bounded page of its known copies.
func (c *Client) Details(ctx context.Context, id string, options DetailsOptions) (*Work, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	p, err := provider(options.Provider)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, invalid("id is required")
	}
	if options.LocationOffset < 0 || options.LocationLimit < 0 || options.LocationLimit > 100 {
		return nil, invalid("location_offset must be nonnegative and location_limit between zero and 100")
	}
	if isHash(id) {
		if p == "openalex" {
			return nil, invalid("Anna file hashes require provider anna")
		}
		if c.config.Archive == nil {
			return nil, missingArchive()
		}
		d, err := c.config.Archive.Details(ctx, id, 0)
		if err != nil {
			return nil, c.wrap(err, "anna")
		}
		w := &Work{Provider: "anna", Hash: d.Hash, Title: d.Title, Authors: d.Authors, Journal: d.Publisher, Year: d.Year, Format: d.Format, PageURL: d.URL, URL: d.URL, Description: d.Description, Stats: d.Stats, FastDownload: d.FastDownload, Access: []string{}, DetailsArgs: map[string]any{"id": id, "provider": "anna"}}
		if len(d.Identifiers["doi"]) > 0 {
			w.DOI = d.Identifiers["doi"][0]
		}
		if w.Hash != "" {
			if w.FastDownload || w.DOI == "" {
				w.DownloadArgs = map[string]any{"hash": w.Hash}
			}
			if w.FastDownload {
				w.Access = append(w.Access, "anna_download")
			}
		}
		if w.DownloadArgs == nil && w.DOI != "" {
			w.DownloadArgs = map[string]any{"doi": w.DOI, "provider": "anna"}
		}
		return w, nil
	}
	if p == "anna" {
		if openalex.IsWorkID(id) {
			return nil, invalid("OpenAlex IDs require provider openalex or auto")
		}
		if c.config.Archive == nil {
			return nil, missingArchive()
		}
		r, err := c.config.Archive.LookupDOI(ctx, id, 0)
		if err != nil {
			return nil, c.wrap(err, "anna")
		}
		return mapArchive(r), nil
	}
	if p == "auto" && c.config.OpenAlexAPIKey == "" && !openalex.IsWorkID(id) {
		return c.Details(ctx, id, DetailsOptions{Provider: "anna"})
	}
	w, err := c.lookup(ctx, id)
	if err != nil {
		return nil, err
	}
	limit := options.LocationLimit
	if limit == 0 {
		limit = 10
	}
	start := min(options.LocationOffset, len(w.Locations))
	end := start + min(limit, len(w.Locations)-start)
	w.Locations = w.Locations[start:end]
	if end < w.LocationTotal {
		w.NextDetailsArgs = map[string]any{"id": w.OpenAlexID, "provider": "openalex", "location_offset": end, "location_limit": limit}
	}
	return w, nil
}
