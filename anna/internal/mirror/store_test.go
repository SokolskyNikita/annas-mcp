package mirror

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// process is one run of the server sharing the state file at path.
func process(t *testing.T, path string, clk *clock) *Health {
	t.Helper()
	health := NewHealth()
	health.now = clk.Now
	health.jitter = func(hold time.Duration) time.Duration { return hold }
	NewStore(path, health)
	return health
}

func savedStates(t *testing.T, path string) map[string]HostState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved stateFile
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Mirrors
}

func TestStoreLetsTheNextProcessUseAConfirmedMirrorWithoutChecking(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cache", "mirrors.json")
	clk := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}

	first := process(t, path, clk)
	checks := newChecker(map[string]error{"a.example": errDown, "c.example": errDown})
	if host, err := NewSelector(hosts, first, checks.check).Select(context.Background()); err != nil || host != "b.example" {
		t.Fatalf("first process: %q, %v", host, err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %v, want 0600", info.Mode().Perm())
	}

	clk.Advance(time.Minute)
	second := process(t, path, clk)
	selector := NewSelector(hosts, second, func(_ context.Context, host string) error {
		t.Errorf("second process checked %s", host)
		return nil
	})
	if host, err := selector.Select(context.Background()); err != nil || host != "b.example" {
		t.Fatalf("second process: %q, %v", host, err)
	}
}

func TestStoreCarriesHoldsToTheNextProcess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mirrors.json")
	clk := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	process(t, path, clk).Failed("a.example", Failure{Reason: "answered HTTP 502"})

	clk.Advance(5 * time.Second)
	second := process(t, path, clk)
	if second.Eligible("a.example") || second.State("a.example").Reason != "answered HTTP 502" {
		t.Fatalf("hold was not shared: %+v", second.State("a.example"))
	}
	clk.Advance(baseHold)
	if !second.Eligible("a.example") {
		t.Fatal("shared hold did not end")
	}
}

func TestStoreIgnoresDamagedAndStaleState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clk := &clock{now: now}
	dir := t.TempDir()

	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if health := process(t, corrupt, clk); !health.Eligible("a.example") || !health.State("a.example").LastOK.IsZero() {
		t.Fatal("a damaged file changed the state")
	}

	saved := filepath.Join(dir, "mirrors.json")
	data, _ := json.Marshal(stateFile{Mirrors: map[string]HostState{
		"stale.example":  {LastOK: now.Add(-staleAfter - time.Second)},
		"future.example": {LastOK: now.Add(time.Hour)},
		"held.example":   {LastFail: now, HeldUntil: now.Add(24 * time.Hour), Failures: 1},
		"ok.example":     {LastOK: now.Add(-time.Minute)},
	}})
	if err := os.WriteFile(saved, data, 0o600); err != nil {
		t.Fatal(err)
	}
	health := process(t, saved, clk)
	for _, host := range []string{"stale.example", "future.example"} {
		if state := health.State(host); !state.LastOK.IsZero() {
			t.Errorf("%s was loaded: %+v", host, state)
		}
	}
	if got := health.State("held.example").HeldUntil; !got.Equal(now.Add(maxHold)) {
		t.Errorf("a day-long hold was loaded as ending at %s", got)
	}
	if health.State("ok.example").LastOK.IsZero() {
		t.Error("a recent success was not loaded")
	}
}

func TestStoreWritesOnHoldChangesAndSuccessesOncePerMinute(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mirrors.json")
	start := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	clk := &clock{now: start}
	health := process(t, path, clk)

	health.Succeeded("a.example")
	clk.Advance(30 * time.Second)
	health.Succeeded("a.example")
	if got := savedStates(t, path)["a.example"].LastOK; !got.Equal(start) {
		t.Fatalf("a success within the minute was written: %s", got)
	}
	clk.Advance(31 * time.Second)
	health.Succeeded("a.example")
	if got := savedStates(t, path)["a.example"].LastOK; !got.Equal(clk.Now()) {
		t.Fatalf("a success after a minute was not written: %s", got)
	}
	clk.Advance(time.Second)
	health.Failed("a.example", Failure{Reason: "did not answer"})
	if got := savedStates(t, path)["a.example"]; got.HeldUntil.IsZero() {
		t.Fatalf("a hold was not written: %+v", got)
	}
}

func TestStoreKeepsWhatOtherProcessesLearned(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mirrors.json")
	clk := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	first, second := process(t, path, clk), process(t, path, clk)
	// Both read the empty file before either wrote.
	first.Eligible("a.example")
	second.Eligible("a.example")

	first.Failed("a.example", Failure{Reason: "answered HTTP 502"})
	clk.Advance(time.Second)
	second.Succeeded("b.example")
	saved := savedStates(t, path)
	if saved["a.example"].HeldUntil.IsZero() || saved["b.example"].LastOK.IsZero() {
		t.Fatalf("one process overwrote the other: %+v", saved)
	}
	// The newer state of a mirror wins.
	clk.Advance(time.Second)
	second.Succeeded("a.example")
	if saved := savedStates(t, path)["a.example"]; !saved.HeldUntil.IsZero() || saved.LastOK.IsZero() {
		t.Fatalf("an older hold replaced a newer success: %+v", saved)
	}
}
