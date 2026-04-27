//go:build linux

// Package middleware provides HTTP middleware primitives for the SpeechKit
// server adapter. Each middleware is a stand-alone decorator so tests can
// compose them individually.
package middleware

import "net/http"

// Middleware is the standard HTTP decorator signature.
type Middleware func(http.Handler) http.Handler

// Chain composes middlewares left-to-right: the first argument is the
// outermost wrapper (runs first on request, last on response).
func Chain(mws ...Middleware) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		// Walk in reverse so the first argument ends up outermost.
		h := next
		for i := len(mws) - 1; i >= 0; i-- {
			if mws[i] == nil {
				continue
			}
			h = mws[i](h)
		}
		return h
	}
}
