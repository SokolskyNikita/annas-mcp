package mirror

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

const (
	// freshFor is how recently a mirror must have worked to be used again
	// without a check.
	freshFor = 10 * time.Minute
	// sweepInterval is how often every mirror is checked again while all of
	// them are on hold, so a short network outage on the user's side does
	// not leave the server failing for the whole hold.
	sweepInterval = 30 * time.Second
	// checkTimeout bounds one round of checks.
	checkTimeout = 5 * time.Second
)

// Selector picks the mirror for archive requests. It contacts mirrors only
// while a caller is waiting for one: the mirror in use is kept for as long
// as it works, a mirror that worked recently is used without a check, and
// the remaining mirrors are checked in parallel only when neither applies.
type Selector struct {
	hosts  []string
	health *Health
	check  ProbeFunc

	mu        sync.Mutex
	current   string
	selecting chan struct{}
	// lastRound is when the last round of checks ended. While every
	// mirror is on hold, the next round waits sweepInterval after it.
	lastRound time.Time
}

// NewSelector chooses among hosts, preferred first, with check deciding
// whether a mirror works.
func NewSelector(hosts []string, health *Health, check ProbeFunc) *Selector {
	return &Selector{hosts: slices.Clone(hosts), health: health, check: check}
}

// Has reports whether host is one of the selector's mirrors.
func (s *Selector) Has(host string) bool {
	return slices.Contains(s.hosts, host)
}

// Hosts returns the mirrors, preferred first.
func (s *Selector) Hosts() []string {
	return slices.Clone(s.hosts)
}

// Current returns the mirror in use, or "" before the first selection.
func (s *Selector) Current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Select returns the mirror to use, never one of exclude. Concurrent
// callers share one round of checks. When every mirror is on hold it
// returns an *UnavailableError without contacting any of them, except for
// one round of checks per sweepInterval.
func (s *Selector) Select(ctx context.Context, exclude ...string) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		s.mu.Lock()
		if s.current != "" && s.usable(s.current, exclude) {
			host := s.current
			s.mu.Unlock()
			return host, nil
		}
		if pending := s.selecting; pending != nil {
			s.mu.Unlock()
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-pending:
				continue
			}
		}
		if host := s.freshest(exclude); host != "" {
			s.current = host
			s.mu.Unlock()
			logger.GetLogger().Info("Using an Anna mirror that worked recently", zap.String("host", host))
			return host, nil
		}
		candidates := s.eligible(exclude)
		if len(candidates) == 0 {
			if s.health.now().Sub(s.lastRound) < sweepInterval {
				err := s.unavailable()
				s.mu.Unlock()
				return "", err
			}
			candidates = slices.DeleteFunc(slices.Clone(s.hosts), func(host string) bool { return slices.Contains(exclude, host) })
			if len(candidates) == 0 {
				err := s.unavailable()
				s.mu.Unlock()
				return "", err
			}
		}
		pending := make(chan struct{})
		s.selecting = pending
		s.mu.Unlock()

		host, err := s.checkAll(ctx, candidates)

		s.mu.Lock()
		if ctx.Err() == nil {
			s.lastRound = s.health.now()
		}
		if host != "" {
			s.current = host
		}
		if errors.Is(err, errAllFailed) {
			err = s.unavailable()
		}
		s.selecting = nil
		close(pending)
		s.mu.Unlock()
		return host, err
	}
}

var errAllFailed = errors.New("every mirror failed its check")

// checkAll checks the candidates in parallel and returns the first that
// passes. Returning cancels the checks still running.
func (s *Selector) checkAll(ctx context.Context, candidates []string) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	type result struct {
		host string
		err  error
	}
	results := make(chan result, len(candidates))
	check := s.check
	for _, host := range candidates {
		go func() {
			// A check that starts after another mirror won is not sent.
			if err := checkCtx.Err(); err != nil {
				results <- result{host, err}
				return
			}
			results <- result{host, check(checkCtx, host)}
		}()
	}
	log := logger.GetLogger()
	for range candidates {
		checked := <-results
		// A caller that gave up learned nothing about the mirrors.
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if checked.err == nil {
			s.health.Passed(checked.host)
			log.Info("Selected Anna mirror", zap.String("host", checked.host))
			return checked.host, nil
		}
		s.health.Failed(checked.host, checkFailure(checked.err))
		log.Warn("Anna mirror failed its check", zap.String("host", checked.host), zap.Error(checked.err))
	}
	return "", errAllFailed
}

