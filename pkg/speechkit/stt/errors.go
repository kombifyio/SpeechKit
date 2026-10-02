package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// ErrorKind classifies why an STT provider call failed, so embedders can
// decide between retrying, failing over, re-authenticating or surfacing the
// error without parsing messages.
type ErrorKind string

// Error kinds reported by [ProviderError].
const (
	// ErrorKindAuth means the credentials were missing, invalid or not
	// permitted (HTTP 401/403).
	ErrorKindAuth ErrorKind = "auth"
	// ErrorKindRateLimit means the provider throttled the request (HTTP 429).
	ErrorKindRateLimit ErrorKind = "rate_limit"
	// ErrorKindInvalidRequest means the provider rejected the request itself;
	// retrying the same request will not help.
	ErrorKindInvalidRequest ErrorKind = "invalid_request"
	// ErrorKindUnavailable means the provider failed or is temporarily down
	// (HTTP 5xx).
	ErrorKindUnavailable ErrorKind = "unavailable"
	// ErrorKindNetwork means the request never got a provider answer because
	// of a transport failure.
	ErrorKindNetwork ErrorKind = "network"
	// ErrorKindTimeout means the provider or the deadline ran out of time.
	ErrorKindTimeout ErrorKind = "timeout"
	// ErrorKindUnknown means the failure fits no other kind.
	ErrorKindUnknown ErrorKind = "unknown"
)

// Sentinel errors for use with [errors.Is]. A [ProviderError] matches the
// sentinel of its Kind.
var (
	// ErrAuth matches provider errors of [ErrorKindAuth].
	ErrAuth = errors.New("stt: provider authentication failed")
	// ErrRateLimited matches provider errors of [ErrorKindRateLimit].
	ErrRateLimited = errors.New("stt: provider rate limited")
	// ErrInvalidRequest matches provider errors of [ErrorKindInvalidRequest].
	ErrInvalidRequest = errors.New("stt: provider rejected request")
	// ErrProviderUnavailable matches provider errors of [ErrorKindUnavailable].
	ErrProviderUnavailable = errors.New("stt: provider unavailable")
)

// maxProviderErrorMessage bounds ProviderError.Message in bytes.
const maxProviderErrorMessage = 256

// ProviderError is the typed failure of one STT provider call. It carries
// no audio and no credentials; Message is a short, user-safe reason.
type ProviderError struct {
	// Provider is the provider name, for example "deepgram".
	Provider string
	// StatusCode is the HTTP status, or 0 for transport failures.
	StatusCode int
	// Kind classifies the failure.
	Kind ErrorKind
	// Retryable reports whether retrying the same request may succeed.
	Retryable bool
	// RetryAfter is the provider's requested back-off, or 0 when none was given.
	RetryAfter time.Duration
	// Message is a short, user-safe reason, truncated to 256 bytes.
	Message string
	// Err is the underlying cause, if any.
	Err error
}

// Error implements error. It names the provider, the status and the safe
// reason, never raw response bodies.
func (e *ProviderError) Error() string {
	provider := e.Provider
	if provider == "" {
		provider = "stt"
	}
	msg := e.Message
	if len(msg) > maxProviderErrorMessage {
		msg = msg[:maxProviderErrorMessage] + "..."
	}
	switch {
	case e.StatusCode != 0 && msg != "":
		return fmt.Sprintf("%s error (%d): %s", provider, e.StatusCode, msg)
	case e.StatusCode != 0:
		return fmt.Sprintf("%s error (%d)", provider, e.StatusCode)
	case msg != "" && e.Err != nil:
		return fmt.Sprintf("%s %s: %v", provider, msg, e.Err)
	case msg != "":
		return fmt.Sprintf("%s %s", provider, msg)
	case e.Err != nil:
		return fmt.Sprintf("%s: %v", provider, e.Err)
	default:
		return fmt.Sprintf("%s error (%s)", provider, e.Kind)
	}
}

// Unwrap returns the underlying cause.
func (e *ProviderError) Unwrap() error { return e.Err }

// Is reports whether target is the sentinel for e's Kind.
func (e *ProviderError) Is(target error) bool {
	switch target {
	case ErrAuth:
		return e.Kind == ErrorKindAuth
	case ErrRateLimited:
		return e.Kind == ErrorKindRateLimit
	case ErrInvalidRequest:
		return e.Kind == ErrorKindInvalidRequest
	case ErrProviderUnavailable:
		return e.Kind == ErrorKindUnavailable
	}
	return false
}

// HTTPError classifies a non-2xx provider response. resp supplies the status
// and Retry-After header (nil is tolerated); body is only inspected to derive
// a safe reason and is never copied into the error.
func HTTPError(provider string, resp *http.Response, body []byte) *ProviderError {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	e := &ProviderError{
		Provider:   provider,
		StatusCode: status,
		Kind:       ErrorKindUnknown,
		Message:    netsec.SafeProviderErrorReason(status, body),
	}
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		e.Kind = ErrorKindAuth
	case status == http.StatusTooManyRequests:
		e.Kind = ErrorKindRateLimit
		e.Retryable = true
		if resp != nil {
			e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
	case status == http.StatusRequestTimeout || status == http.StatusGatewayTimeout:
		e.Kind = ErrorKindTimeout
		e.Retryable = true
	case status >= 500:
		e.Kind = ErrorKindUnavailable
		e.Retryable = true
		if resp != nil && status == http.StatusServiceUnavailable {
			e.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
		}
	case status >= 400:
		// 400, 404, 413, 415, 422 and other client errors: the request itself
		// is at fault.
		e.Kind = ErrorKindInvalidRequest
	}
	if len(e.Message) > maxProviderErrorMessage {
		e.Message = e.Message[:maxProviderErrorMessage]
	}
	return e
}

// parseRetryAfter reads a Retry-After value in delta-seconds or HTTP-date form.
func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if at, err := http.ParseTime(v); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

// ClassifyTransportError wraps a failure from performing the HTTP request
// itself. Deadline expiry and timeouts become [ErrorKindTimeout], other
// network failures [ErrorKindNetwork]; both are retryable and keep err
// reachable through [errors.Unwrap]. Caller cancellation, nil and errors
// that are not transport failures are returned unchanged, so
// errors.Is(err, context.Canceled) keeps working.
func ClassifyTransportError(provider string, err error) error {
	// A *url.Error carries the full request URL; scrub query strings and
	// userinfo so a credential in the URL never reaches a log or the UI.
	err = netsec.RedactURLError(err)
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	var pe *ProviderError
	if errors.As(err, &pe) {
		return err
	}
	var netErr net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return &ProviderError{Provider: provider, Kind: ErrorKindTimeout, Retryable: true, Err: err}
	case errors.As(err, &netErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return &ProviderError{Provider: provider, Kind: ErrorKindNetwork, Retryable: true, Err: err}
	}
	return err
}
