package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/SokolskyNikita/annas-mcp/anna/internal/parse"
)

// probePath is Anna's uptime endpoint. It answers from the archive's own
// servers with a few bytes of JSON and, unlike /search, is not behind the
// DDoS-Guard browser check for anonymous requests. A real search takes
// seconds and returns hundreds of kilobytes, which made probes time out.
const probePath = "/dyn/up/"

const (
	maxProbeBodyBytes = 64 << 10
	browserUserAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)

// ProbeFunc reports whether the mirror at host is usable.
type ProbeFunc func(ctx context.Context, host string) error

// errNotUptime is a check answered by something other than the uptime
// endpoint, such as a DDoS-Guard page.
var errNotUptime = errors.New("did not answer with Anna's Archive uptime status")

// statusError is a check answered with an HTTP error status.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("probe failed with status %d", e.code) }

// Prober checks mirrors on Anna's uptime endpoint.
type Prober struct {
	client *http.Client
	cookie func() string
}

// NewProber checks mirrors with client, sending the Cookie header that
// cookie returns, if any, so a renewed session reaches later checks. A nil
// client uses http.DefaultClient; Selector bounds each check's time.
func NewProber(client *http.Client, cookie func() string) *Prober {
	if client == nil {
		client = http.DefaultClient
	}
	return &Prober{client: client, cookie: cookie}
}

// Check is a ProbeFunc.
func (p *Prober) Check(ctx context.Context, host string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+probePath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	if p.cookie != nil {
		if cookie := strings.TrimSpace(p.cookie()); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}

	resp, err := p.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &statusError{resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes))
	if err != nil {
		return err
	}
	if !parse.IsUpAnswer(body) {
		return errNotUptime
	}
	return nil
}

// do follows redirects only within the mirror and only over HTTPS, so the
// session cookie never leaves it.
func (p *Prober) do(req *http.Request) (*http.Response, error) {
	client := *p.client
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
			if !sameOrigin(previous.URL, next.URL) {
				return fmt.Errorf("refusing cross-origin redirect to %s", next.URL)
			}
		}
		return nil
	}
	return client.Do(req)
}
