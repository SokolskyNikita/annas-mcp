package anna

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SokolskyNikita/annas-mcp/internal/apperr"
	"github.com/SokolskyNikita/annas-mcp/internal/mirror"
)

// Anna's Archive only serves search to a signed-in member: without a valid
// aa_account_id2 cookie, /search redirects to a DDoS-Guard check that answers
// 403, and the cookie stops working after about a week. The secret key that
// authorizes the fast download API also signs in through the account form, so
// the client mints the cookie itself and signs in again when the archive
// rejects it. ANNAS_ACCOUNT_COOKIE, when set, is only the first session.

const accountCookieName = "aa_account_id2"

// signInRetryInterval stops a rejected or failed sign-in from being repeated
// on every request. A session minted this recently that still gets 403 points
// at the membership tier or the mirror, not at an expired cookie.
const signInRetryInterval = time.Minute

const maxSignInBodyBytes = 1 << 20

// manualCookieSteps is the way around a failed sign-in: a browser session
// passes the same checks, and its cookie can be copied by hand.
const manualCookieSteps = "in a browser signed in to Anna's Archive, open developer tools → Application (Chrome, Edge) or Storage (Firefox, Safari) → Cookies and copy the aa_account_id2 value into ANNAS_ACCOUNT_COOKIE. Then restart the MCP server. Details: https://github.com/SokolskyNikita/annas-mcp#signing-in"

// sessionCookie returns the Cookie header for archive requests.
func (c *Client) sessionCookie() string {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.cookie
}

// ensureSession signs in before the first archive request when no cookie was
// configured. Mirror probes send the session too, so it runs before them.
func (c *Client) ensureSession(ctx context.Context) error {
	if c.sessionCookie() != "" {
		return nil
	}
	_, err := c.signIn(ctx, "")
	return err
}

// signIn replaces the rejected session and reports whether the caller should
// retry with the new one. Concurrent callers share one sign-in.
func (c *Client) signIn(ctx context.Context, rejected string) (bool, error) {
	if strings.TrimSpace(c.config.SecretKey) == "" {
		return false, nil
	}
	for {
		c.sessionMu.Lock()
		if c.cookie != rejected {
			c.sessionMu.Unlock()
			return true, nil
		}
		if pending := c.signingIn; pending != nil {
			c.sessionMu.Unlock()
			select {
			case <-ctx.Done():
				return false, ctx.Err()
			case <-pending:
				continue
			}
		}
		if !c.signedInAt.IsZero() && time.Since(c.signedInAt) < signInRetryInterval {
			err := c.signInErr
			c.sessionMu.Unlock()
			return false, err
		}
		pending := make(chan struct{})
		c.signingIn = pending
		c.sessionMu.Unlock()

		cookie, err := c.signInAnyMirror(ctx)

		c.sessionMu.Lock()
		adopted := err == nil && ctx.Err() == nil
		if ctx.Err() == nil {
			if adopted {
				c.cookie = cookie
			}
			c.signedInAt, c.signInErr = time.Now(), err
		}
		c.signingIn = nil
		close(pending)
		c.sessionMu.Unlock()
		if adopted {
			c.saveSession(cookie)
		}
		return err == nil, err
	}
}

// signInAnyMirror tries the current mirror first. With automatic mirror
// selection the other official mirrors are fallbacks, because sign-in runs
// before the resolver has found a working one.
func (c *Client) signInAnyMirror(ctx context.Context) (string, error) {
	c.baseMu.Lock()
	origins := []string{c.baseURL}
	c.baseMu.Unlock()
	if c.config.AutoBaseURL {
		for _, host := range mirror.OfficialHosts() {
			if origin := "https://" + host; origin != origins[0] {
				origins = append(origins, origin)
			}
		}
	}
	var reasons []string
	code := apperr.Upstream
	for _, origin := range origins {
		cookie, err := c.signInAt(ctx, origin)
		if err == nil {
			return cookie, nil
		}
		if ctx.Err() != nil || apperr.CodeOf(err) == apperr.Config {
			return "", err
		}
		// A mirror that answered without a session is the case a browser
		// cookie gets around, so it outranks mirrors that were unreachable.
		if apperr.CodeOf(err) == apperr.UpstreamBlocked {
			code = apperr.UpstreamBlocked
		}
		reasons = append(reasons, err.Error())
	}
	return "", apperr.New(code, "automatic sign-in with ANNAS_SECRET_KEY failed ("+strings.Join(reasons, "; ")+"). If Anna's Archive works in your browser, set the cookie by hand: "+manualCookieSteps)
}

func (c *Client) signInAt(ctx context.Context, origin string) (string, error) {
	accountURL := origin + "/account/"
	form := url.Values{"key": {strings.TrimSpace(c.config.SecretKey)}}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, accountURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", BrowserUserAgent)
	client := c.requestClient(0)
	// A successful sign-in sets the cookie on a redirect back to /account/.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		return "", redactErr(requestFailure(err, accountURL), c.config.SecretKey)
	}
	defer response.Body.Close()
	for _, cookie := range response.Cookies() {
		if cookie.Name == accountCookieName && cookie.Value != "" && cookie.MaxAge >= 0 {
			return accountCookieName + "=" + cookie.Value, nil
		}
	}
	// Response bodies never reach the error: they can echo the key back.
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxSignInBodyBytes))
	host := responseHost(response, accountURL)
	if bytes.Contains(body, []byte("Invalid secret key")) {
		return "", apperr.New(apperr.Config, fmt.Sprintf("Anna's Archive rejected ANNAS_SECRET_KEY. Copy the secret key from https://%s/account/ into ANNAS_SECRET_KEY and restart the MCP server", host))
	}
	if response.StatusCode == http.StatusForbidden && bytes.Contains(body, []byte("DDoS-Guard")) {
		return "", apperr.New(apperr.UpstreamBlocked, host+" put the sign-in form behind a DDoS-Guard browser check")
	}
	return "", apperr.New(apperr.UpstreamBlocked, fmt.Sprintf("%s answered the sign-in form with HTTP status %d but no session", host, response.StatusCode))
}

// forbiddenMessage explains a 403 that a renewed session did not fix.
func (c *Client) forbiddenMessage() string {
	if strings.TrimSpace(c.config.SecretKey) != "" {
		return "Anna's Archive returned 403 even after signing in with ANNAS_SECRET_KEY. Check that your membership allows this (search needs Lucky Librarian or higher). If searching works in your browser, set the cookie by hand: " + manualCookieSteps
	}
	return "Anna's Archive returned 403, so ANNAS_ACCOUNT_COOKIE has probably expired. Set ANNAS_SECRET_KEY so the server signs in by itself, or replace the cookie: " + manualCookieSteps
}
