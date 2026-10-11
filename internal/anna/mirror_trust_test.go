package anna

import (
	"context"
	"net/url"
	"testing"
)

func TestOnlyCandidateMirrorsReceiveTheSessionCookie(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{AutoBaseURL: true})
	for raw, want := range map[string]bool{
		"https://annas-archive.gl/search":    true,
		"https://annas-archive.pk/md5/x":     true,
		"https://ANNAS-ARCHIVE.GD/scidb/x":   true,
		"http://annas-archive.gl/search":     false,
		"https://annas-archive.su/search":    false,
		"https://annas-archive.gl.evil.com/": false,
		"https://user@annas-archive.gl/":     false,
		"https://downloads.example/file":     false,
	} {
		u, _ := url.Parse(raw)
		if _, got := client.archiveHost(u); got != want {
			t.Errorf("archiveHost(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestConfiguredMirrorIsTriedFirst(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{BaseURL: "https://proxy.example/", AutoBaseURL: true})
	if hosts := client.mirrors.Hosts(); len(hosts) != 4 || hosts[0] != "proxy.example" {
		t.Fatalf("candidates: %v", hosts)
	}
}

func TestExplicitBaseURLCanOverrideAutomaticMirrorAllowlist(t *testing.T) {
	t.Parallel()

	client := NewClient(Config{BaseURL: "annas-archive.su"})
	base, err := client.baseURLFor(context.Background())
	if err != nil {
		t.Fatalf("baseURLFor returned error: %v", err)
	}
	if base != "https://annas-archive.su" {
		t.Fatalf("explicit base URL became %q, want configured origin", base)
	}
}
