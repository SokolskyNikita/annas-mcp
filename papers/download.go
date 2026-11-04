package papers

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/internal/filetransfer"
	"github.com/SokolskyNikita/annas-mcp/internal/openalex"
)

// Download resolves a selected work and obtains one compatible file. Explicit
// hashes, locations, versions and providers retain their selection semantics.
func (c *Client) Download(ctx context.Context, options DownloadOptions, progress anna.ProgressFunc) (DownloadResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	options, err := normalizeDownload(options)
	if err != nil {
		return DownloadResult{}, err
	}
	if err = ctx.Err(); err != nil {
		return DownloadResult{}, err
	}
	if options.Hash != "" || options.Provider == "anna" || (options.Format != "" && options.Format != "pdf" && options.Artifact == "pdf") {
		return c.downloadArchive(ctx, options, nil, nil, progress)
	}
	if options.Provider == "auto" && c.config.OpenAlexAPIKey == "" && options.OpenAlexID == "" && options.Artifact == "pdf" && options.LocationID == "" && options.Version == "" {
		return c.downloadArchive(ctx, options, nil, nil, progress)
	}
	id := options.DOI
	if options.OpenAlexID != "" {
		id = options.OpenAlexID
	}
	lookupCtx := ctx
	cancel := func() {}
	if options.Provider == "auto" && c.config.Archive != nil {
		lookupCtx, cancel = context.WithTimeout(ctx, 8*time.Second)
	}
	work, lookupErr := c.lookup(lookupCtx, id)
	cancel()
	if ctx.Err() != nil {
		return DownloadResult{}, ctx.Err()
	}
	var attempts []Attempt
	lastErr := lookupErr
	if lookupErr != nil {
		attempts = append(attempts, downloadAttempt("openalex", lookupErr))
	} else {
		candidates, candidateErr := downloadCandidates(work, options)
		if candidateErr != nil {
			return DownloadResult{}, candidateErr
		}
		for _, candidate := range candidates {
			result, fileErr := c.downloadCandidate(ctx, work, options, candidate, progress)
			if fileErr == nil {
				result.FallbackReason = attemptReasons(attempts)
				return result, nil
			}
			if terminalDownloadError(ctx, fileErr) {
				return DownloadResult{}, c.wrap(fileErr, "openalex")
			}
			lastErr = c.wrap(fileErr, "openalex")
			attempts = append(attempts, downloadAttempt("openalex", lastErr))
		}
		if len(candidates) == 0 {
			attempts = append(attempts, Attempt{Provider: "openalex", Reason: "no_content", Message: "OpenAlex found the work but no compatible downloadable file is advertised"})
		}
	}
	if eligibleArchiveFallback(options, work) && c.config.Archive != nil {
		return c.downloadArchive(ctx, options, work, attempts, progress)
	}
	return DownloadResult{}, unavailableDownload(work, attempts, lastErr)
}

// downloadCandidate consumes an already resolved work so callers such as Read
// can fetch its artifact without repeating the metadata lookup.
func (c *Client) downloadCandidate(ctx context.Context, work *Work, options DownloadOptions, candidate downloadCandidate, progress anna.ProgressFunc) (DownloadResult, error) {
	title := options.Title
	if title == "" {
		title = work.Title
	}
	key := strings.Join([]string{work.OpenAlexID, candidate.route, candidate.location, candidate.version, options.Artifact}, "|")
	result, err := c.files.Download(ctx, filetransfer.Request{Key: key, URL: candidate.url, Title: title, Artifact: options.Artifact, Hosted: candidate.hosted, Progress: progress})
	if err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{DownloadResult: anna.DownloadResult{Path: result.Path, Bytes: result.Bytes}, Provider: "openalex", Route: candidate.route, OpenAlexID: work.OpenAlexID, DOI: work.DOI, LocationID: candidate.location, Version: candidate.version, MIMEType: result.MIMEType, SHA256: result.SHA256, Reused: result.Reused, URL: work.PageURL}, nil
}

