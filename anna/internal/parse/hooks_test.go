package parse

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestUpstreamWatchCoversHooks checks that anna/scripts/upstream-watch.json
// watches every hook the parsers use: a change to one in Anna's templates
// must reach the weekly check. A watched string covers a hook it is part of.
func TestUpstreamWatchCoversHooks(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("../../scripts/upstream-watch.json")
	if err != nil {
		t.Fatal(err)
	}
	var watch struct {
		Files []struct {
			Hooks []string `json:"hooks"`
		} `json:"files"`
	}
	if err := json.Unmarshal(data, &watch); err != nil {
		t.Fatal(err)
	}
	for _, hook := range hooks {
		covered := false
		for _, file := range watch.Files {
			for _, watched := range file.Hooks {
				covered = covered || strings.Contains(hook, watched)
			}
		}
		if !covered {
			t.Errorf("anna/scripts/upstream-watch.json does not watch %q", hook)
		}
	}
}
