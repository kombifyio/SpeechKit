package netsec

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Headers in this set are logged as the literal string "[REDACTED]" when
// callers use RedactHeaders or the RedactedHeadersKey context value.
var sensitiveHeaders = map[string]struct{}{
	"authorization":        {},
	"proxy-authorization":  {},
	"x-api-key":            {},
	"x-auth-token":         {},
	"x-amz-security-token": {},
	"cookie":               {},
	"set-cookie":           {},
	"x-goog-api-key":       {},
}

// ClientOptions configures a SpeechKit HTTP client.
type ClientOptions struct {
	// Timeout is the per-request timeout. Zero disables it (discouraged).
	Timeout time.Duration

	// TLSMinVersion pins the minimum TLS version (default: tls.VersionTLS12).
	TLSMinVersion uint16

	// InnerTransport, if non-nil, is wrapped by the RedactingRoundTripper.
	// Normally nil — the function constructs a hardened *http.Transport.
	InnerTransport http.RoundTripper

	// DisableKeepAlives turns off connection reuse. Leave false for best perf.
	DisableKeepAlives bool

	// DialValidation, when non-nil, validates the actual resolved target IP
	// before opening a TCP connection. Use this for any client that might reach
	// user-configurable provider, model download, or update URLs.
	DialValidation *ValidationOptions

	// SetupTraffic marks a client that carries only setup traffic (model
	// downloads and update checks; no audio, transcripts, prompts or
	// credentials). While [Policy.AllowSetupTraffic] is true such a client is
	// not bound by [Policy.Validation]; DialValidation still applies.
	SetupTraffic bool
}

// NewSafeHTTPClient is the original name of [NewClient] and behaves the same.
func NewSafeHTTPClient(opts ClientOptions) *http.Client {
	return NewClient(opts)
}

// NewClient returns an *http.Client with explicit TLS 1.2+ minimum,
// sensible timeouts, dial-target validation, and a transport wrapper that
// stores redacted request headers in context for logging paths that opt in.
//
// Every dial is checked at dial time against the process-wide [Policy]
// installed by [Configure] (so a client built before Configure is still
// bound by it) and, when set, against opts.DialValidation as well; both must
// pass. While either is active the environment proxy is ignored, because a
// proxy resolves the target outside this process.
//
// The client is safe to share across goroutines.
func NewClient(opts ClientOptions) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	minTLS := opts.TLSMinVersion
	if minTLS == 0 {
		minTLS = tls.VersionTLS12
	}
	// The pointer is kept, not copied: providers adjust their Validation field
	// after construction and the dial must see the current value.
	local := opts.DialValidation

	inner := opts.InnerTransport
	if inner == nil {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}
		// Environment proxies resolve the target host outside this process,
		// which would bypass resolve-time IP validation, so a client with its
		// own validation never uses one; the rest ignore it only while the
		// process-wide policy is active.
		var proxy func(*http.Request) (*url.URL, error)
		if local == nil {
			proxy = func(req *http.Request) (*url.URL, error) {
				if len(activeValidations(nil, opts.SetupTraffic)) > 0 {
					return nil, nil
				}
				return http.ProxyFromEnvironment(req)
			}
		}
		inner = &http.Transport{
			Proxy:                 proxy,
			DialContext:           guardedDialContext(dialer, local, opts.SetupTraffic),
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     opts.DisableKeepAlives,
			TLSClientConfig: &tls.Config{ //nolint:gosec // G402: minTLS default is tls.VersionTLS12, validated above
				MinVersion: minTLS,
			},
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: &RedactingRoundTripper{Base: inner},
		// Re-validate every redirect hop with the active option sets. Dial-time
		// IP validation already guards each connection, but rejecting the
		// redirect URL up front keeps http->https downgrades, userinfo
		// smuggling, and public-host redirects out of the request path.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("netsec: stopped after 10 redirects")
			}
			for _, v := range activeValidations(local, opts.SetupTraffic) {
				if err := ValidateProviderURL(req.URL.String(), v); err != nil {
					return fmt.Errorf("netsec: redirect target rejected: %w", err)
				}
			}
			return nil
		},
	}
}

// activeValidations lists the option sets a dial or redirect must satisfy:
// the process-wide policy first, then the caller's own.
func activeValidations(local *ValidationOptions, setupTraffic bool) []ValidationOptions {
	var out []ValidationOptions
	pol := currentPolicy()
	if g := pol.Validation; g != nil && (!setupTraffic || !pol.AllowSetupTraffic) {
		out = append(out, *g)
	}
	if local != nil {
		out = append(out, *local)
	}
	return out
}

// guardedDialContext validates the actual resolved target IPs against every
// active option set before opening a connection. With no option set active it
// is a plain dial.
func guardedDialContext(dialer *net.Dialer, local *ValidationOptions, setupTraffic bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		validations := activeValidations(local, setupTraffic)
		if len(validations) == 0 {
			return dialer.DialContext(ctx, network, address)
		}
		check := func(ip net.IP) error {
			for _, v := range validations {
				if err := ValidateResolvedIP(ip, v); err != nil {
					return err
				}
			}
			return nil
		}

		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, fmt.Errorf("netsec: split dial address %q: %w", address, err)
		}

		if ip := net.ParseIP(host); ip != nil {
			if err := check(ip); err != nil {
				return nil, fmt.Errorf("netsec: dial target %s: %w", address, err)
			}
			return dialer.DialContext(ctx, network, address)
		}

		resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("netsec: resolve %s: %w", host, err)
		}
		if len(resolved) == 0 {
			return nil, fmt.Errorf("%w: host=%s", ErrInvalidHost, host)
		}

		for _, addr := range resolved {
			if err := check(addr.IP); err != nil {
				return nil, fmt.Errorf("netsec: resolved %s to %s: %w", host, addr.String(), err)
			}
		}

		var lastErr error
		for _, addr := range resolved {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, fmt.Errorf("netsec: dial %s: %w", address, lastErr)
	}
}

// RedactingRoundTripper is an http.RoundTripper that records redacted
// copies of the outgoing request header set on the *http.Request context
// so that downstream logging middleware can inspect non-sensitive headers
// only.
//
// It does NOT rewrite the outgoing request — the real Authorization
// header is sent over TLS as expected. Redaction applies only to what
// observability paths can access via ctx.Value(RedactedHeadersKey).
type RedactingRoundTripper struct {
	Base http.RoundTripper
}

// ctxKey is an unexported type to prevent collisions.
type ctxKey int

// RedactedHeadersKey is the context key used to store redacted headers.
const RedactedHeadersKey ctxKey = 1

// RoundTrip implements http.RoundTripper.
func (r *RedactingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	req = req.WithContext(context.WithValue(req.Context(), RedactedHeadersKey, RedactHeaders(req.Header)))
	return base.RoundTrip(req)
}

// RedactHeaders returns a copy of h where sensitive header values are
// replaced with the literal "[REDACTED]". Safe for logging.
func RedactHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vs := range h {
		lk := strings.ToLower(k)
		if _, sensitive := sensitiveHeaders[lk]; sensitive {
			redacted := make([]string, len(vs))
			for i := range vs {
				redacted[i] = "[REDACTED]"
			}
			out[k] = redacted
			continue
		}
		copyVs := make([]string, len(vs))
		copy(copyVs, vs)
		out[k] = copyVs
	}
	return out
}

// RedactBearer returns a safe rendering of an "Authorization: Bearer xxx"
// header value for logs. It always returns "Bearer [REDACTED]" when the
// input is non-empty, and "" otherwise.
func RedactBearer(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	return "Bearer [REDACTED]"
}
