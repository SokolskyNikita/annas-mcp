package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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

var candidateHostPattern = regexp.MustCompile(`^annas-archive\.[a-z0-9-]+$`)

// Candidate is a mirror listed in the current SLUM directory.
//
// SLUM's current page publishes the availability status in the directory, so
// the old heartbeat endpoint and its monitor IDs are intentionally not part of
// this model.
type Candidate struct {
	BaseURL string
	Status  string
}

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

// ParseBaseURL validates and canonicalizes a configured archive base URL. A
// bare hostname is interpreted as HTTPS for backwards-compatible environment
// configuration. Explicit HTTP URLs, credentials, paths, queries, fragments,
// and malformed hosts are rejected.
func ParseBaseURL(raw string) (string, error) {
	return parseBaseURL(raw, true)
}

// ParseCandidateURL validates a URL discovered in SLUM. Discovered mirrors
// must be HTTPS origins whose host is exactly annas-archive.<single-label>.
// This rejects userinfo tricks such as annas-archive.gl@evil.example.
func ParseCandidateURL(raw string) (string, error) {
	value, err := parseBaseURL(raw, false)
	if err != nil {
		return "", err
	}
	host := value
	if port := strings.LastIndexByte(host, ':'); port >= 0 {
		host = host[:port]
	}
	if !candidateHostPattern.MatchString(host) {
		return "", fmt.Errorf("mirror host %q is not an annas-archive single-label origin", host)
	}
	return host, nil
}

// ParseStatusPageHTML extracts mirrors from the current SLUM directory. The
// status page is HTML, so use the DOM rather than regexes over generated
// markup. A missing or empty Anna card is reported as no candidates and lets
// Resolve use its configured fallback.
func ParseStatusPageHTML(html string) ([]Candidate, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, err
	}

	var card *goquery.Selection
	doc.Find(".site-card").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		title := strings.ToLower(strings.TrimSpace(selection.Find(".site-card-title").First().Text()))
		if title == "annas" || title == "anna's archive" || title == "anna’s archive" {
			card = selection
			return false
		}
		return true
	})
	if card == nil {
		return nil, nil
	}

	candidates := make([]Candidate, 0, maxMirrorCandidates)
	seen := make(map[string]struct{})
	card.Find(".domain-item-dense").EachWithBreak(func(_ int, item *goquery.Selection) bool {
		if len(candidates) >= maxMirrorCandidates {
			return false
		}
		href, ok := item.Find("a[href]").First().Attr("href")
		if !ok {
			return true
		}
		baseURL, parseErr := ParseCandidateURL(href)
		if parseErr != nil {
			return true
		}
		if _, exists := seen[baseURL]; exists {
			return true
		}
		seen[baseURL] = struct{}{}
		candidates = append(candidates, Candidate{
			BaseURL: baseURL,
			Status:  statusFromDirectoryItem(item),
		})
		return true
	})

	return candidates, nil
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
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		if resolveCtx.Err() != nil {
			return "", resolveCtx.Err()
		}
		return "", err
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
		if fallbackBaseURL != "" {
			return fallbackBaseURL, nil
		}
		if resolveCtx.Err() != nil {
			return "", resolveCtx.Err()
		}
		if err == nil {
			err = errors.New("no Anna mirror candidates discovered")
		}
		return "", err
	}

	candidates = rankCandidates(candidates)
	maxCandidates := opts.MaxCandidates
	if maxCandidates <= 0 || maxCandidates > maxMirrorCandidates {
		maxCandidates = maxMirrorCandidates
	}
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}
	l.Info("Resolved Anna mirror candidates",
		zap.Int("discoveredCandidates", len(candidates)),
		zap.Int("candidateLimit", maxCandidates),
	)

	for _, candidate := range candidates {
		if err := resolveCtx.Err(); err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if fallbackBaseURL != "" {
				return fallbackBaseURL, nil
			}
			return "", err
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
	if fallbackBaseURL != "" {
		l.Warn("No reachable Anna mirror found, using fallback mirror",
			zap.String("fallbackBaseURL", fallbackBaseURL),
		)
		return fallbackBaseURL, nil
	}
	if resolveCtx.Err() != nil {
		return "", resolveCtx.Err()
	}
	return "", errors.New("no reachable Anna mirror found")
}

