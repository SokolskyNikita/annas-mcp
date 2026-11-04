package openalex

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var yearPattern = regexp.MustCompile(`^[0-9]{4}(?:-[0-9]{4})?$`)
var languagePattern = regexp.MustCompile(`^[a-z]{2,3}$`)
var orcidPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{4}-[0-9]{4}-[0-9Xx]{4}$`)

func searchValues(options SearchOptions) (url.Values, error) {
	invalid := func(message string) (url.Values, error) {
		return nil, &Error{Code: "INVALID_ARGUMENT", Reason: "invalid_filter", Message: message}
	}
	values := url.Values{}
	if query := strings.TrimSpace(options.Query); query != "" {
		values.Set("search", query)
	}
	if title := strings.TrimSpace(options.Title); title != "" {
		values.Set("search.title", title)
	}
	var filters []string
	if author := strings.TrimSpace(options.Author); author != "" {
		if strings.ContainsAny(author, "\r\n\x00") {
			return invalid("author must be a single line")
		}
		escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(author)
		filters = append(filters, `raw_author_name.search:"`+escaped+`"`)
	}
	if options.AuthorID != "" {
		raw := strings.TrimPrefix(strings.TrimSpace(options.AuthorID), "https://orcid.org/")
		if orcidPattern.MatchString(raw) {
			filters = append(filters, "authorships.author.orcid:https://orcid.org/"+strings.ToUpper(raw))
		} else {
			id, err := entityID(raw, "A")
			if err != nil {
				return invalid("author_id must be an OpenAlex author ID or ORCID")
			}
			filters = append(filters, "authorships.author.id:"+id)
		}
	}
	if options.InstitutionID != "" {
		id, err := entityID(options.InstitutionID, "I")
		if err != nil {
			return invalid("institution_id must be an OpenAlex institution ID")
		}
		filters = append(filters, "institutions.id:"+id)
	}
	if options.RepositoryID != "" {
		id, err := entityID(options.RepositoryID, "S")
		if err != nil {
			return invalid("repository_id must be an OpenAlex source ID")
		}
		filters = append(filters, "repository:"+id)
	}
	if options.Year != "" {
		if !yearPattern.MatchString(options.Year) {
			return invalid("year must be a four-digit year or inclusive range, such as 2020-2024")
		}
		years := strings.Split(options.Year, "-")
		if len(years) == 2 && years[0] > years[1] {
			return invalid("year range must start before it ends")
		}
		filters = append(filters, "publication_year:"+options.Year)
	}
	if options.Language != "" {
		var languages []string
		for _, language := range strings.Split(strings.ToLower(options.Language), ",") {
			language = strings.TrimSpace(language)
			if !languagePattern.MatchString(language) {
				return invalid("language must contain comma-separated ISO language codes")
			}
			languages = append(languages, language)
		}
		filters = append(filters, "language:"+strings.Join(languages, "|"))
	}
	types := options.WorkTypes
	if len(types) == 0 {
		types = []string{"article", "review", "preprint", "conference-paper"}
	}
	supported := map[string]bool{"article": true, "review": true, "preprint": true, "conference-paper": true, "dissertation": true, "book": true, "book-chapter": true, "report": true, "dataset": true, "data-paper": true, "software": true, "software-paper": true, "other": true, "editorial": true, "letter": true, "erratum": true, "paratext": true, "reference-entry": true, "peer-review": true, "standard": true, "grant": true, "libguides": true, "supplementary-materials": true}
	var selected []string
	for _, kind := range types {
		if kind == "any" {
			if len(types) != 1 {
				return invalid("work_type any cannot be combined with other types")
			}
			selected = nil
			break
		}
		if !supported[kind] {
			return invalid("unsupported OpenAlex work_type")
		}
		selected = append(selected, kind)
		if kind == "dissertation" {
			values.Set("corpus", "all")
		}
	}
	if len(selected) > 0 {
		filters = append(filters, "type:"+strings.Join(selected, "|"))
	}
	if options.DownloadableOnly {
		filters = append(filters, "has_content.pdf:true")
	}
	if options.Relation != "" {
		id, err := CanonicalID(options.WorkID)
		if err != nil || !IsWorkID(id) {
			return invalid("relation requires a seed OpenAlex W ID")
		}
		relation := map[string]string{"citing": "cites", "references": "cited_by", "related": "related_to"}[options.Relation]
		if relation == "" {
			return invalid("relation must be citing, references or related")
		}
		filters = append(filters, relation+":"+id)
	} else if options.WorkID != "" {
		return invalid("work_id requires a relation")
	}
	if values.Get("search") == "" && values.Get("search.title") == "" && options.Author == "" && options.AuthorID == "" && options.InstitutionID == "" && options.RepositoryID == "" && options.Year == "" && options.Language == "" && len(options.WorkTypes) == 0 && !options.DownloadableOnly && options.Relation == "" {
		return invalid("provide a query, title, author or another discovery filter")
	}
	switch options.Sort {
	case "", "relevance":
	case "newest":
		values.Set("sort", "publication_year:desc")
	case "oldest":
		values.Set("sort", "publication_year:asc")
	case "most_cited":
		values.Set("sort", "cited_by_count:desc")
	default:
		return invalid("OpenAlex sort must be relevance, newest, oldest or most_cited")
	}
	limit := options.PerPage
	if limit == 0 {
		limit = 10
	}
	if limit < 1 || limit > 100 {
		return invalid("OpenAlex limit must be between 1 and 100")
	}
	values.Set("per_page", strconv.Itoa(limit))
	page := options.Page
	if page == 0 {
		page = 1
	}
	if page < 1 {
		return invalid("page must start at 1")
	}
	if options.Cursor != "" {
		if page > 1 {
			return invalid("use a cursor or numbered page, not both")
		}
		if len(options.Cursor) > 4096 {
			return invalid("cursor is too long")
		}
		values.Set("cursor", options.Cursor)
	} else {
		if page > 10000/limit {
			return invalid("use cursor pagination beyond the first 10000 results")
		}
		values.Set("page", strconv.Itoa(page))
	}
	values.Set("filter", strings.Join(filters, ","))
	values.Set("select", "id,doi,display_name,type,publication_year,publication_date,language,cited_by_count,authorships,locations,primary_location,best_oa_location,has_content,content_urls")
	return values, nil
}
