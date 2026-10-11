package mirror

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var hosts = []string{"a.example", "b.example", "c.example"}

var errDown = errors.New("connection refused")

// clock is a manual clock for Health and Selector.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// testHealth has a manual clock and no jitter.
func testHealth() (*Health, *clock) {
	clk := &clock{now: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	health := NewHealth()
	health.now = clk.Now
	health.jitter = func(hold time.Duration) time.Duration { return hold }
	return health, clk
}

// checker answers checks from a table of errors; a host without an entry
// passes.
type checker struct {
	mu    sync.Mutex
	fail  map[string]error
	calls []string
}

func newChecker(fail map[string]error) *checker {
	if fail == nil {
		fail = map[string]error{}
	}
	return &checker{fail: fail}
}

func (c *checker) check(_ context.Context, host string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, host)
	return c.fail[host]
}

func (c *checker) set(host string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail[host] = err
}

func (c *checker) checked() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.calls)
}

func TestHoldsGrowWithFailuresAndStopAtTheMaximum(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	for i, want := range []time.Duration{15 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		health.Failed("a.example", Failure{Reason: "did not answer"})
		if got := health.State("a.example").HeldUntil.Sub(clk.Now()); got != want {
			t.Fatalf("failure %d: held for %s, want %s", i+1, got, want)
		}
	}
	if health.Eligible("a.example") {
		t.Fatal("a held mirror is eligible")
	}
	health.Succeeded("a.example")
	if state := health.State("a.example"); !health.Eligible("a.example") || state.Failures != 0 {
		t.Fatalf("a success did not clear the failures: %+v", state)
	}
}

func TestPassedCheckEndsTheHoldButKeepsTheFailures(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	health.Failed("a.example", Failure{})
	health.Failed("a.example", Failure{})
	health.Passed("a.example")
	if !health.Eligible("a.example") || health.State("a.example").Failures != 2 {
		t.Fatalf("after a passed check: %+v", health.State("a.example"))
	}
	// A mirror that answers checks but fails real requests is held longer
	// each time instead of starting over.
	health.Failed("a.example", Failure{})
	if got := health.State("a.example").HeldUntil.Sub(clk.Now()); got != time.Minute {
		t.Fatalf("third failure held for %s, want 1m", got)
	}
}

func TestRetryAfterReplacesTheHoldUpToTheMaximum(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	health.Failed("a.example", Failure{RetryAfter: 90 * time.Second})
	if got := health.State("a.example").HeldUntil.Sub(clk.Now()); got != 90*time.Second {
		t.Fatalf("held for %s, want Retry-After's 90s", got)
	}
	health.Failed("b.example", Failure{RetryAfter: time.Hour})
	if got := health.State("b.example").HeldUntil.Sub(clk.Now()); got != maxHold {
		t.Fatalf("held for %s, want at most %s", got, maxHold)
	}
}

func TestJitterStaysWithinATenth(t *testing.T) {
	t.Parallel()
	for range 1000 {
		if got := jitter(100 * time.Second); got < 90*time.Second || got > 110*time.Second {
			t.Fatalf("jitter(100s) = %s", got)
		}
	}
}

func TestSelectKeepsAWorkingMirrorWithoutCheckingAgain(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	checks := newChecker(map[string]error{"b.example": errDown, "c.example": errDown})
	selector := NewSelector(hosts, health, checks.check)
	if host, err := selector.Select(context.Background()); err != nil || host != "a.example" {
		t.Fatalf("Select = %q, %v", host, err)
	}
	selector.check = func(_ context.Context, host string) error {
		t.Errorf("checked %s again", host)
		return nil
	}
	for range 5 {
		clk.Advance(time.Hour)
		if host, err := selector.Select(context.Background()); err != nil || host != "a.example" {
			t.Fatalf("Select = %q, %v", host, err)
		}
	}
}

func TestSelectUsesARecentlyWorkingMirrorWithoutChecking(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	health.Succeeded("b.example")
	checks := newChecker(nil)
	host, err := NewSelector(hosts, health, checks.check).Select(context.Background())
	if err != nil || host != "b.example" || len(checks.checked()) != 0 {
		t.Fatalf("Select = %q, %v after checking %v", host, err, checks.checked())
	}
	// Past freshFor, a new selector checks before trusting it.
	clk.Advance(freshFor + time.Second)
	if _, err := NewSelector(hosts, health, checks.check).Select(context.Background()); err != nil || len(checks.checked()) == 0 {
		t.Fatalf("stale mirror used without a check: %v", err)
	}
}

func TestSelectLeavesHeldAndExcludedMirrorsAlone(t *testing.T) {
	t.Parallel()
	health, _ := testHealth()
	health.Failed("a.example", Failure{Reason: "answered HTTP 502"})
	checks := newChecker(map[string]error{"b.example": errDown})
	host, err := NewSelector(hosts, health, checks.check).Select(context.Background(), "c.example")
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Select = %q, %v; want UnavailableError", host, err)
	}
	if calls := checks.checked(); !slices.Equal(calls, []string{"b.example"}) {
		t.Fatalf("checked %v, want only b.example", calls)
	}
}

func TestSelectMovesOffAFailedMirror(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	checks := newChecker(map[string]error{"b.example": errDown, "c.example": errDown})
	selector := NewSelector(hosts, health, checks.check)
	if host, _ := selector.Select(context.Background()); host != "a.example" {
		t.Fatalf("first selection: %q", host)
	}
	// The other mirrors' holds from the first round run out, and one of
	// them recovers.
	clk.Advance(baseHold + time.Second)
	checks.set("c.example", nil)
	health.Failed("a.example", Failure{Reason: "answered HTTP 502"})
	if host, err := selector.Select(context.Background()); err != nil || host != "c.example" {
		t.Fatalf("after a.example failed: %q, %v", host, err)
	}
}

