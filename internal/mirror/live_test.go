package mirror

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in check against SLUM and the official mirrors. Probes go out without
// an account cookie, as before the first sign-in, so this also catches the
// uptime endpoint moving behind DDoS-Guard's browser check. Needs no
// credentials.
func TestLiveResolve(t *testing.T) {
	if os.Getenv("ANNAS_MCP_TEST_MIRROR_LIVE") != "1" {
		t.Skip("set ANNAS_MCP_TEST_MIRROR_LIVE=1 to probe the real mirrors")
	}
	// Resolve runs first so its time includes new connections, as on startup.
	resolver := NewResolver(nil, DefaultStatusPageURL, nil)
	start := time.Now()
	selected, err := resolver.Resolve(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("no official mirror passed its probe: %v", err)
	}
	t.Logf("selected %s in %s, including discovery", selected, time.Since(start).Round(time.Millisecond))

	candidates, err := resolver.discover(context.Background(), maxMirrorCandidates)
	if err != nil {
		t.Fatalf("discovery: %v", err)
	}
	for _, candidate := range candidates {
		start := time.Now()
		err := resolver.defaultProbe(context.Background(), candidate.BaseURL)
		t.Logf("probe %s (SLUM status %q): %v after %s", candidate.BaseURL, candidate.Status, err, time.Since(start).Round(time.Millisecond))
	}
}
