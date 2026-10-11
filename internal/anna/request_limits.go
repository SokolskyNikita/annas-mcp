package anna

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"time"
)

// Measured against the live mirrors in October 2026: searches answer in 2–7
// seconds and sign-in in about 5. These limits leave room for a slow mirror
// while still catching one that has stopped answering, well inside the
// 60-second search deadline.
const (
	connectTimeout        = 10 * time.Second
	responseHeaderTimeout = 30 * time.Second
	bodyIdleTimeout       = 60 * time.Second
	doiMetadataTimeout    = 10 * time.Second
)

// newTransport is http.DefaultTransport with shorter limits, so a server
// that cannot be reached fails in seconds. Its header timeout also covers
// requests that do not go through send, such as sign-in.
func newTransport() http.RoundTripper {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: connectTimeout, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = connectTimeout
	transport.ResponseHeaderTimeout = responseHeaderTimeout
	return transport
}

// noResponseError is a server that went quiet. It is a timeout, so callers
// see REQUEST_TIMEOUT.
type noResponseError struct{ message string }

func (e *noResponseError) Error() string   { return e.message }
func (e *noResponseError) Timeout() bool   { return true }
func (e *noResponseError) Temporary() bool { return true }

// send bounds the wait for response headers, and each pause while the body
// is read, separately from the operation's deadline. A server that accepts
// the connection and then goes quiet fails in seconds instead of holding the
// whole operation, and the caller can tell that apart from its own deadline.
func (c *Client) send(client *http.Client, request *http.Request) (*http.Response, error) {
	ctx, cancel := context.WithCancel(request.Context())
	host := requestHost(request.URL.String())
	var quiet atomic.Bool
	timer := time.AfterFunc(c.headerTimeout, func() {
		quiet.Store(true)
		cancel()
	})
	response, err := client.Do(request.WithContext(ctx))
	if !timer.Stop() && quiet.Load() {
		if err == nil {
			response.Body.Close()
		}
		cancel()
		return nil, &noResponseError{fmt.Sprintf("%s did not answer within %s", host, c.headerTimeout)}
	}
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = newIdleBody(response.Body, cancel, c.idleTimeout, host)
	return response, nil
}

// idleBody fails a read once the body has sent nothing for limit, and ends
// the request when closed.
type idleBody struct {
	body    io.ReadCloser
	cancel  context.CancelFunc
	timer   *time.Timer
	limit   time.Duration
	host    string
	stalled atomic.Bool
}

func newIdleBody(body io.ReadCloser, cancel context.CancelFunc, limit time.Duration, host string) *idleBody {
	b := &idleBody{body: body, cancel: cancel, limit: limit, host: host}
	b.timer = time.AfterFunc(limit, func() {
		b.stalled.Store(true)
		cancel()
	})
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if b.stalled.Load() {
		return n, &noResponseError{fmt.Sprintf("%s stopped sending data for %s", b.host, b.limit)}
	}
	if n > 0 {
		b.timer.Reset(b.limit)
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	err := b.body.Close()
	b.cancel()
	return err
}