func TestSelectChecksInParallelAndCancelsTheRest(t *testing.T) {
	t.Parallel()

	var started, finished sync.WaitGroup
	started.Add(3)
	finished.Add(3)
	allStarted := make(chan struct{})
	go func() { started.Wait(); close(allStarted) }()
	health, _ := testHealth()
	selector := NewSelector(hosts, health, func(ctx context.Context, host string) error {
		defer finished.Done()
		started.Done()
		// The least preferred mirror answers once every check is running,
		// which checking one mirror at a time would never reach. The others
		// hang.
		if host == "c.example" {
			select {
			case <-allStarted:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		<-ctx.Done()
		return ctx.Err()
	})

	host, err := selector.Select(context.Background())
	if err != nil || host != "c.example" {
		t.Fatalf("Select = %q, %v; want c.example", host, err)
	}
	done := make(chan struct{})
	go func() { finished.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("checks still running after a mirror was selected")
	}
}

func TestEveryMirrorHeldFailsFastAndIsCheckedAgainEvery30Seconds(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	// Long holds, so only the 30-second rule lets a call check again.
	for _, host := range hosts {
		for range 4 {
			health.Failed(host, Failure{Reason: "did not answer"})
		}
	}
	checks := newChecker(map[string]error{"a.example": errDown, "b.example": errDown, "c.example": errDown})
	selector := NewSelector(hosts, health, checks.check)
	var unavailable *UnavailableError

	if _, err := selector.Select(context.Background()); !errors.As(err, &unavailable) || len(checks.checked()) != 3 {
		t.Fatalf("first call: %v after checking %v", err, checks.checked())
	}
	clk.Advance(20 * time.Second)
	for range 5 {
		if _, err := selector.Select(context.Background()); !errors.As(err, &unavailable) {
			t.Fatalf("within 30 seconds: %v", err)
		}
	}
	if calls := checks.checked(); len(calls) != 3 {
		t.Fatalf("calls within 30 seconds of the last round checked again: %v", calls)
	}
	if unavailable.RetryIn != 10*time.Second {
		t.Fatalf("RetryIn = %s, want 10s", unavailable.RetryIn)
	}

	clk.Advance(11 * time.Second)
	checks.set("b.example", nil)
	if host, err := selector.Select(context.Background()); err != nil || host != "b.example" {
		t.Fatalf("after 30 seconds: %q, %v", host, err)
	}
	if calls := checks.checked(); len(calls) <= 3 || !slices.Contains(calls[3:], "b.example") {
		t.Fatalf("expected one more round of checks, got %v", calls)
	}
}

func TestUnavailableErrorDescribesEachMirror(t *testing.T) {
	t.Parallel()
	health, clk := testHealth()
	checks := newChecker(map[string]error{
		"a.example": &statusError{403},
		"b.example": context.DeadlineExceeded,
		"c.example": errDown,
	})
	selector := NewSelector(hosts, health, checks.check)
	_, err := selector.Select(context.Background())
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("Select: %v", err)
	}
	// Within the holds, the next call fails without checking and says when
	// the first hold ends.
	clk.Advance(10 * time.Second)
	_, err = selector.Select(context.Background())
	message := err.Error()
	for _, want := range []string{
		"no Anna's Archive mirror is working: ",
		"a.example answered its check with HTTP 403 10s ago",
		"b.example did not answer its check 10s ago",
		"c.example could not be reached 10s ago",
		". Try again in 5s",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("message lacks %q: %s", want, message)
		}
	}
	if !errors.As(err, &unavailable) || unavailable.Blocked() {
		t.Fatalf("a mix of failures counted as blocked: %v", err)
	}

	health, _ = testHealth()
	_, err = NewSelector(hosts[:1], health, newChecker(map[string]error{"a.example": &statusError{403}}).check).Select(context.Background())
	if !errors.As(err, &unavailable) || !unavailable.Blocked() || !strings.HasSuffix(err.Error(), ". Try again in 15s") {
		t.Fatalf("a single blocked mirror: %v", err)
	}
}

func TestConcurrentCallersShareOneRoundAndMayGiveUp(t *testing.T) {
	t.Parallel()
	health, _ := testHealth()
	var mu sync.Mutex
	calls := 0
	started, release := make(chan struct{}), make(chan struct{})
	selector := NewSelector(hosts[:1], health, func(context.Context, string) error {
		mu.Lock()
		calls++
		mu.Unlock()
		close(started)
		<-release
		return nil
	})
	results := make(chan string, 2)
	for range 2 {
		go func() {
			host, _ := selector.Select(context.Background())
			results <- host
		}()
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := selector.Select(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a waiting caller could not give up: %v", err)
	}
	close(release)
	for range 2 {
		if host := <-results; host != "a.example" {
			t.Fatalf("waiter got %q", host)
		}
	}
	if calls != 1 {
		t.Fatalf("%d checks; waiters must share one", calls)
	}
}

func TestCallerThatGivesUpLearnsNothing(t *testing.T) {
	t.Parallel()
	health, _ := testHealth()
	ctx, cancel := context.WithCancel(context.Background())
	selector := NewSelector(hosts, health, func(checkCtx context.Context, _ string) error {
		cancel()
		<-checkCtx.Done()
		return checkCtx.Err()
	})
	if _, err := selector.Select(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Select: %v", err)
	}
	for _, host := range hosts {
		if state := health.State(host); state.Failures != 0 || !health.Eligible(host) {
			t.Fatalf("%s was held after the caller gave up: %+v", host, state)
		}
	}
	if !selector.lastRound.IsZero() {
		t.Fatal("an abandoned round delays the next one")
	}
}
