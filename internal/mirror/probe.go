package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// probePath is Anna's uptime endpoint. It answers from the archive's own
// servers with a few bytes of JSON and, unlike /search, is not behind the
// DDoS-Guard browser check for anonymous requests. A real search takes
// seconds and returns hundreds of kilobytes, which made probes time out.
const probePath = "/dyn/up/"

func (r *Resolver) defaultProbe(ctx context.Context, baseURL string) error {
	host, err := ParseCandidateURL(baseURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+host+probePath, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	if r.accountCookie != nil {
		if cookie := strings.TrimSpace(r.accountCookie()); cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
	}

	resp, err := r.do(req, true)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("probe failed with status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBodyBytes))
	if err != nil {
		return err
	}
	if !isAnnaUpResponse(body) {
		return errors.New("probe response was not Anna's Archive uptime status")
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

// isAnnaUpResponse accepts the uptime endpoint's JSON object, such as
// {"aa_logged_in":0}. A DDoS-Guard page or another site's response fails.
func isAnnaUpResponse(body []byte) bool {
	var status map[string]json.RawMessage
	if err := json.Unmarshal(body, &status); err != nil {
		return false
	}
	_, ok := status["aa_logged_in"]
	return ok
}
