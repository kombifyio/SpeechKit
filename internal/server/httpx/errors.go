//go:build linux

// Package httpx contains tiny cross-handler helpers for JSON error envelopes
// and status mapping. Keeping it in its own package prevents import cycles
// between core and mode packages.
package httpx

import (
	"encoding/json"
	"net/http"
)

// ErrorBody is the wire shape of every 4xx/5xx response from the SpeechKit
// server adapter. It is deliberately minimal so consumers (kombify-AI, OSS
// integrators) can pin to a stable contract.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries the machine-readable code and the human-friendly
// message. Additional context belongs in Details (a free-form map).
type ErrorDetail struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteError emits a standard JSON error envelope and sets the status code.
// It never panics; encoding failures degrade to an empty body.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorBody{
		Error: ErrorDetail{Code: code, Message: message},
	})
}

// WriteErrorWithDetails is the same as WriteError but attaches a details map.
func WriteErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorBody{
		Error: ErrorDetail{Code: code, Message: message, Details: details},
	})
}
