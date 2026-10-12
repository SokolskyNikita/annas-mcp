package anna

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	languageCodePattern = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)
	searchFormatPattern = regexp.MustCompile(`^[a-z0-9]{1,10}$`)
	sourcePattern       = regexp.MustCompile(`^[a-z0-9_]{1,40}$`)
	yearPattern         = regexp.MustCompile(`^\d{1,4}$`)
)

var contentTypes = map[string]bool{
	"book_fiction": true, "book_nonfiction": true, "book_unknown": true, "book_comic": true,
	"magazine": true, "standards_document": true, "musical_score": true, "audiobook": true,
	"other": true, "journal_article": true,
	// Legacy values that ResolveSearch maps or drops.
	"book_any": true, "journal": true,
}

// Anna's sort values; relevance is its default, sent as no sort at all.
var sortValues = map[string]string{
	"": "", "relevance": "", "newest": "newest", "oldest": "oldest",
	"largest": "largest", "smallest": "smallest",
	"newest_added": "newest_added", "oldest_added": "oldest_added", "random": "random",
}

// NormalizeSearch validates archive search semantics and returns independent slices.
// A zero page becomes page one. defaultContent may select the journals index.
func NormalizeSearch(query string, options SearchOptions, defaultContent string) (SearchOptions, error) {
	if options.Page < 0 {
		return options, NewError(InvalidArgument, "page must be 1 or greater")
	}
	fields := make([]SearchField, 0, len(options.Fields))
	for _, field := range options.Fields {
		field.Name = strings.ToLower(strings.TrimSpace(field.Name))
		switch field.Name {
		case "title", "author", "publisher", "year":
		default:
			return options, NewError(InvalidArgument, "unsupported search field "+field.Name)
		}
		field.Value = strings.Join(strings.Fields(field.Value), " ")
		if field.Value == "" {
			continue
		}
		if len(field.Value) > 300 {
			return options, NewError(InvalidArgument, field.Name+" must be at most 300 characters")
		}
		if field.Name == "year" && !yearPattern.MatchString(field.Value) {
			return options, NewError(InvalidArgument, "year must be a publication year such as 2008")
		}
		fields = append(fields, field)
	}
	if len(fields) > 9 {
		return options, NewError(InvalidArgument, "search accepts at most nine fields")
	}
	options.Fields = fields
	if strings.TrimSpace(query) == "" && len(fields) == 0 {
		return options, NewError(InvalidArgument, "search query is empty; pass a query or at least one of title, author, publisher or year")
	}
	var err error
	if options.Language, err = normalizeFilter(options.Language, "language", languageCodePattern.MatchString); err != nil {
		return options, NewError(InvalidArgument, "language must be a language code such as en, or a comma-separated list such as en,de")
	}
	for i, v := range options.Language {
		options.Language[i] = canonicalLanguage(v)
	}
	if options.Source, err = normalizeFilter(options.Source, "source", sourcePattern.MatchString); err != nil {
		return options, err
	}
	if options.Content, err = normalizeFilter(options.Content, "content", func(v string) bool { return contentTypes[v] }); err != nil {
		return options, err
	}
	if options.Format, err = normalizeFilter(options.Format, "format", searchFormatPattern.MatchString); err != nil {
		return options, err
	}
	if options.Access, err = normalizeFilter(options.Access, "access", func(v string) bool { return v == "aa_download" || v == "aa_scidb" }); err != nil {
		return options, err
	}
	sortValue, ok := sortValues[strings.ToLower(strings.TrimSpace(options.Sort))]
	if !ok {
		return options, NewError(InvalidArgument, fmt.Sprintf("unsupported sort %q; use relevance, newest, oldest, largest, smallest, newest_added, oldest_added or random", options.Sort))
	}
	options.Sort = sortValue
	options = ResolveSearch(options, defaultContent)
	switch options.Index {
	case "", "journals", "meta":
	default:
		return options, NewError(InvalidArgument, "unsupported search index "+options.Index)
	}
	if options.Index == "meta" && (options.Sort == "largest" || options.Sort == "smallest") {
		return options, NewError(InvalidArgument, "catalog records have no file size; sort by relevance, newest, oldest, newest_added, oldest_added or random")
	}
	return options, nil
}
func normalizeFilter(raw []string, name string, valid func(string) bool) ([]string, error) {
	var values []string
	for _, part := range raw {
		part = strings.ToLower(strings.TrimSpace(part))
		excluded := strings.HasPrefix(part, "-")
		value := strings.TrimSpace(strings.TrimPrefix(part, "-"))
		if value == "" {
			if excluded {
				return nil, NewError(InvalidArgument, name+" has a - without a value")
			}
			continue
		}
		if !valid(value) {
			return nil, NewError(InvalidArgument, fmt.Sprintf("unsupported %s %q", name, value))
		}
		if excluded {
			value = "-" + value
		}
		values = append(values, value)
	}
	return values, nil
}

// canonicalLanguage restores tag casing that Anna's codes use, such as zh-Hant.
func canonicalLanguage(code string) string {
	prefix := ""
	if strings.HasPrefix(code, "-") {
		prefix, code = "-", code[1:]
	}
	parts := strings.Split(code, "-")
	for i := 1; i < len(parts); i++ {
		switch len(parts[i]) {
		case 4:
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		case 2:
			parts[i] = strings.ToUpper(parts[i])
		}
	}
	return prefix + strings.Join(parts, "-")
}
