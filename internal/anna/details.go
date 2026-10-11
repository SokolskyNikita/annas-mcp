package anna

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/SokolskyNikita/annas-mcp/internal/anna/parse"
	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// Details is the full record for a file or catalog entry, trimmed to the
// fields that help choose an edition, cite it, or judge a file's quality.
type Details struct {
	ID                    string              `json:"id"`
	Hash                  string              `json:"hash,omitempty"`
	URL                   string              `json:"url"`
	Title                 string              `json:"title"`
	Authors               string              `json:"authors,omitempty"`
	Publisher             string              `json:"publisher,omitempty"`
	Edition               string              `json:"edition,omitempty"`
	Year                  string              `json:"year,omitempty"`
	Languages             []string            `json:"languages,omitempty"`
	Content               string              `json:"content,omitempty"`
	Format                string              `json:"format,omitempty"`
	SizeBytes             int64               `json:"size_bytes,omitempty"`
	Description           string              `json:"description,omitempty"`
	AlternativeTitles     []string            `json:"alternative_titles,omitempty"`
	AlternativeAuthors    []string            `json:"alternative_authors,omitempty"`
	AlternativePublishers []string            `json:"alternative_publishers,omitempty"`
	AlternativeEditions   []string            `json:"alternative_editions,omitempty"`
	Identifiers           map[string][]string `json:"identifiers,omitempty"`
	Classifications       map[string][]string `json:"classifications,omitempty"`
	Subjects              []string            `json:"subjects,omitempty"`
	OriginalFilename      string              `json:"original_filename,omitempty"`
	AddedDate             string              `json:"added_date,omitempty"`
	Sources               []string            `json:"sources,omitempty"`
	FastDownload          bool                `json:"fast_download"`
	Problems              []Problem           `json:"problems,omitempty"`
	Stats                 *RecordStats        `json:"stats,omitempty"`
	RecentlyDownloaded    bool                `json:"recently_downloaded,omitempty"`
}

// Problem is an issue a source library reported for a file. BetterHash
// names a better copy of the same work when the archive knows one.
type Problem struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	BetterHash  string `json:"better_hash,omitempty"`
}

// RecordStats are the archive's community counts for a file.
type RecordStats struct {
	Downloads    int `json:"downloads"`
	GreatQuality int `json:"great_quality"`
	Reports      int `json:"reports"`
	Lists        int `json:"lists"`
	Comments     int `json:"comments"`
}

const (
	maxDetailDescription = 4000
	maxDetailAlternates  = 10
	maxDetailIdentifiers = 20
	maxDetailSubjects    = 15
)

// Identifiers worth showing, in display order; the record also holds
// internal paths, hashes and collection ids.
var detailIdentifiers = []struct{ key, label string }{
	{"isbn13", "ISBN-13"}, {"isbn10", "ISBN-10"}, {"doi", "DOI"}, {"issn", "ISSN"}, {"iso", "ISO"},
	{"oclc", "OCLC"}, {"lccn", "LCCN"}, {"ol", "Open Library"}, {"goodreads", "Goodreads"},
	{"gbooks", "Google Books"}, {"librarything", "LibraryThing"}, {"ocaid", "Internet Archive"},
	{"asin", "ASIN"}, {"pmid", "PubMed"}, {"arxiv", "arXiv"},
}

var detailClassifications = []string{"ddc", "lcc", "udc"}

var detailSubjectKeys = []string{"openlib_subject", "edsebk_subject", "rgb_subject", "zlib_category_name"}

// English labels from Anna's md5_problem_type_mapping.
var problemLabels = map[string]string{
	"lgrsnf_visible":         "Not visible in Libgen.rs Non-Fiction",
	"lgrsfic_visible":        "Not visible in Libgen.rs Fiction",
	"lgli_visible":           "Not visible in Libgen.li",
	"lgli_broken":            "Marked broken in Libgen.li",
	"zlib_missing":           "Missing from Z-Library",
	"zlib_spam":              "Marked as spam in Z-Library",
	"zlib_bad_file":          "Marked as a bad file in Z-Library",
	"duxiu_pdg_broken_files": "Not all pages could be converted to PDF",
	"upload_exiftool_failed": "Running exiftool failed on this file",
}

// Details returns the record behind a search hit: a file hash, a catalog id
// such as isbndb:9780521879286, or a record URL.
func (c *Client) Details(ctx context.Context, id string, timeout time.Duration) (*Details, error) {
	ctx, cancel := operationContext(ctx, timeout)
	defer cancel()
	prefix, value, err := ParseRecordID(id)
	if err != nil {
		return nil, err
	}
	if err := c.requireArchiveAccess(ctx); err != nil {
		return nil, err
	}
	var details *Details
	err = c.onMirror(ctx, func(base string) error {
		var err error
		details, err = c.detailsOn(ctx, base, prefix, value)
		return err
	})
	if err != nil {
		if apperr.CodeOf(err) == apperr.NotFound {
			return nil, apperr.New(apperr.NotFound, fmt.Sprintf("Anna's Archive has no record %s:%s", prefix, value))
		}
		return nil, err
	}
	return details, nil
}

