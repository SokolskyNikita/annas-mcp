package modes

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna"
	"github.com/SokolskyNikita/annas-mcp/papers"
)

func renderWork(work *papers.Work) string {
	if work == nil {
		return ""
	}
	pairs := []string{
		"Title", work.Title, "Authors", work.Authors, "Year", work.Year,
		"Type", work.WorkType, "Provider", work.Provider, "OpenAlex ID", work.OpenAlexID,
		"DOI", work.DOI, "Journal or repository", work.Journal, "Language", work.Language,
		"Access", strings.Join(work.Access, ", "), "Hash", work.Hash,
		"Version warning", yesOrEmpty(work.PossibleIssues), "Page", work.PageURL,
		"Abstract", work.Abstract,
	}
	if work.CitationCount > 0 {
		pairs = append(pairs, "Citations", strconv.Itoa(work.CitationCount))
	}
	for _, location := range work.Locations {
		copy := location.Source
		if location.Version != "" {
			copy += " (" + location.Version + ")"
		}
		if location.License != "" {
			copy += " [" + location.License + "]"
		}
		pairs = append(pairs, "Location", location.ID+" "+strings.TrimSpace(copy), "Landing page", location.LandingPageURL)
		if command := articleDownloadCommand(location.DownloadArgs); command != "" {
			pairs = append(pairs, "Get this copy", command)
		}
	}
	if command := articleDownloadCommand(work.DownloadArgs); command != "" {
		pairs = append(pairs, "Download", command)
	}
	return labeled(appendStats(pairs, work.Stats)...)
}

func articleDownloadCommand(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	command := []string{"annas-cli", "article-download"}
	if hash, ok := args["hash"].(string); ok && hash != "" {
		command = append(command, "--hash", shellArgument(hash))
	} else if id, ok := args["openalex_id"].(string); ok && id != "" {
		command = append(command, "--openalex-id", shellArgument(id))
	} else if doi, ok := args["doi"].(string); ok && doi != "" {
		command = append(command, shellArgument(doi))
	} else {
		return ""
	}
	for _, name := range []string{"provider", "location_id", "version", "artifact"} {
		if value, ok := args[name].(string); ok && value != "" {
			command = append(command, "--"+strings.ReplaceAll(name, "_", "-"), shellArgument(value))
		}
	}
	return strings.Join(command, " ")
}

// shellArgument quotes examples without allowing shell expansion in metadata.
func shellArgument(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func printWorkPage(w io.Writer, page *papers.SearchResult) error {
	if page.Work != nil {
		_, err := fmt.Fprintln(w, renderWork(page.Work))
		return err
	}
	if len(page.Results) == 0 {
		_, err := fmt.Fprintln(w, "No articles found.")
		return err
	}
	for i, work := range page.Results {
		if _, err := fmt.Fprintf(w, "Article %d:\n%s\n\n", i+1, renderWork(work)); err != nil {
			return err
		}
	}
	for _, work := range page.PartialMatches {
		if _, err := fmt.Fprintf(w, "Partial match (may ignore filters):\n%s\n\n", renderWork(work)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "Provider: %s. Page %d: showing %d of %d results.\n", page.Provider, page.Page, len(page.Results), page.Total); err != nil {
		return err
	}
	if len(page.NextSearchArgs) > 0 {
		_, err := fmt.Fprintf(w, "Next page: %s\n", articleSearchCommand(page.NextSearchArgs))
		return err
	}
	return nil
}

func articleSearchCommand(args map[string]any) string {
	command := []string{"annas-cli", "article-search"}
	if query, ok := args["query"].(string); ok && query != "" {
		command = append(command, shellArgument(query))
	}
	for _, name := range []string{"provider", "title", "author", "publisher", "year", "language", "format", "source", "sort", "content", "work_type", "author_id", "institution_id", "repository_id", "relation", "work_id", "cursor", "page", "limit"} {
		if value, ok := args[name]; ok && fmt.Sprint(value) != "" {
			command = append(command, "--"+strings.ReplaceAll(name, "_", "-"), shellArgument(fmt.Sprint(value)))
		}
	}
	for _, name := range []string{"downloadable_only", "search_descriptions", "include_stats"} {
		if value, ok := args[name].(bool); ok && value {
			command = append(command, "--"+strings.ReplaceAll(name, "_", "-"))
		}
	}
	return strings.Join(command, " ")
}

func printReadResult(w io.Writer, result *papers.ReadResult) error {
	if _, err := fmt.Fprintln(w, labeled("Title", result.Title, "Source", result.Source, "Page", result.URL), "\n\n"+result.Text); err != nil {
		return err
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintln(w, "Warning:", warning); err != nil {
			return err
		}
	}
	if result.NextArgs != nil {
		_, err := fmt.Fprintf(w, "\nRead more: annas-cli article-read %s --continuation %s --limit %d\n", shellArgument(result.NextArgs.ID), shellArgument(result.NextArgs.Continuation), result.NextArgs.Limit)
		return err
	}
	return nil
}

func printArticleDownloadResult(w io.Writer, result any, asJSON bool) error {
	if asJSON {
		return json.NewEncoder(w).Encode(result)
	}
	switch value := result.(type) {
	case anna.DownloadResult:
		return printDownloadResult(w, value, "Article", false)
	case papers.DownloadResult:
		return printCoordinatedDownload(w, &value)
	case *papers.DownloadResult:
		return printCoordinatedDownload(w, value)
	default:
		return fmt.Errorf("unsupported download result %T", result)
	}
}

func printCoordinatedDownload(w io.Writer, result *papers.DownloadResult) error {
	if err := printDownloadResult(w, result.DownloadResult, "Article", false); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, labeled(
		"Provider", result.Provider, "Route", result.Route, "Version", result.Version,
		"Reused local file", yesNo(result.Reused), "SHA-256", result.SHA256,
		"Page", result.URL, "Fallback", result.FallbackReason,
	))
	return err
}
