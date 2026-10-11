package mirror

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"go.uber.org/zap"
)

// DefaultStatusPageURL is the SLUM directory that lists Anna's current mirrors.
const DefaultStatusPageURL = "https://open-slum.org/"

// DefaultResolveTimeout bounds one automatic selection attempt. Callers can
// provide a shorter deadline through Resolve's context or ResolveOptions.
const DefaultResolveTimeout = 15 * time.Second

const (
	defaultProbeTimeout = 5 * time.Second
	maxMirrorCandidates = 8
	maxStatusPageBytes  = 2 << 20
	maxProbeBodyBytes   = 64 << 10
	browserUserAgent    = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

var (
	errNoCandidates = errors.New("no Anna mirror candidates discovered")
	errNoReachable  = errors.New("no reachable Anna mirror found")
)

// ResolveOptions tunes one Resolve call. Zero values select the defaults.
type ResolveOptions struct {
	// FallbackBaseURL is returned when discovery or every probe fails. It is
	// ignored unless it is a bare host or an HTTPS origin.
	FallbackBaseURL string
	Timeout         time.Duration
	MaxCandidates   int
}

// ProbeFunc reports whether the mirror at baseURL is usable.
type ProbeFunc func(ctx context.Context, baseURL string) error

// Resolver discovers official mirrors from a status page and returns the
// first one to pass its probe.
type Resolver struct {
	client        *http.Client
	statusPageURL string
	probe         ProbeFunc
	accountCookie func() string
}

// NewResolver never fails: a nil client gets a short default timeout, a nil
// probe uses defaultProbe, and a blank or non-HTTPS status page URL is
// replaced by DefaultStatusPageURL.
func NewResolver(client *http.Client, statusPageURL string, probe ProbeFunc) *Resolver {
	r := &Resolver{client: client, statusPageURL: strings.TrimSpace(statusPageURL), probe: probe}
	if r.client == nil {
		r.client = &http.Client{Timeout: defaultProbeTimeout}
	}
	if !validHTTPSURL(r.statusPageURL) {
		r.statusPageURL = DefaultStatusPageURL
	}
	if r.probe == nil {
		r.probe = r.defaultProbe
	}
	return r
}

// SetAccountCookie sets where mirror probes read the normalized aa_account_id2
// Cookie header, so a renewed session reaches later probes. Call it before
// Resolve; the cookie is never sent to the status page.
func (r *Resolver) SetAccountCookie(cookieHeader func() string) {
	r.accountCookie = cookieHeader
}

// Resolve probes the best-ranked official mirrors at the same time and returns
// the first to pass, so a slow or hanging mirror does not hold up selection.
// Discovery and probing share one deadline. When they fail, the validated
// fallback is returned instead, unless the caller's own context was cancelled.
func (r *Resolver) Resolve(ctx context.Context, opts ResolveOptions) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultResolveTimeout
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fb := newFallback(ctx, attemptCtx, opts.FallbackBaseURL)
	log := logger.GetLogger()

	candidates, err := r.discover(attemptCtx, candidateLimit(opts.MaxCandidates))
	if err != nil {
		log.Warn("Mirror discovery failed", zap.String("statusPageURL", r.statusPageURL), fb.field(), zap.Error(err))
		return fb.use(err)
	}
	log.Info("Discovered Anna mirror candidates", zap.Int("candidates", len(candidates)))

	// Returning cancels attemptCtx and with it the probes still running. The
	// channel is buffered so they can still report and exit.
	type result struct {
		baseURL string
		err     error
	}
	results := make(chan result, len(candidates))
	for _, candidate := range candidates {
		go func() {
			results <- result{candidate.BaseURL, r.probe(attemptCtx, candidate.BaseURL)}
		}()
	}
	for range candidates {
		probed := <-results
		if probed.err != nil {
			log.Warn("Anna mirror probe failed", zap.String("baseURL", probed.baseURL), zap.Error(probed.err))
			continue
		}
		log.Info("Selected Anna mirror automatically", zap.String("baseURL", probed.baseURL))
		return probed.baseURL, nil
	}
	log.Warn("No Anna mirror passed its probe", fb.field())
	return fb.use(errNoReachable)
}

// discover fetches the status page and returns at most limit official
// candidates, best-ranked first.
func (r *Resolver) discover(ctx context.Context, limit int) ([]Candidate, error) {
	page, err := r.fetch(ctx, r.statusPageURL)
	if err != nil {
		return nil, err
	}
	candidates, err := ParseStatusPageHTML(page)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, errNoCandidates
	}
	candidates = rankCandidates(candidates)
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	return candidates, nil
}

func candidateLimit(requested int) int {
	if requested > 0 && requested < maxMirrorCandidates {
		return requested
	}
	return maxMirrorCandidates
}

// fallback decides what Resolve returns once automatic selection has failed.
type fallback struct {
	caller, attempt context.Context
	baseURL         string
}

func newFallback(caller, attempt context.Context, raw string) fallback {
	baseURL, err := ParseBaseURL(raw)
	if err != nil {
		baseURL = ""
	}
	return fallback{caller: caller, attempt: attempt, baseURL: baseURL}
}

// field names the fallback in log entries, and is omitted when there is
// none: the caller then picks its own mirror.
func (f fallback) field() zap.Field {
	if f.baseURL == "" {
		return zap.Skip()
	}
	return zap.String("fallbackBaseURL", f.baseURL)
}

// use gives precedence to the caller's cancellation, then the configured
// fallback, then the resolver's own timeout, and finally the failure cause.
func (f fallback) use(cause error) (string, error) {
	switch {
	case f.caller.Err() != nil:
		return "", f.caller.Err()
	case f.baseURL != "":
		return f.baseURL, nil
	case f.attempt.Err() != nil:
		return "", f.attempt.Err()
	default:
		return "", cause
	}
}