func (r *Resolver) defaultProbe(ctx context.Context, baseURL string) error {
	host, err := ParseCandidateURL(baseURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+"/search?q=test", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	if r.accountCookie != "" {
		req.Header.Set("Cookie", r.accountCookie)
	}

	resp, err := r.do(req, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("probe failed with status %d", resp.StatusCode)
	}
	// Search pages can be much larger than the probe needs to inspect. Read a
	// bounded prefix and look for the stable Anna marker without rejecting a
	// healthy response merely because it contains many search results.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes))
	if err != nil {
		return err
	}
	if !probeResponseHasAnnaMarker(body) {
		return errors.New("probe response did not contain the Anna's Archive marker")
	}
	return nil
}

func (r *Resolver) fetch(ctx context.Context, rawURL string) (string, error) {
	if !validHTTPSURL(rawURL) {
		return "", fmt.Errorf("status page URL must be an HTTPS origin: %s", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	resp, err := r.do(req, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("unexpected status %d for %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxStatusPageBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxStatusPageBytes {
		return "", errors.New("status page response is too large")
	}
	return string(body), nil
}

func (r *Resolver) do(req *http.Request, sameOriginOnly bool) (*http.Response, error) {
	client := *r.client
	previousPolicy := client.CheckRedirect
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if previousPolicy != nil {
			if err := previousPolicy(next, via); err != nil {
				return err
			}
		}
		if len(via) > 0 {
			previous := via[len(via)-1]
			if !strings.EqualFold(next.URL.Scheme, "https") {
				return fmt.Errorf("refusing HTTPS downgrade redirect to %s", next.URL)
			}
			if sameOriginOnly && !sameOrigin(previous.URL, next.URL) {
				return fmt.Errorf("refusing cross-origin redirect to %s", next.URL)
			}
		}
		return nil
	}
	return client.Do(req)
}

func rankCandidates(candidates []Candidate) []Candidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		switch strings.ToLower(strings.TrimSpace(candidate.Status)) {
		case "down", "timeout":
			continue
		default:
			filtered = append(filtered, candidate)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		leftRank := statusRank(filtered[i].Status)
		rightRank := statusRank(filtered[j].Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return filtered[i].BaseURL < filtered[j].BaseURL
	})
	return filtered
}

func statusFromDirectoryItem(item *goquery.Selection) string {
	badge := item.Find(".status-badge").First()
	if badge.Length() == 0 {
		return ""
	}
	if classes, ok := badge.Attr("class"); ok {
		for _, class := range strings.Fields(strings.ToLower(classes)) {
			switch class {
			case "up", "protected", "degraded", "down", "timeout", "unknown":
				return class
			}
		}
	}
	text := strings.ToLower(strings.TrimSpace(badge.Text()))
	for _, status := range []string{"protected", "degraded", "timeout", "down", "up", "unknown"} {
		if strings.Contains(text, status) {
			return status
		}
	}
	return ""
}

func statusRank(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "up":
		return 0
	case "protected":
		return 1
	case "degraded":
		return 2
	case "unknown", "":
		return 3
	default:
		return 4
	}
}

func probeResponseHasAnnaMarker(body []byte) bool {
	normalized := strings.ToLower(strings.ReplaceAll(string(body), "’", "'"))
	return strings.Contains(normalized, "anna's archive") || strings.Contains(normalized, "in one place - anna")
}

func parseBaseURL(raw string, allowPort bool) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid base URL: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", errors.New("base URL must use HTTPS")
	}
	if parsed.User != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return "", errors.New("base URL must be an HTTPS origin without credentials")
	}
	if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("base URL must not include a path, query, or fragment")
	}
	port := parsed.Port()
	if !allowPort && port != "" {
		return "", errors.New("discovered mirror must not include a port")
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "" || strings.ContainsAny(host, "\\/\x00\r\n") {
		return "", errors.New("base URL has an invalid hostname")
	}
	if port != "" {
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		host += ":" + port
	}
	return host, nil
}

func validHTTPSURL(raw string) bool {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Scheme, "https") || parsed.Hostname() == "" || parsed.User != nil {
		return false
	}
	return true
}

func sameOrigin(left, right *url.URL) bool {
	if left == nil || right == nil {
		return false
	}
	return strings.EqualFold(left.Scheme, right.Scheme) && strings.EqualFold(left.Host, right.Host)
}
