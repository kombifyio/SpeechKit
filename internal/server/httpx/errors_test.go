//go:build linux

package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteErrorEmitsJSONEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteError(rec, http.StatusBadRequest, "invalid_request", "bad input")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q, want application/json", got)
	}
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error.Code != "invalid_request" || body.Error.Message != "bad input" {
		t.Fatalf("error body = %#v", body.Error)
	}
	if body.Error.Details != nil {
		t.Fatalf("details should be omitted, got %#v", body.Error.Details)
	}
}

func TestWriteErrorWithDetailsIncludesDetails(t *testing.T) {
	rec := httptest.NewRecorder()

	WriteErrorWithDetails(rec, http.StatusTooManyRequests, "rate_limited", "try later", map[string]any{
		"retry_after_seconds": float64(5),
	})

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got := body.Error.Details["retry_after_seconds"]; got != float64(5) {
		t.Fatalf("retry detail = %#v, want 5", got)
	}
}
