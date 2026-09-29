package anna

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDOILookupUsesCurrentMirrorForIdentity(t *testing.T) {
	t.Parallel()
	resolved, details := 0, 0
	client := NewClient(Config{
		BaseURL: "annas-archive.gl", AutoBaseURL: true, AccountCookie: "aa_account_id2=test",
		Resolver: func(context.Context) (string, error) {
			resolved++
			if resolved == 1 {
				return "annas-archive.gl", nil
			}
			return "annas-archive.gd", nil
		},
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "doi.org" {
				if req.Header.Get("Cookie") != "" {
					t.Fatal("sent account cookie to DOI.org")
				}
				return fixtureResponse(t, req, http.StatusOK, "doi_metadata.json", "application/json", nil), nil
			}
			if req.Header.Get("Cookie") != "aa_account_id2=test" {
				t.Fatal("archive request used a stale mirror without its cookie")
			}
			if strings.HasPrefix(req.URL.Path, "/scidb/") {
				return fixtureResponse(t, req, http.StatusForbidden, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Host != "annas-archive.gd" {
				t.Fatal("search/detail request did not move to the current mirror")
			}
			if strings.HasPrefix(req.URL.Path, "/md5/") {
				details++
				return fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", nil), nil
			}
			return fixtureResponse(t, req, http.StatusOK, "search_result.html", "text/html", nil), nil
		})},
	})
	paper, err := client.LookupDOI(context.Background(), "10.1000/example", time.Second)
	if err != nil || paper == nil || paper.DOI != "10.1000/example" || details != 1 || resolved != 2 {
		t.Fatalf("mirror change broke DOI identity validation: paper=%+v error=%v details=%d resolutions=%d", paper, err, details, resolved)
	}
}

func TestDOIIdentityRejectsDifferentRecordRedirect(t *testing.T) {
	t.Parallel()
	const selected = "0123456789abcdef0123456789abcdef"
	const other = "abcdef0123456789abcdef0123456789"
	client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := fixtureResponse(t, req, http.StatusOK, "article_detail.html", "text/html", nil)
		if req.URL.Path == "/md5/"+selected {
			response.StatusCode = http.StatusFound
			response.Header.Set("Location", "/md5/"+other)
		}
		return response, nil
	})}})
	if _, err := client.archiveIdentityForBook(context.Background(), &Book{Hash: selected}); err == nil {
		t.Fatal("metadata from a different MD5 record was accepted")
	}
}

func TestDOIAuthorNormalizationPreservesNonLatinNames(t *testing.T) {
	t.Parallel()
	if sameAuthorSet([]string{"李 王"}, []string{"山 田"}) || sameAuthorSet([]string{"Иван Петров"}, []string{"Анна Сергеева"}) {
		t.Fatal("distinct non-Latin authors collapsed to the same identity")
	}
}

func TestDOIAuthorNormalizationPreservesDistinctLetters(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{{"Ægir Smith", "Agir Smith"}, {"Œdipe Smith", "Odipe Smith"}, {"Þor Smith", "Tor Smith"}} {
		metadata := doiMetadata{Title: "Shared title", Authors: []string{pair[0]}}
		record := archiveIdentity{Title: "Shared title", Authors: []string{pair[1]}}
		if verifyDOIIdentity("10.1000/example", metadata, record) {
			t.Errorf("accepted different authors %q and %q", pair[0], pair[1])
		}
		record.Authors = []string{strings.ToLower(pair[0])}
		if !verifyDOIIdentity("10.1000/example", metadata, record) {
			t.Errorf("rejected the same author %q", pair[0])
		}
	}
}