func normalizeDownload(options DownloadOptions) (DownloadOptions, error) {
	options.DOI = strings.TrimSpace(options.DOI)
	options.OpenAlexID = strings.TrimSpace(options.OpenAlexID)
	options.Hash = strings.TrimSpace(options.Hash)
	options.Provider = strings.ToLower(strings.TrimSpace(options.Provider))
	if options.Provider == "" {
		options.Provider = "auto"
	}
	if options.Provider != "auto" && options.Provider != "anna" && options.Provider != "openalex" {
		return options, downloadError(anna.InvalidArgument, "invalid_provider", "provider must be auto, anna or openalex")
	}
	count := 0
	for _, value := range []string{options.DOI, options.OpenAlexID, options.Hash} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return options, downloadError(anna.InvalidArgument, "invalid_identifier", "supply exactly one of doi, openalex_id or hash")
	}
	if options.DOI != "" {
		if openalex.IsWorkID(options.DOI) {
			options.OpenAlexID = options.DOI
			options.DOI = ""
		} else if normalized := openalex.NormalizeDOI(options.DOI); normalized != "" {
			options.DOI = normalized
		} else {
			return options, downloadError(anna.InvalidArgument, "invalid_identifier", "doi must be a DOI or DOI URL")
		}
	}
	if options.OpenAlexID != "" {
		if !openalex.IsWorkID(options.OpenAlexID) {
			return options, downloadError(anna.InvalidArgument, "invalid_identifier", "openalex_id must be a W ID or OpenAlex work URL")
		}
		options.OpenAlexID, _ = openalex.CanonicalID(options.OpenAlexID)
	}
	options.Artifact = strings.ToLower(strings.TrimSpace(options.Artifact))
	if options.Artifact == "" {
		options.Artifact = "pdf"
	}
	if options.Artifact != "pdf" && options.Artifact != "tei_xml" {
		return options, downloadError(anna.InvalidArgument, "invalid_artifact", "artifact must be pdf or tei_xml")
	}
	options.Format = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(options.Format), "."))
	options.Version = normalizedVersion(options.Version)
	if options.Version == "invalid" {
		return options, downloadError(anna.InvalidArgument, "invalid_version", "version must be published, accepted or submitted")
	}
	constrained := options.LocationID != "" || options.Version != "" || options.Artifact == "tei_xml"
	if options.Hash != "" && (options.Provider == "openalex" || constrained) {
		return options, downloadError(anna.InvalidArgument, "incompatible_arguments", "an AA hash cannot select an OpenAlex copy, version or artifact")
	}
	if options.Provider == "anna" && (options.OpenAlexID != "" || constrained) {
		return options, downloadError(anna.InvalidArgument, "incompatible_arguments", "Anna's Archive accepts a DOI or AA hash without OpenAlex copy controls")
	}
	if options.Provider == "openalex" && options.Format != "" && options.Format != "pdf" && !(options.Artifact == "tei_xml" && options.Format == "xml") {
		return options, downloadError(anna.InvalidArgument, "invalid_format", "OpenAlex PDF downloads require format pdf; use artifact tei_xml for XML")
	}
	if options.Artifact == "tei_xml" && (options.LocationID != "" || options.Version != "" || (options.Format != "" && options.Format != "xml")) {
		return options, downloadError(anna.InvalidArgument, "incompatible_arguments", "TEI XML uses the hosted structured text without a location, version or non-XML extension")
	}
	if constrained && options.Artifact == "pdf" && options.Format != "" && options.Format != "pdf" {
		return options, downloadError(anna.InvalidArgument, "invalid_format", "a selected OpenAlex location or version requires PDF format")
	}
	if options.OpenAlexID != "" && options.Artifact == "pdf" && options.Format != "" && options.Format != "pdf" {
		return options, downloadError(anna.InvalidArgument, "invalid_format", "a selected OpenAlex work can be downloaded as PDF or TEI XML")
	}
	return options, nil
}

type downloadCandidate struct {
	url, route, location, version string
	hosted                        bool
}

