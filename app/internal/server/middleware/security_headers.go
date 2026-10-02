//go:build linux

package middleware

import (
	"net/http"
	"strconv"
)

// DefaultContentSecurityPolicy is a strict policy suitable for the JSON API
// surface: nothing loads by default. HTML handlers that legitimately need
// inline assets (e.g. the admin sign-in page) override Content-Security-Policy
// on their own response with a per-page policy that hashes those assets.
const DefaultContentSecurityPolicy = "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

// SecurityHeadersOptions configures the SecurityHeaders middleware. Zero values
// fall back to safe defaults, so an empty struct yields a strict baseline.
type SecurityHeadersOptions struct {
	// ContentSecurityPolicy overrides DefaultContentSecurityPolicy.
	ContentSecurityPolicy string
	// FrameOptions overrides the X-Frame-Options value (default "DENY").
	FrameOptions string
	// ReferrerPolicy overrides the Referrer-Policy value (default "no-referrer").
	ReferrerPolicy string
	// HSTS emits Strict-Transport-Security. Only enable behind TLS termination;
	// it is meaningless (and ignored by browsers) over plain HTTP.
	HSTS bool
	// HSTSMaxAgeSeconds is the HSTS max-age; defaults to two years when HSTS is on.
	HSTSMaxAgeSeconds int
}

// SecurityHeaders returns a middleware that stamps baseline security response
// headers on every response: a strict Content-Security-Policy, X-Frame-Options,
// X-Content-Type-Options, Referrer-Policy, and optional HSTS.
//
// Headers are set before the inner handler runs, so a handler may override any
// of them for its own response (the admin sign-in HTML page replaces CSP with a
// per-page hashed policy). This keeps the API surface locked down by default
// while letting the rare HTML response opt into exactly what it needs.
func SecurityHeaders(opts SecurityHeadersOptions) Middleware {
	csp := opts.ContentSecurityPolicy
	if csp == "" {
		csp = DefaultContentSecurityPolicy
	}
	frame := opts.FrameOptions
	if frame == "" {
		frame = "DENY"
	}
	referrer := opts.ReferrerPolicy
	if referrer == "" {
		referrer = "no-referrer"
	}
	var hsts string
	if opts.HSTS {
		age := opts.HSTSMaxAgeSeconds
		if age <= 0 {
			age = 63072000 // 2 years
		}
		hsts = "max-age=" + strconv.Itoa(age) + "; includeSubDomains"
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", csp)
			h.Set("X-Frame-Options", frame)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", referrer)
			if hsts != "" {
				h.Set("Strict-Transport-Security", hsts)
			}
			next.ServeHTTP(w, r)
		})
	}
}
