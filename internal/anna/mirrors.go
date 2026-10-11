package anna

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/logger"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
	"go.uber.org/zap"
)

// baseURLFor returns the origin for the next archive request, signing in
// first when there is no session yet.
func (c *Client) baseURLFor(ctx context.Context) (string, error) {
	if c.configErr != nil {
		return "", c.configErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.ensureSession(ctx); err != nil {
		return "", err
	}
	if c.mirrors == nil {
		return c.baseURL, nil
	}
	host, err := c.mirrors.Select(ctx)
	if err != nil {
		return "", mirrorUnavailable(err)
	}
	return "https://" + host, nil
}

// mirrorUnavailable gives a *mirror.UnavailableError its public code.
func mirrorUnavailable(err error) error {
	var unavailable *mirror.UnavailableError
	if !errors.As(err, &unavailable) {
		return err
	}
	if unavailable.Blocked() {
		return apperr.Wrap(apperr.UpstreamBlocked, "", err)
	}
	return apperr.Wrap(apperr.Upstream, "", err)
}

// onMirror runs fn against the selected mirror and records the mirror as
// working when fn succeeds. Only a whole operation counts: a mirror that
// answers requests with pages that are not what was asked for is failing.
func (c *Client) onMirror(ctx context.Context, fn func(base string) error) error {
	base, err := c.baseURLFor(ctx)
	if err != nil {
		return err
	}
	if err := fn(base); err != nil {
		return err
	}
	c.mirrorSucceeded(base)
	return nil
}

// archiveHost returns the host of u when it is one of the archive's
// mirrors. Only these receive the session cookie.
func (c *Client) archiveHost(u *url.URL) (string, bool) {
	if u == nil || u.Scheme != "https" || u.User != nil {
		return "", false
	}
	host := strings.ToLower(u.Host)
	if c.mirrors != nil {
		return host, c.mirrors.Has(host)
	}
	return host, "https://"+host == c.baseURL
}

// trackedHost returns the mirror that rawURL goes to, when automatic
// selection keeps track of it.
func (c *Client) trackedHost(rawURL string) (string, bool) {
	if c.mirrors == nil {
		return "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", false
	}
	return c.archiveHost(u)
}

// mirrorSucceeded records that an operation worked on the mirror of rawURL.
func (c *Client) mirrorSucceeded(rawURL string) {
	if host, ok := c.trackedHost(rawURL); ok {
		c.health.Succeeded(host)
	}
}

// mirrorFailed holds the mirror of rawURL back from selection.
func (c *Client) mirrorFailed(rawURL string, failure mirror.Failure) {
	if host, ok := c.trackedHost(rawURL); ok {
		c.health.Failed(host, failure)
		logger.GetLogger().Warn("Anna mirror failed", zap.String("host", host), zap.String("reason", failure.Reason))
	}
}

// responseFailure says how an archive response shows its mirror failing, or
// returns nil when it does not. The fast download API answers 403 about the
// account, not the mirror.
func responseFailure(request *http.Request, response *http.Response) *mirror.Failure {
	switch status := response.StatusCode; {
	case status >= 500:
		return &mirror.Failure{Reason: fmt.Sprintf("answered HTTP %d", status)}
	case status == http.StatusTooManyRequests:
		return &mirror.Failure{Reason: "rate-limited requests (HTTP 429)", RetryAfter: retryAfter(response.Header.Get("Retry-After"))}
	case status == http.StatusForbidden && !request.URL.Query().Has("key"):
		return &mirror.Failure{Reason: "refused access (HTTP 403)", Blocked: true}
	}
	return nil
}

// unreachable describes a request that got no response.
func unreachable(err error) mirror.Failure {
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return mirror.Failure{Reason: "did not answer"}
	}
	return mirror.Failure{Reason: "could not be reached"}
}

// retryAfter reads a Retry-After header given in seconds. A date is ignored,
// leaving the usual hold.
func retryAfter(value string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
