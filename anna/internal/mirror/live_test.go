package mirror

import (
	"context"
	"os"
	"testing"
	"time"
)

// Opt-in check against the official mirrors. Checks go out without an
// account cookie, as before the first sign-in, so this also catches the
// uptime endpoint moving behind DDoS-Guard's browser check. Needs no
// credentials.
func TestLiveSelect(t *testing.T) {
	if os.Getenv("ANNAS_MCP_TEST_MIRROR_LIVE") != "1" {
		t.Skip("set ANNAS_MCP_TEST_MIRROR_LIVE=1 to check the real mirrors")
	}
	prober := NewProber(nil, nil)
	// Select runs first so its time includes new connections, as on startup.
	start := time.Now()
	selected, err := NewSelector(OfficialHosts(), NewHealth(), prober.Check).Select(context.Background())
	if err != nil {
		t.Fatalf("no official mirror passed its check: %v", err)
	}
	t.Logf("selected %s in %s", selected, time.Since(start).Round(time.Millisecond))

	for _, host := range OfficialHosts() {
		ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
		start := time.Now()
		err := prober.Check(ctx, host)
		cancel()
		t.Logf("check %s: %v after %s", host, err, time.Since(start).Round(time.Millisecond))
	}
}
