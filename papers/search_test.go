package papers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

type searchTransport func(*http.Request) (*http.Response, error)

func (f searchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func searchFixture(t *testing.T, name string) string {
	t.Helper()
	b, e := os.ReadFile("testdata/search/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func searchClient(t *testing.T, handler searchTransport) *Client {
	t.Helper()
	c, e := NewClient(Config{OpenAlexAPIKey: "test-key", HTTPClient: &http.Client{Transport: handler}})
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func jsonReply(body string) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestSearchNoDOIDissertationHasReplayableArguments(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("corpus") != "all" {
			t.Error("dissertation corpus must include repositories")
		}
		return jsonReply(searchFixture(t, "works")), nil
	})
	page, e := c.Search(context.Background(), SearchOptions{Query: "algorithms", WorkType: "dissertation", Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Results) != 1 || page.Results[0].DOI != "" || page.Results[0].DownloadArgs["openalex_id"] != "W100" {
		t.Fatalf("unexpected work: %+v", page)
	}
	if page.NextSearchArgs["provider"] != "openalex" || page.NextSearchArgs["cursor"] != "abc" {
		t.Fatalf("not replayable: %+v", page.NextSearchArgs)
	}
}
func TestSearchCursorExhaustionDoesNotRestartNumberedPaging(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) {
		var body map[string]any
		if e := json.Unmarshal([]byte(searchFixture(t, "works")), &body); e != nil {
			t.Fatal(e)
		}
		body["meta"].(map[string]any)["next_cursor"] = ""
		b, _ := json.Marshal(body)
		return jsonReply(string(b)), nil
	})
	page, e := c.Search(context.Background(), SearchOptions{Query: "algorithms", Cursor: "last", Limit: 1})
	if e != nil {
		t.Fatal(e)
	}
	if page.MorePages || page.NextSearchArgs != nil {
		t.Fatalf("cursor restarted: %+v", page)
	}
}
func TestAnyTypeIsSentUnrestricted(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Query().Get("filter"), "type:") {
			t.Error("any type was restricted")
		}
		return jsonReply(searchFixture(t, "works")), nil
	})
	if _, e := c.Search(context.Background(), SearchOptions{Query: "algorithms", WorkType: "any"}); e != nil {
		t.Fatal(e)
	}
}
func TestDetailsKeepsSignedFileURLPrivateAndBoundsOffset(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) { return jsonReply(searchFixture(t, "exact")), nil })
	work, e := c.Details(context.Background(), "W100", DetailsOptions{})
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(work)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(b), "secret") {
		t.Fatal("signed file URL leaked")
	}
	if len(work.Locations) != 1 || work.Locations[0].DownloadURL == "" || work.Locations[0].DownloadArgs == nil {
		t.Fatal("private location cannot be selected")
	}
	work, e = c.Details(context.Background(), "W100", DetailsOptions{LocationOffset: int(^uint(0) >> 1)})
	if e != nil {
		t.Fatal(e)
	}
	if len(work.Locations) != 0 {
		t.Fatal("out-of-bounds page should be empty")
	}
}
func TestExactLookupRejectsDiscoveryFiltersBeforeHTTP(t *testing.T) {
	calls := 0
	c := searchClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		return jsonReply(searchFixture(t, "exact")), nil
	})
	if _, e := c.Search(context.Background(), SearchOptions{Query: "W100", WorkType: "dissertation"}); e == nil {
		t.Fatal("ignored filter")
	}
	if calls != 0 {
		t.Fatal("invalid arguments issued HTTP")
	}
}

func TestCitationTraversalDoesNotExcludeDissertations(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) {
		filter := r.URL.Query().Get("filter")
		if strings.Contains(filter, "type:") || !strings.Contains(filter, "cited_by:W100") {
			t.Fatalf("unexpected bibliography filter: %s", filter)
		}
		return jsonReply(searchFixture(t, "works")), nil
	})
	if _, err := c.Search(context.Background(), SearchOptions{Relation: "references", WorkID: "W100"}); err != nil {
		t.Fatal(err)
	}
}

func TestEmptySearchRemainsUsefulWhenArchiveFallbackIsUnavailable(t *testing.T) {
	c := searchClient(t, func(r *http.Request) (*http.Response, error) { return jsonReply(searchFixture(t, "empty")), nil })
	c.config.Archive = &downloadArchiveFake{}
	result, err := c.Search(context.Background(), SearchOptions{Query: "absent work"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != "openalex" || result.Results == nil || len(result.Results) != 0 || len(result.Warnings) != 1 {
		t.Fatalf("unexpected empty-search outcome: %+v", result)
	}
}