func (c *Client) detailsOn(ctx context.Context, base, prefix, value string) (*Details, error) {
	var summary *parse.MD5Summary
	var wait sync.WaitGroup
	if prefix == "md5" {
		wait.Add(1)
		go func() {
			defer wait.Done()
			// Statistics are secondary; the record is still useful without them.
			summary, _ = c.fetchSummary(ctx, base, value)
		}()
	}
	var record parse.Record
	recordURL := buildPathURL(base, "/db/aarecord_elasticsearch/"+prefix+":"+escapeRecordValue(value)+".json")
	err := c.fetchJSON(ctx, recordURL, &record, "record details")
	wait.Wait()
	if err != nil {
		return nil, err
	}
	details := detailsFromRecord(&record, base, prefix, value)
	if summary != nil {
		details.Stats = &RecordStats{
			Downloads: summary.DownloadsTotal, GreatQuality: summary.GreatQualityCount,
			Reports: summary.ReportsCount, Lists: summary.ListsCount, Comments: summary.CommentsCount,
		}
		details.RecentlyDownloaded = summary.DownloadStillActive == 1
	}
	return details, nil
}

// recordPrefixPattern is a record id's prefix, such as md5, isbndb or ol.
var recordPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ParseRecordID accepts a 32-character hash, md5:<hash>, a catalog id such
// as ol:OL123M, or a record URL or path copied from a search result.
func ParseRecordID(input string) (prefix, value string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", apperr.New(apperr.InvalidArgument, "id is empty")
	}
	if strings.HasPrefix(input, "/") || strings.Contains(input, "://") {
		parsed, parseErr := url.Parse(input)
		if parseErr == nil {
			if p, v, ok := parse.RecordPath(parsed.Path); ok {
				if unescaped, unescapeErr := url.PathUnescape(v); unescapeErr == nil {
					v = unescaped
				}
				input = p + ":" + v
			}
		}
	}
	if hash, hashErr := normalizeHash(input); hashErr == nil {
		return "md5", hash, nil
	}
	prefix, value, found := strings.Cut(input, ":")
	prefix = strings.ToLower(prefix)
	if !found || !recordPrefixPattern.MatchString(prefix) || value == "" || len(value) > 300 || strings.ContainsAny(value, " \t\r\n?#") {
		return "", "", apperr.New(apperr.InvalidArgument, "id must be a 32-character hash from search or a record id such as isbndb:9780521879286")
	}
	if prefix == "md5" {
		hash, hashErr := normalizeHash(value)
		if hashErr != nil {
			return "", "", hashErr
		}
		return prefix, hash, nil
	}
	return prefix, value, nil
}

