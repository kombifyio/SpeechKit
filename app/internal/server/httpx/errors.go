//go:build linux

// Package httpx contains tiny cross-handler helpers for JSON error envelopes
// and status mapping. Keeping it in its own package prevents import cycles
// between core and mode packages.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// ErrorBody is the wire shape of every 4xx/5xx response from the SpeechKit
// server adapter. It is deliberately minimal so consumers (service-to-service
// callers, OSS integrators) can pin to a stable contract.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries the machine-readable code, the user-facing error class
// and the human-friendly message. Clients render their localized text from
// Class (limit, outage, right, consent, expired), never from Message.
// Additional context belongs in Details (a free-form map).
type ErrorDetail struct {
	Code    string         `json:"code"`
	Class   string         `json:"class,omitempty"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteError emits a standard JSON error envelope and sets the status code.
// It never panics; encoding failures degrade to an empty body.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteErrorWithDetails(w, status, code, message, nil)
}

// WriteErrorWithDetails is the same as WriteError but attaches a details map.
func WriteErrorWithDetails(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorBody{
		Error: ErrorDetail{Code: code, Class: ErrorClass(code), Message: message, Details: details},
	})
}

// ErrorClass is the user-facing class of an HTTP error. It follows the code
// alone (cascaded.FailureClass): a status says nothing about the reason, so a
// 403 budget, origin or binding error is never read as a missing right. An
// unclassed code gets no class and clients apply their own code rules.
func ErrorClass(code string) string {
	return string(cascaded.FailureClass(code))
}
