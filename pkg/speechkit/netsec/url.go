// Package netsec provides centralized network security primitives used by
// every HTTP-based provider in SpeechKit (STT, TTS, LLM, downloads).
//
// It addresses the audit findings S1-S5 (2026-04-16):
//   - SSRF via unvalidated user-supplied BaseURL configuration
//   - Missing TLS hardening on HTTP clients
//   - Bearer tokens leaking through default loggers
//
// All HTTP provider constructors MUST route user-supplied URLs through
// ValidateProviderURL and MUST obtain their *http.Client via NewSafeHTTPClient.
package netsec

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidationOptions controls how strict URL validation behaves.
// The zero value is the safe default: HTTPS-only, no loopback, no private IPs.
type ValidationOptions struct {
	// AllowLoopback permits http:// and https:// URLs whose host is a
	// literal loopback address (127.0.0.0/8, ::1, or the name "localhost").
	// Enable for local whisper-server / Ollama / dev endpoints.
	AllowLoopback bool

	// AllowPrivate permits URLs whose host is in RFC1918 / RFC6598 /
	// link-local / unique-local IPv6 ranges. Enable only for self-hosted
	// VPS scenarios where the user explicitly opts in.
	AllowPrivate bool

	// AllowHTTP permits http:// for non-loopback hosts. Leave false unless
	// an operator has a documented reason (e.g. test harness).
	AllowHTTP bool
}

// Validation errors. Callers can match on these with errors.Is for UX.
var (
	ErrEmptyURL          = errors.New("netsec: empty URL")
	ErrInvalidURL        = errors.New("netsec: URL could not be parsed")
	ErrMissingScheme     = errors.New("netsec: URL must have a scheme")
	ErrMissingHost       = errors.New("netsec: URL must have a host")
	ErrUnsupportedScheme = errors.New("netsec: scheme must be http or https")
	ErrInsecureHTTP      = errors.New("netsec: plain http:// not allowed for this host")
	ErrLoopbackBlocked   = errors.New("netsec: loopback addresses not allowed")
	ErrPrivateBlocked    = errors.New("netsec: private / link-local / ULA addresses not allowed")
	ErrInvalidHost       = errors.New("netsec: URL host could not be resolved as a literal IP or name")
	ErrUserInfoForbidden = errors.New("netsec: URL user-info (user:pass@) is not permitted")
)

// ValidateProviderURL parses raw and rejects URLs that would expose the
// caller to SSRF. It does not make network calls — hostnames that are
// not IP literals are checked by NewSafeHTTPClient when DialValidation is set.
// Provider, download and update clients should combine URL validation with
// resolve-time dial validation.
func ValidateProviderURL(raw string, opts ValidationOptions) error {
	if strings.TrimSpace(raw) == "" {
		return ErrEmptyURL
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}

	if u.Scheme == "" {
		return ErrMissingScheme
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: got %q", ErrUnsupportedScheme, u.Scheme)
	}

	if u.Host == "" {
		return ErrMissingHost
	}

	// user:pass@host is almost always a misconfiguration or phishing vector.
	if u.User != nil {
		return ErrUserInfoForbidden
	}

	host := u.Hostname()
	if host == "" {
		return ErrMissingHost
	}

	ip := net.ParseIP(host)
	isLoopback := ip != nil && ip.IsLoopback()
	if strings.EqualFold(host, "localhost") {
		isLoopback = true
	}

	switch {
	case isLoopback:
		if !opts.AllowLoopback {
			return fmt.Errorf("%w: host=%s", ErrLoopbackBlocked, host)
		}
		// Loopback + http is fine; loopback + https is fine too.
		return nil
	case ip != nil:
		if isPrivateIP(ip) && !opts.AllowPrivate {
			return fmt.Errorf("%w: host=%s", ErrPrivateBlocked, host)
		}
	}

	// Non-loopback: enforce https unless AllowHTTP is set.
	if scheme == "http" && !opts.AllowHTTP {
		return fmt.Errorf("%w: host=%s", ErrInsecureHTTP, host)
	}

	return nil
}

// ValidateResolvedIP applies SSRF range restrictions to a concrete IP address
// returned by DNS resolution or visible on a dial target.
func ValidateResolvedIP(ip net.IP, opts ValidationOptions) error {
	if ip == nil {
		return ErrInvalidHost
	}
	if ip.IsLoopback() {
		if !opts.AllowLoopback {
			return fmt.Errorf("%w: ip=%s", ErrLoopbackBlocked, ip.String())
		}
		return nil
	}
	if isPrivateIP(ip) && !opts.AllowPrivate {
		return fmt.Errorf("%w: ip=%s", ErrPrivateBlocked, ip.String())
	}
	return nil
}

// isPrivateIP returns true for RFC1918, RFC6598, link-local, ULA and
// unspecified addresses. Loopback is handled separately by the caller.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	// RFC6598 CGNAT range 100.64.0.0/10
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1]&0xC0 == 64 {
			return true
		}
	}
	return false
}

// BuildEndpoint validates baseURL and joins path safely. It prevents a
// malicious baseURL like "https://legit.example.com/.." from escaping
// the host, and normalises trailing slashes.
func BuildEndpoint(baseURL, path string, opts ValidationOptions) (string, error) {
	if err := ValidateProviderURL(baseURL, opts); err != nil {
		return "", err
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	// Reject baseURL containing ".." path segments — they're never legitimate
	// in a provider config and may indicate tampering.
	if strings.Contains(u.Path, "..") {
		return "", fmt.Errorf("%w: baseURL path must not contain '..'", ErrInvalidURL)
	}

	// Normalise: strip trailing slash on base, leading slash on path.
	base := strings.TrimRight(u.Path, "/")
	rel := strings.TrimLeft(path, "/")

	u.Path = base
	if rel != "" {
		u.Path = base + "/" + rel
	}
	// RawQuery/Fragment on base are preserved only when path is empty.
	if rel != "" {
		u.RawQuery = ""
		u.Fragment = ""
	}
	return u.String(), nil
}
