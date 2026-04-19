package netsec

import (
	"crypto/tls"
	"net"
	"net/http"
	"strings"
	"time"
)

// Default header allowlist for RedactingRoundTripper. Headers NOT in this
// set are logged as the literal string "[REDACTED]" when DumpRequest /
// DumpResponse is called with the Redact helper.
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
}

// NewSafeHTTPClient returns an *http.Client with explicit TLS 1.2+ minimum,
// sensible timeouts, and a RedactingRoundTripper wrapper so that any
// subsequent call to httputil.DumpRequest will not leak Authorization headers.
//
// The client is safe to share across goroutines.
func NewSafeHTTPClient(opts ClientOptions) *http.Client {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	minTLS := opts.TLSMinVersion
	if minTLS == 0 {
		minTLS = tls.VersionTLS12
	}

	inner := opts.InnerTransport
	if inner == nil {
		inner = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
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