func (s *Selector) usable(host string, exclude []string) bool {
	return !slices.Contains(exclude, host) && s.health.Eligible(host)
}

func (s *Selector) eligible(exclude []string) []string {
	var hosts []string
	for _, host := range s.hosts {
		if s.usable(host, exclude) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// freshest returns the usable mirror that worked most recently, if that was
// within freshFor.
func (s *Selector) freshest(exclude []string) string {
	best, bestOK := "", s.health.now().Add(-freshFor)
	for _, host := range s.eligible(exclude) {
		if ok := s.health.State(host).LastOK; ok.After(bestOK) {
			best, bestOK = host, ok
		}
	}
	return best
}

// unavailable describes every mirror's last failure. The caller holds s.mu.
func (s *Selector) unavailable() *UnavailableError {
	now := s.health.now()
	// The next contact is the next sweep or the first hold to end.
	next := s.lastRound.Add(sweepInterval)
	if !next.After(now) {
		next = time.Time{}
	}
	err := &UnavailableError{}
	for _, host := range s.hosts {
		state := s.health.State(host)
		if state.HeldUntil.After(now) && (next.IsZero() || state.HeldUntil.Before(next)) {
			next = state.HeldUntil
		}
		if state.LastFail.IsZero() {
			continue
		}
		err.Mirrors = append(err.Mirrors, MirrorStatus{Host: host, Reason: state.Reason, Ago: now.Sub(state.LastFail), Blocked: state.Blocked})
	}
	if !next.IsZero() {
		err.RetryIn = next.Sub(now)
	}
	return err
}

// MirrorStatus is one mirror's last failure.
type MirrorStatus struct {
	Host    string
	Reason  string
	Ago     time.Duration
	Blocked bool
}

// UnavailableError reports that no mirror can be used right now.
type UnavailableError struct {
	Mirrors []MirrorStatus
	// RetryIn is how long until a call would contact a mirror again.
	RetryIn time.Duration
}

func (e *UnavailableError) Error() string {
	parts := make([]string, 0, len(e.Mirrors))
	for _, m := range e.Mirrors {
		when := "just now"
		if ago := m.Ago.Round(time.Second); ago > 0 {
			when = ago.String() + " ago"
		}
		parts = append(parts, fmt.Sprintf("%s %s %s", m.Host, m.Reason, when))
	}
	message := "no Anna's Archive mirror is working"
	if len(parts) > 0 {
		message += ": " + strings.Join(parts, ", ")
	}
	if e.RetryIn > 0 {
		message += fmt.Sprintf(". Try again in %s", roundUp(e.RetryIn))
	}
	return message
}

// Blocked reports whether every mirror refused access, as opposed to
// failing or being unreachable.
func (e *UnavailableError) Blocked() bool {
	for _, m := range e.Mirrors {
		if !m.Blocked {
			return false
		}
	}
	return len(e.Mirrors) > 0
}

func roundUp(d time.Duration) time.Duration {
	return ((d + time.Second - 1) / time.Second) * time.Second
}

// checkFailure describes a failed check for UnavailableError.
func checkFailure(err error) Failure {
	var status *statusError
	var network net.Error
	switch {
	case errors.As(err, &status):
		return Failure{Reason: fmt.Sprintf("answered its check with HTTP %d", status.code), Blocked: status.code == http.StatusForbidden}
	case errors.Is(err, errNotUptime):
		return Failure{Reason: "answered its check with a page that is not Anna's Archive", Blocked: true}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &network) && network.Timeout():
		return Failure{Reason: "did not answer its check"}
	default:
		return Failure{Reason: "could not be reached"}
	}
}
