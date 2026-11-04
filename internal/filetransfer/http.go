package filetransfer

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

func validateURL(raw string, hosted bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return &Error{Code: "INVALID_ARGUMENT", Reason: "artifact URL must be public HTTPS without credentials"}
	}
	if hosted && (u.Host != "content.openalex.org" || !strings.HasPrefix(u.Path, "/works/")) {
		return &Error{Code: "INVALID_ARGUMENT", Reason: "hosted content URL must use the official OpenAlex content host"}
	}
	if u.Hostname() == "localhost" || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return &Error{Code: "INVALID_ARGUMENT", Reason: "artifact URL must not address a local network"}
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return &Error{Code: "INVALID_ARGUMENT", Reason: "artifact URL must not address a private network"}
	}
	return nil
}

func newHTTPClient(custom *http.Client, key string) *http.Client {
	var client http.Client
	if custom != nil {
		client = *custom
	} else {
		client.Transport = &http.Transport{
			DialContext:           publicDial,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		}
	}
	client.Jar = nil
	if client.Timeout == 0 {
		client.Timeout = 30 * time.Minute
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many artifact redirects")
		}
		if err := validateURL(req.URL.String(), false); err != nil {
			return err
		}
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		if req.URL.Host == "content.openalex.org" && via[0].Header.Get("Authorization") != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		return nil
	}
	return &client
}

// Resolve and pin a public address on each new connection. Validating only the
// URL or a prior DNS response would leave redirects and DNS rebinding unchecked.
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range addresses {
		if !publicIP(ip) {
			return nil, errors.New("artifact destination resolved to a private network")
		}
	}
	dialer := net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	var dialErr error
	for _, ip := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		dialErr = err
	}
	if dialErr == nil {
		dialErr = errors.New("artifact host has no public addresses")
	}
	return nil, dialErr
}

var reservedNetworks = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range reservedNetworks {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}
