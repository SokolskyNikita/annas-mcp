package anna

import (
	"context"
	"net/url"
	"testing"
)

func TestOnlyCandidateMirrorsReceiveTheSessionCookie(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{AutoBaseURL: true})
	var hosts struct {
		Comment string
		Cases   []struct {
			URL    string
			Mirror bool
		}
	}
	loadCases(t, "archive_hosts", &hosts)
	for _, tc := range hosts.Cases {
		u, _ := url.Parse(tc.URL)
		if _, got := client.archiveHost(u); got != tc.Mirror {
			t.Errorf("archiveHost(%s) = %v, want %v", tc.URL, got, tc.Mirror)
		}
	}
}

func TestConfiguredMirrorIsTriedFirst(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{BaseURL: "https://proxy.example/", AutoBaseURL: true})
	if hosts := client.mirrors.Hosts(); len(hosts) != 4 || hosts[0] != "proxy.example" {
		t.Fatalf("candidates: %v", hosts)
	}
}

func TestExplicitBaseURLCanOverrideAutomaticMirrorAllowlist(t *testing.T) {
	t.Parallel()

	client := mustClient(Config{BaseURL: "annas-archive.su"})
	base, err := client.baseURLFor(context.Background())
	if err != nil {
		t.Fatalf("baseURLFor returned error: %v", err)
	}
	if base != "https://annas-archive.su" {
		t.Fatalf("explicit base URL became %q, want configured origin", base)
	}
}