// escapeRecordValue escapes each path segment; DOI and HathiTrust ids keep
// their slashes.
func escapeRecordValue(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func (c *Client) fetchSummary(ctx context.Context, base, hash string) (*parse.MD5Summary, error) {
	summaryURL := buildPathURL(base, "/dyn/md5/summary/"+hash) + "?cachebust=" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var summary parse.MD5Summary
	if err := c.fetchJSON(ctx, summaryURL, &summary, "file statistics"); err != nil {
		return nil, err
	}
	return &summary, nil
}

// fetchJSON reads an archive JSON endpoint. An HTML body where JSON was
// expected is a browser check or an error page, not record data.
func (c *Client) fetchJSON(ctx context.Context, rawURL string, target any, what string) error {
	return c.fetchJSONWithHeaders(ctx, rawURL, target, what, nil)
}

// fetchJSONWithHeaders reads an archive JSON endpoint with additional request
// headers. Some JSON endpoints use a non-JSON content type, so the body is
// decoded as JSON regardless of Content-Type.
func (c *Client) fetchJSONWithHeaders(ctx context.Context, rawURL string, target any, what string, header http.Header) error {
	response, err := c.doGetWith(ctx, c.requestClient(0), rawURL, header)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return apperr.New(apperr.NotFound, what+" not found")
	case http.StatusForbidden:
		return &forbiddenError{"Anna's Archive refused " + what + "; it shows them to signed-in members only, so check that your membership is active"}
	default:
		return httpResponseError(response, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if err != nil {
		return fmt.Errorf("could not read %s from %s: %w", what, responseHost(response, rawURL), err)
	}
	if len(body) > 8<<20 {
		return apperr.New(apperr.Upstream, what+" exceeded the size limit")
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '<' {
		c.mirrorFailed(rawURL, mirror.Failure{Reason: "returned a web page instead of " + what, Blocked: true})
		return apperr.New(apperr.UpstreamBlocked, "archive returned a web page instead of "+what+"; check the mirror and account access")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return apperr.Wrap(apperr.Upstream, "invalid "+what+" from "+responseHost(response, rawURL), err)
	}
	return nil
}

func detailsFromRecord(record *parse.Record, base, prefix, value string) *Details {
	file := &record.FileUnifiedData
	id := record.ID
	if id == "" {
		id = prefix + ":" + value
	}
	path := string(record.Additional.Path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + prefix + "/" + escapeRecordValue(value)
	}
	details := &Details{
		ID:                    id,
		URL:                   buildPathURL(base, path),
		Title:                 strings.TrimSpace(string(file.TitleBest)),
		Authors:               parse.CleanAuthors(string(file.AuthorBest)),
		Publisher:             strings.TrimSpace(string(file.PublisherBest)),
		Edition:               strings.TrimSpace(string(file.EditionVariaBest)),
		Year:                  strings.TrimSpace(string(file.YearBest)),
		Languages:             distinct(file.MostLikelyLanguageCodes, 0, ""),
		Content:               string(file.ContentTypeBest),
		Format:                strings.ToLower(string(file.ExtensionBest)),
		Description:           truncateText(strings.TrimSpace(string(file.StrippedDescriptionBest)), maxDetailDescription),
		AlternativeTitles:     distinct(file.TitleAdditional, maxDetailAlternates, file.TitleBest),
		AlternativeAuthors:    distinct(file.AuthorAdditional, maxDetailAlternates, file.AuthorBest),
		AlternativePublishers: distinct(file.PublisherAdditional, maxDetailAlternates, file.PublisherBest),
		AlternativeEditions:   distinct(file.EditionVariaAdditional, maxDetailAlternates, file.EditionVariaBest),
		OriginalFilename:      string(file.OriginalFilenameBest),
		AddedDate:             string(file.AddedDateBest),
		Sources:               distinct(record.SearchOnlyFields.SearchRecordSources, 0, ""),
		FastDownload:          string(file.HasAADownloads) == "1" || string(file.HasAADownloads) == "true",
	}
	if prefix == "md5" {
		details.Hash = value
	}
	if size, err := strconv.ParseInt(string(file.FilesizeBest), 10, 64); err == nil && size > 0 {
		details.SizeBytes = size
	}
	for _, identifier := range detailIdentifiers {
		if values := distinct(file.IdentifiersUnified[identifier.key], maxDetailIdentifiers, ""); len(values) > 0 {
			if details.Identifiers == nil {
				details.Identifiers = map[string][]string{}
			}
			details.Identifiers[identifier.key] = values
		}
	}
	for _, key := range detailClassifications {
		if values := distinct(file.ClassificationsUnified[key], maxDetailIdentifiers, ""); len(values) > 0 {
			if details.Classifications == nil {
				details.Classifications = map[string][]string{}
			}
			details.Classifications[key] = values
		}
	}
	var subjects parse.LooseStrings
	for _, key := range detailSubjectKeys {
		for _, subject := range file.ClassificationsUnified[key] {
			// EBSCOhost subjects carry a scheme prefix such as "bisac/".
			if scheme, rest, found := strings.Cut(subject, "/"); found && key == "edsebk_subject" && !strings.Contains(scheme, " ") {
				subject = rest
			}
			subjects = append(subjects, strings.TrimSpace(subject))
		}
	}
	details.Subjects = distinct(subjects, maxDetailSubjects, "")
	for _, problem := range file.Problems {
		label := problemLabels[string(problem.Type)]
		if label == "" {
			label = strings.ReplaceAll(string(problem.Type), "_", " ")
		}
		if reason := strings.TrimSpace(string(problem.Descr)); reason != "" && !strings.EqualFold(reason, label) {
			label += " (" + reason + ")"
		}
		details.Problems = append(details.Problems, Problem{
			Type: string(problem.Type), Description: label,
			BetterHash: strings.TrimPrefix(string(problem.BetterAarecordID), "md5:"),
		})
	}
	return details
}

// distinct trims and de-duplicates values case-insensitively, skipping
// empty ones and any equal to except. limit 0 keeps every value.
func distinct(values parse.LooseStrings, limit int, except parse.LooseString) []string {
	seen := map[string]bool{strings.ToLower(strings.TrimSpace(string(except))): true}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := strings.ToLower(value)
		if value == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out
}

func truncateText(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := text[:limit]
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if i := strings.LastIndex(cut, " "); i > limit*3/4 {
		cut = cut[:i]
	}
	return cut + "…"
}
