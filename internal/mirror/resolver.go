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
	maxProbeBodyBytes   = 512 << 10
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
// first one that passes its probe.
type Resolver struct {
	client        *http.Client
	statusPageURL string
	probe         ProbeFunc
	accountCookie string
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

// SetAccountCookie sets the normalized aa_account_id2 Cookie header sent with
// mirror probes. Call it before Resolve; it is never sent to the status page.
func (r *Resolver) SetAccountCookie(cookieHeader string) {
	r.accountCookie = strings.TrimSpace(cookieHeader)
}

// Resolve returns the first reachable official mirror, trying the best-ranked
// candidates first. Discovery and probing share one deadline. When they fail,
// the validated fallback is returned instead, unless the caller's own context
// was cancelled.
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
		log.Warn("Mirror discovery failed", zap.String("statusPageURL", r.statusPageURL), zap.String("fallbackBaseURL", fb.baseURL), zap.Error(err))
		return fb.use(err)
	}
	log.Info("Discovered Anna mirror candidates", zap.Int("candidates", len(candidates)))

	for _, candidate := range candidates {
		if err := attemptCtx.Err(); err != nil {
			return fb.use(err)
		}
		if err := r.probe(attemptCtx, candidate.BaseURL); err != nil {
			log.Warn("Anna mirror probe failed", zap.String("baseURL", candidate.BaseURL), zap.Error(err))
			continue
		}
		log.Info("Selected Anna mirror automatically", zap.String("baseURL", candidate.BaseURL))
		return candidate.BaseURL, nil
	}
	log.Warn("No Anna mirror passed its probe", zap.String("fallbackBaseURL", fb.baseURL))
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
