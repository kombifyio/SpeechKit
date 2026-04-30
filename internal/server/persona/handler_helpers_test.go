//go:build linux

package persona

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIDFromPathAcceptsOnlyExactItemPaths(t *testing.T) {
	tests := map[string]string{
		"/v1/personas/assistant":       "assistant",
		"/v1/personas/":                "",
		"/v1/personas/assistant/tools": "",
	}
	for path, want := range tests {
		if got := idFromPath(path, "/v1/personas/"); got != want {
			t.Fatalf("idFromPath(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestMethodNotAllowedSetsAllowHeaderAndEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()

	methodNotAllowed(rec, http.MethodGet, http.MethodPost)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
	if got := rec.Header().Get("Allow"); got != "GET, POST" {
		t.Fatalf("Allow header = %q", got)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"code":"method_not_allowed"`) {
		t.Fatalf("body should contain method_not_allowed envelope, got %s", body)
	}
}

func TestDecodeJSONRejectsMissingAndUnknownFields(t *testing.T) {
	rec := httptest.NewRecorder()
	if decodeJSON(rec, httptest.NewRequest(http.MethodPost, "/v1/personas", nil), &Persona{}) {
		t.Fatal("decodeJSON should reject missing bodies")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing body status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/personas", strings.NewReader(`{"id":"p","unknown":true}`))
	if decodeJSON(rec, req, &Persona{}) {
		t.Fatal("decodeJSON should reject unknown fields")
	}
	if body := rec.Body.String(); !strings.Contains(body, "unknown field") {
		t.Fatalf("body should mention unknown field, got %s", body)
	}
}

func TestRequireWriteRejectsReadOnlyAndNonAdminRequests(t *testing.T) {
	handler := &Handler{allowWrites: false}
	rec := httptest.NewRecorder()
	if handler.requireWrite(rec, httptest.NewRequest(http.MethodPost, "/v1/personas", nil)) {
		t.Fatal("read-only handler should reject writes")
	}
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("read-only status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}

	handler.allowWrites = true
	rec = httptest.NewRecorder()
	if handler.requireWrite(rec, httptest.NewRequest(http.MethodPost, "/v1/personas", nil)) {
		t.Fatal("non-admin request should reject writes")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestWritePersonaErrorsMapPersistenceToServerError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeUpsertError(rec, "invalid_persona", errors.Join(ErrPersist, errors.New("disk full")))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("persist upsert status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	rec = httptest.NewRecorder()
	writeDeleteError(rec, "persona_not_found", errors.New("missing"))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
