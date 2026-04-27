package serverclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ServerError is the typed representation of the server's standard
// {"error": {"code", "message", "details"}} envelope. Mode adapters return
// it (wrapped as %w) so callers can use errors.As to introspect both the
// HTTP status and the machine-readable error code without re-parsing the
// HTTP response.
type ServerError struct {
	Status  int            // HTTP status code from the response
	Code    string         // Server-side error code (e.g. "provider_unavailable")
	Message string         // Human-friendly message from the envelope
	Details map[string]any // Optional details; nil when the envelope omits them
}

// Error formats the envelope for log/diagnostic use. The format is stable
// across the v1 surface so log greps stay durable.
func (e *ServerError) Error() string {
	if e == nil {
		return "<nil ServerError>"
	}
	if e.Message != "" {
		return fmt.Sprintf("serverclient: HTTP %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("serverclient: HTTP %d %s", e.Status, e.Code)
}

// IsCode reports whether the error envelope carries the given code. Useful
// for callers that want to branch on, say, "provider_unavailable" without a
// type assertion. Returns false if err is not (or does not wrap) a
// *ServerError.
func IsCode(err error, code string) bool {
	var se *ServerError
	if errors.As(err, &se) && se != nil {
		return se.Code == code
	}
	return false
}

// errorEnvelope is the wire shape we decode into. Kept private; callers see
// *ServerError instead.
type errorEnvelope struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	} `json:"error"`
}

// decodeErrorEnvelope reads up to maxBody bytes from resp.Body and tries to
// decode the standard envelope. If the body is not JSON or doesn't carry an
// envelope, returns a *ServerError with an empty Code and the raw body
// (truncated) as Message — so 502 HTML pages from a load balancer still
// surface usefully.
func decodeErrorEnvelope(resp *http.Response, maxBody int64) *ServerError {
	if resp == nil {
		return &ServerError{Status: 0, Code: "no_response", Message: "no response from server"}
	}
	se := &ServerError{Status: resp.StatusCode}
	if maxBody <= 0 {
		maxBody = 64 << 10
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if len(body) == 0 {
		se.Message = http.StatusText(resp.StatusCode)
		return se
	}

	var env errorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && (env.Error.Code != "" || env.Error.Message != "") {
		se.Code = env.Error.Code
		se.Message = env.Error.Message
		se.Details = env.Error.Details
		return se
	}

	// Fall through: body is not the standard envelope. Surface a truncated
	// raw body so debugging isn't blind. Cap at 256 bytes for log hygiene.
	const rawCap = 256
	if len(body) > rawCap {
		body = append(body[:rawCap-3], '.', '.', '.')
	}
	se.Message = string(body)
	return se
}
