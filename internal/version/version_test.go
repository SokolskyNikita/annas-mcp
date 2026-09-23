package version

import (
	"strings"
	"testing"
)

func TestGetVersionUsesEmbeddedVersionByDefault(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	Version = ""

	if got, want := GetVersion(), strings.TrimSpace(embeddedVersion); got != want {
		t.Fatalf("GetVersion() = %q, want embedded version %q", got, want)
	}
}

func TestGetVersionPrefersLinkerValue(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })
	Version = " v9.9.9 "

	if got, want := GetVersion(), "v9.9.9"; got != want {
		t.Fatalf("GetVersion() = %q, want linker value %q", got, want)
	}
}
