package mirror

import (
	"maps"
	"math/rand/v2"
	"sync"
	"time"
)

// A failing mirror is held back from selection for baseHold, doubled for
// each further failure in a row, up to maxHold.
const (
	baseHold = 15 * time.Second
	maxHold  = 5 * time.Minute
)

// Failure is one failed request to a mirror, or one failed check.
type Failure struct {
	// Reason completes a sentence that starts with the host, as in
	// "annas-archive.gl did not answer".
	Reason string
	// Blocked marks a mirror that refused access rather than failing.
	Blocked bool
	// RetryAfter is the mirror's own Retry-After, which replaces the hold.
	RetryAfter time.Duration
}

// HostState is what recent requests and checks showed about one mirror.
type HostState struct {
	LastOK    time.Time `json:"last_ok,omitzero"`
	LastFail  time.Time `json:"last_fail,omitzero"`
	Failures  int       `json:"failures,omitempty"`
	HeldUntil time.Time `json:"held_until,omitzero"`
	Reason    string    `json:"reason,omitempty"`
	Blocked   bool      `json:"blocked,omitempty"`
}

// Health remembers what recent requests and checks showed about each mirror,
// so selection skips mirrors known to be failing. It learns only from
// requests a caller was making anyway and from the checks Select runs for a
// waiting caller; nothing runs in the background.
type Health struct {
	mu     sync.Mutex
	hosts  map[string]HostState
	now    func() time.Time
	jitter func(time.Duration) time.Duration
	// store, when set, shares the states with other processes. It is read
	// the first time Health is used and told about every change.
	store *Store
	load  sync.Once
}

func NewHealth() *Health {
	return &Health{hosts: make(map[string]HostState), now: time.Now, jitter: jitter}
}

// jitter spreads holds by up to a tenth either way, so servers that saw the
// same failure do not all come back to a mirror at once.
func jitter(hold time.Duration) time.Duration {
	return hold + time.Duration((rand.Float64()*0.2-0.1)*float64(hold))
}

// Succeeded records a request that worked, which clears the mirror's
// failures.
func (h *Health) Succeeded(host string) {
	h.update(host, func(s *HostState, now time.Time) {
		s.LastOK, s.Failures, s.HeldUntil = now, 0, time.Time{}
	})
}

// Passed records a passed check. It ends the hold but keeps the count of
// failures, so a mirror that answers checks while failing real requests is
// held longer each time.
func (h *Health) Passed(host string) {
	h.update(host, func(s *HostState, now time.Time) {
		s.LastOK, s.HeldUntil = now, time.Time{}
	})
}

// Failed holds the mirror back from selection. Requests that were already
// under way when the mirror was set aside report the same outage, so a
// failure during a hold does not lengthen it; only one after the hold ends
// counts as another failure in a row.
func (h *Health) Failed(host string, failure Failure) {
	h.update(host, func(s *HostState, now time.Time) {
		s.LastFail, s.Reason, s.Blocked = now, failure.Reason, failure.Blocked
		if now.Before(s.HeldUntil) && failure.RetryAfter <= 0 {
			return
		}
		s.Failures++
		hold := failure.RetryAfter
		if hold <= 0 {
			hold = h.jitter(holdFor(s.Failures))
		}
		s.HeldUntil = now.Add(min(hold, maxHold))
	})
}

func holdFor(failures int) time.Duration {
	hold := baseHold
	for i := 1; i < failures && hold < maxHold; i++ {
		hold *= 2
	}
	return min(hold, maxHold)
}

// Eligible reports whether the mirror may be selected: it is not on hold.
func (h *Health) Eligible(host string) bool {
	h.loaded()
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.now().Before(h.hosts[host].HeldUntil)
}

// State returns what is known about the mirror.
func (h *Health) State(host string) HostState {
	h.loaded()
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.hosts[host]
}

func (h *Health) update(host string, change func(*HostState, time.Time)) {
	h.loaded()
	h.mu.Lock()
	state := h.hosts[host]
	change(&state, h.now())
	h.hosts[host] = state
	h.mu.Unlock()
	if h.store != nil {
		h.store.changed(host, state)
	}
}

// loaded reads the shared states the first time Health is used.
func (h *Health) loaded() {
	if h.store != nil {
		h.load.Do(h.store.load)
	}
}

// merge takes each state that is newer than the one Health has.
func (h *Health) merge(states map[string]HostState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for host, state := range states {
		if latest(state).After(latest(h.hosts[host])) {
			h.hosts[host] = state
		}
	}
}

func (h *Health) snapshot() map[string]HostState {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.hosts)
}

// latest is when anything was last learned about a mirror.
func latest(state HostState) time.Time {
	if state.LastOK.After(state.LastFail) {
		return state.LastOK
	}
	return state.LastFail
}