func downloadCandidates(work *Work, options DownloadOptions) ([]downloadCandidate, error) {
	if options.LocationID != "" {
		for _, location := range work.Locations {
			if location.ID != options.LocationID {
				continue
			}
			if options.Version != "" && normalizedVersion(location.Version) != options.Version {
				return nil, downloadError(anna.NotFound, "version_mismatch", "the selected location does not establish the requested version")
			}
			if !eligibleLocation(location) {
				return nil, downloadError(anna.NotFound, "no_content", "the selected location has no direct open PDF")
			}
			return []downloadCandidate{{url: locationURL(location), route: "external_pdf", location: location.ID, version: location.Version}}, nil
		}
		return nil, downloadError(anna.InvalidArgument, "unknown_location", "location_id does not belong to the selected work")
	}
	if options.Artifact == "tei_xml" {
		if work.ContentXMLURL != "" {
			return []downloadCandidate{{url: work.ContentXMLURL, route: "hosted_tei_xml", hosted: true}}, nil
		}
		return nil, nil
	}
	var candidates []downloadCandidate
	alternateDOI := options.DOI != "" && openalex.NormalizeDOI(work.DOI) != options.DOI
	if work.ContentPDFURL != "" && options.Version == "" && !alternateDOI {
		candidates = append(candidates, downloadCandidate{url: work.ContentPDFURL, route: "hosted_pdf", hosted: true})
	}
	locations := append([]Location(nil), work.Locations...)
	sort.SliceStable(locations, func(i, j int) bool {
		if work.WorkType == "dissertation" && (locations[i].SourceType == "repository") != (locations[j].SourceType == "repository") {
			return locations[i].SourceType == "repository"
		}
		rankI, rankJ := versionRank(locations[i].Version), versionRank(locations[j].Version)
		if rankI != rankJ {
			return rankI < rankJ
		}
		if locations[i].Source != locations[j].Source {
			return locations[i].Source < locations[j].Source
		}
		return locations[i].ID < locations[j].ID
	})
	seen := map[string]bool{}
	externalCount := 0
	for _, location := range locations {
		if externalCount == 2 {
			break
		}
		if !eligibleLocation(location) || seen[locationURL(location)] {
			continue
		}
		if options.Version != "" && normalizedVersion(location.Version) != options.Version {
			continue
		}
		if alternateDOI && openalex.NormalizeDOI(location.DOI) != options.DOI {
			continue
		}
		candidates = append(candidates, downloadCandidate{url: locationURL(location), route: "external_pdf", location: location.ID, version: location.Version})
		seen[locationURL(location)] = true
		externalCount++
	}
	return candidates, nil
}
func locationURL(location Location) string {
	if location.DownloadURL != "" {
		return location.DownloadURL
	}
	return location.PDFURL
}
func eligibleLocation(location Location) bool {
	parsed, err := url.Parse(locationURL(location))
	return location.IsOA && err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil
}
func normalizedVersion(version string) string {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "", "any":
		return ""
	case "published", "publishedversion":
		return "publishedVersion"
	case "accepted", "acceptedversion":
		return "acceptedVersion"
	case "submitted", "submittedversion":
		return "submittedVersion"
	default:
		return "invalid"
	}
}
func versionRank(version string) int {
	switch normalizedVersion(version) {
	case "publishedVersion":
		return 0
	case "acceptedVersion":
		return 1
	case "submittedVersion":
		return 2
	default:
		return 3
	}
}
func eligibleArchiveFallback(options DownloadOptions, work *Work) bool {
	if options.Provider != "auto" || options.LocationID != "" || options.Version != "" || options.Artifact != "pdf" {
		return false
	}
	return options.DOI != "" || (work != nil && openalex.NormalizeDOI(work.DOI) != "")
}
func (c *Client) downloadArchive(ctx context.Context, options DownloadOptions, work *Work, attempts []Attempt, progress anna.ProgressFunc) (DownloadResult, error) {
	if c.config.Archive == nil {
		return DownloadResult{}, unavailableDownload(work, append(attempts, Attempt{Provider: "anna", Reason: "missing_key", Message: "set ANNAS_SECRET_KEY or ANNAS_ACCOUNT_COOKIE to use Anna's Archive"}), missingArchive())
	}
	doi := options.DOI
	if doi == "" && work != nil {
		doi = openalex.NormalizeDOI(work.DOI)
	}
	archiveOptions := anna.ArticleDownloadOptions{DOI: doi, Hash: options.Hash, Title: options.Title, Format: options.Format}
	if work != nil {
		archiveOptions.ExpectedTitle = work.Title
	}
	result, err := c.config.Archive.DownloadArticle(ctx, archiveOptions, 0, progress)
	if err != nil {
		attempts = append(attempts, downloadAttempt("anna", c.wrap(err, "anna")))
		return DownloadResult{}, unavailableDownload(work, attempts, err)
	}
	output := DownloadResult{DownloadResult: result, Provider: "anna", Route: "archive", DOI: doi, Hash: options.Hash, URL: "", FallbackReason: attemptReasons(attempts)}
	output.Reused = result.Reused
	if result.MD5 != "" {
		output.Hash = result.MD5
	}
	if work != nil {
		output.OpenAlexID = work.OpenAlexID
		output.URL = work.PageURL
	}
	return output, nil
}
func downloadError(code, reason, message string) *Error {
	return &Error{Code: code, Reason: reason, Message: message}
}
func downloadAttempt(provider string, err error) Attempt {
	var detail *Error
	if errors.As(err, &detail) {
		return Attempt{Provider: provider, Reason: detail.Reason, Message: detail.Message}
	}
	return Attempt{Provider: provider, Reason: strings.ToLower(anna.CodeOf(err)), Message: err.Error()}
}
func attemptReasons(attempts []Attempt) string {
	values := make([]string, 0, len(attempts))
	for _, attempt := range attempts {
		values = append(values, attempt.Provider+": "+attempt.Reason)
	}
	return strings.Join(values, "; ")
}
func unavailableDownload(work *Work, attempts []Attempt, cause error) *Error {
	result := &Error{Code: anna.NotFound, Reason: "no_content", Message: "no configured source supplied the selected file", Attempts: attempts, Err: cause}
	if len(attempts) > 0 {
		last := attempts[len(attempts)-1]
		result.Provider = last.Provider
		result.Reason = last.Reason
		result.Message = fmt.Sprintf("no configured source supplied the selected file: %s", last.Message)
	}
	var detail *Error
	if errors.As(cause, &detail) {
		result.Code = detail.Code
		result.Retryable = detail.Retryable
		result.RetryAfter = detail.RetryAfter
	} else if cause != nil {
		result.Code = anna.CodeOf(cause)
	}
	if work != nil {
		result.WorkID = work.OpenAlexID
		result.LandingPage = work.PageURL
		result.NextActions = []map[string]any{{"tool": "article_details", "arguments": map[string]any{"id": work.OpenAlexID}}}
		if work.PageURL != "" {
			result.NextActions = append(result.NextActions, map[string]any{"url": work.PageURL, "action": "open_landing_page"})
		}
	}
	return result
}
func terminalDownloadError(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pathError *os.PathError
	var transferError *filetransfer.Error
	if errors.As(err, &transferError) && (transferError.Code == "LOCAL_IO" || transferError.Code == "CONFIG") {
		return true
	}
	return errors.As(err, &pathError)
}
