package env

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLoadProvidersDefersInvalidArchiveSettings(t *testing.T) {
	setEnv(t, "", "", "http://fallback.example", "", "")
	t.Setenv("OPENALEX_API_KEY", " test-openalex-key ")
	cfg := LoadProviders()
	if cfg.OpenAlexAPIKey != "test-openalex-key" {
		t.Fatal("OpenAlex key was not read and trimmed")
	}
	if cfg.ArchiveError == nil {
		t.Fatal("invalid archive settings must retain their error")
	}
	if cfg.DownloadPath == "" || cfg.SessionFile == "" {
		t.Fatal("provider configuration lost shared path settings")
	}
	if _, err := Load(); err == nil {
		t.Fatal("strict archive configuration must still reject invalid settings")
	}
	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), cfg.OpenAlexAPIKey) {
		t.Fatal("OpenAlex key leaked into configuration JSON")
	}
}
