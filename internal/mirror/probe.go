package mirror

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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

func probeResponseHasAnnaMarker(body []byte) bool {
	normalized := strings.ToLower(strings.ReplaceAll(string(body), "’", "'"))
	return strings.Contains(normalized, "anna's archive") || strings.Contains(normalized, "in one place - anna")
}
