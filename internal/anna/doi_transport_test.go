package anna

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDOILookupUsesCurrentMirrorForIdentity(t *testing.T) {
	t.Parallel()
	var details atomic.Int32
	var glFailed atomic.Bool
	client := NewClient(Config{
		BaseURL: "annas-archive.gl", AutoBaseURL: true, AccountCookie: "aa_account_id2=test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/dyn/up/" {
				return mirrorUp(t, req, func(host string) bool {
					return host == "annas-archive.gl" || host == "annas-archive.gd" && glFailed.Load()
				})
			}
			if req.URL.Host == "doi.org" {
				if req.Header.Get("Cookie") != "" {
					t.Fatal("sent account cookie to DOI.org")
				}
				return fixtureResponse(t, req, http.StatusOK, attentionCSL, "application/json", nil), nil
			}
			if req.Header.Get("Cookie") != "aa_account_id2=test" {
				t.Fatal("archive request used a stale mirror without its cookie")
			}
			if strings.HasPrefix(req.URL.Path, "/scidb/") {
				if req.URL.Host != "annas-archive.gl" {
					return fixtureResponse(t, req, http.StatusNotFound, "blocked.txt", "text/plain", nil), nil
				}
				glFailed.Store(true)
				return fixtureResponse(t, req, http.StatusBadGateway, "blocked.txt", "text/plain", nil), nil
			}
			if req.URL.Host != "annas-archive.gd" {
				t.Fatal("search/detail request did not move to the current mirror")
			}
			if strings.HasPrefix(req.URL.Path, "/md5/") {
				details.Add(1)
				return fixtureResponse(t, req, http.StatusOK, recordAttentionPage, "text/html", nil), nil
			}
			return htmlResponse(req, http.StatusOK, searchPageWith(t, capturedCard(t, searchAttentionPage, attentionHash))), nil
		})},
	})
	paper, err := client.LookupDOI(context.Background(), attentionDOI, time.Second)
	if err != nil || paper == nil || paper.DOI != attentionDOI || details.Load() != 1 || client.mirrors.Current() != "annas-archive.gd" {
		t.Fatalf("mirror change broke DOI identity validation: paper=%+v error=%v details=%d mirror=%s", paper, err, details.Load(), client.mirrors.Current())
	}
}

func TestDOIIdentityRejectsDifferentRecordRedirect(t *testing.T) {
	t.Parallel()
	const selected = "0123456789abcdef0123456789abcdef"
	const other = "abcdef0123456789abcdef0123456789"
	client := NewClient(Config{BaseURL: "annas.test", AccountCookie: "aa_account_id2=test", HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		response := fixtureResponse(t, req, http.StatusOK, recordAttentionPage, "text/html", nil)
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
