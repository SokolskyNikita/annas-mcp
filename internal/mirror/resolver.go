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

const (
	DefaultStatusPageURL = "https://open-slum.org/"

	// DefaultResolveTimeout bounds one automatic selection attempt. Callers can
	// provide a shorter deadline through Resolve's context or ResolveOptions.
	DefaultResolveTimeout = 15 * time.Second
	defaultProbeTimeout   = 5 * time.Second
	maxStatusPageBytes    = 2 << 20
	maxProbeBodyBytes     = 512 << 10
	maxMirrorCandidates   = 8
	browserUserAgent      = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

type ResolveOptions struct {
	FallbackBaseURL string
	Timeout         time.Duration
	MaxCandidates   int
}

type ProbeFunc func(context.Context, string) error

type Resolver struct {
	client        *http.Client
	statusPageURL string
	probe         ProbeFunc
	accountCookie string
}

// NewResolver creates a resolver. The status page and mirror requests are
// restricted to HTTPS; an invalid custom status page falls back to the known
// SLUM URL because this constructor cannot return an error.
func NewResolver(client *http.Client, statusPageURL string, probe ProbeFunc) *Resolver {
	if client == nil {
		client = &http.Client{Timeout: defaultProbeTimeout}
	}

	if strings.TrimSpace(statusPageURL) == "" || !validHTTPSURL(statusPageURL) {
		statusPageURL = DefaultStatusPageURL
	}

	resolver := &Resolver{
		client:        client,
		statusPageURL: statusPageURL,
	}
	if probe == nil {
		resolver.probe = resolver.defaultProbe
	} else {
		resolver.probe = probe
	}
	return resolver
}

// SetAccountCookie attaches the already-normalized aa_account_id2 Cookie
// header to mirror probes. It must be called before Resolve.
func (r *Resolver) SetAccountCookie(cookieHeader string) {
	r.accountCookie = strings.TrimSpace(cookieHeader)
}

// Resolve selects a reachable mirror from the current SLUM page. Discovery
// and probing share a bounded child context. An explicit caller cancellation
// is returned to the caller; a resolver timeout may use a validated fallback.
func (r *Resolver) Resolve(ctx context.Context, opts ResolveOptions) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	fallbackBaseURL, fallbackErr := ParseBaseURL(opts.FallbackBaseURL)
	if fallbackErr != nil {
		fallbackBaseURL = ""
	}

	resolveTimeout := opts.Timeout
	if resolveTimeout <= 0 {
		resolveTimeout = DefaultResolveTimeout
	}
	resolveCtx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	l := logger.GetLogger()
	html, err := r.fetch(resolveCtx, r.statusPageURL)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		l.Warn("Failed to fetch mirror status page, using fallback mirror if available",
			zap.String("statusPageURL", r.statusPageURL),
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		return resolveFallback(ctx, resolveCtx, fallbackBaseURL, err)
	}

	candidates, err := ParseStatusPageHTML(html)
	if err != nil || len(candidates) == 0 {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		l.Warn("Failed to discover Anna mirror candidates, using fallback mirror if available",
			zap.Int("candidateCount", len(candidates)),
			zap.String("fallbackBaseURL", fallbackBaseURL),
			zap.Error(err),
		)
		if err == nil {
			err = errors.New("no Anna mirror candidates discovered")
		}
		return resolveFallback(ctx, resolveCtx, fallbackBaseURL, err)
	}

	maxCandidates := candidateLimit(opts.MaxCandidates)
	candidates = rankCandidates(candidates)
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	l.Info("Resolved Anna mirror candidates",
		zap.Int("discoveredCandidates", len(candidates)),
		zap.Int("candidateLimit", maxCandidates),
	)

	for _, candidate := range candidates {
		if err := resolveCtx.Err(); err != nil {
			return resolveFallback(ctx, resolveCtx, fallbackBaseURL, err)
		}
		if r.probe != nil {
			if err := r.probe(resolveCtx, candidate.BaseURL); err != nil {
				l.Warn("Anna mirror probe failed",
					zap.String("baseURL", candidate.BaseURL),
					zap.Error(err),
				)
				continue
			}
		}

		l.Info("Selected Anna mirror automatically",
			zap.String("baseURL", candidate.BaseURL),
		)
		return candidate.BaseURL, nil
	}

	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	noReachableErr := errors.New("no reachable Anna mirror found")
	if fallbackBaseURL != "" {
		l.Warn("No reachable Anna mirror found, using fallback mirror",
			zap.String("fallbackBaseURL", fallbackBaseURL),
		)
	}
	return resolveFallback(ctx, resolveCtx, fallbackBaseURL, noReachableErr)
}

func resolveFallback(ctx, resolveCtx context.Context, fallbackBaseURL string, err error) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if fallbackBaseURL != "" {
		return fallbackBaseURL, nil
	}
	if resolveCtx.Err() != nil {
		return "", resolveCtx.Err()
	}
	return "", err
}

func candidateLimit(limit int) int {
	if limit <= 0 || limit > maxMirrorCandidates {
		return maxMirrorCandidates
	}
	return limit
}
