//go:build linux

package vocabulary

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// fakeDictStore is a controllable store.UserDictionaryStore for handler tests.
type fakeDictStore struct {
	entries     []store.UserDictionaryEntry
	listErr     error
	replaceErr  error
	listCalls   int
	lastLang    string
	lastEntries []store.UserDictionaryEntry
	replaced    bool
}

func (f *fakeDictStore) ReplaceUserDictionaryEntries(_ context.Context, language string, entries []store.UserDictionaryEntry) error {
	f.replaced = true
	f.lastLang = language
	f.lastEntries = entries
	if f.replaceErr != nil {
		return f.replaceErr
	}
	f.entries = entries
	return nil
}

func (f *fakeDictStore) ListUserDictionaryEntries(_ context.Context, _ string) ([]store.UserDictionaryEntry, error) {
	f.listCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.entries, nil
}

func (f *fakeDictStore) RecordUserDictionaryUsage(_ context.Context, _, _ string) error { return nil }

func withRole(r *http.Request, role string) *http.Request {
	return r.WithContext(middleware.InjectIdentityForTest(r.Context(),
		middleware.Identity{UserID: "u1", OrgID: "o1", Role: role}))
}

func TestDictionary_StoreUnavailable(t *testing.T) {
	h := New(nil)
	rec := httptest.NewRecorder()
	h.dictionary(rec, httptest.NewRequest(http.MethodGet, "/v1/vocabulary/dictionary", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestDictionary_GET_ListsEntries(t *testing.T) {
	fs := &fakeDictStore{entries: []store.UserDictionaryEntry{
		{Spoken: "kombify", Canonical: "kombify", Language: "en", Enabled: true},
	}}
	h := New(fs)

	rec := httptest.NewRecorder()
	h.dictionary(rec, httptest.NewRequest(http.MethodGet, "/v1/vocabulary/dictionary?language=en", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Entries []store.UserDictionaryEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) != 1 || body.Entries[0].Canonical != "kombify" {
		t.Fatalf("unexpected entries: %+v", body.Entries)
	}
}

func TestDictionary_GET_StoreError(t *testing.T) {
	h := New(&fakeDictStore{listErr: errors.New("db down")})
	rec := httptest.NewRecorder()
	h.dictionary(rec, httptest.NewRequest(http.MethodGet, "/v1/vocabulary/dictionary", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestDictionary_GET_RejectsInvalidScope(t *testing.T) {
	for _, scope := range []string{"bogus", "tenant", "", "%20org%20"} {
		t.Run(scope, func(t *testing.T) {
			fs := &fakeDictStore{}
			h := New(fs)
			rec := httptest.NewRecorder()

			h.dictionary(rec, httptest.NewRequest(http.MethodGet, "/v1/vocabulary/dictionary?scope="+scope, nil))

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			if fs.listCalls != 0 {
				t.Fatalf("store was called %d times for invalid scope", fs.listCalls)
			}
		})
	}
}

func TestDictionary_POST_RequiresAdmin(t *testing.T) {
	fs := &fakeDictStore{}
	h := New(fs)

	body := `{"language":"en","entries":[{"spoken":"kombify","canonical":"kombify"}]}`
	r := withRole(httptest.NewRequest(http.MethodPost, "/v1/vocabulary/dictionary", strings.NewReader(body)), "")
	rec := httptest.NewRecorder()
	h.dictionary(rec, r)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST status = %d, want 403", rec.Code)
	}
	if fs.replaced {
		t.Fatal("non-admin POST must not reach the store")
	}
}

func TestDictionary_POST_AdminReplaces(t *testing.T) {
	fs := &fakeDictStore{}
	h := New(fs)

	body := `{"language":"de","entries":[{"spoken":"kombify","canonical":"kombify"}]}`
	r := withRole(httptest.NewRequest(http.MethodPost, "/v1/vocabulary/dictionary", strings.NewReader(body)), "admin")
	rec := httptest.NewRecorder()
	h.dictionary(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("admin POST status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !fs.replaced || fs.lastLang != "de" || len(fs.lastEntries) != 1 {
		t.Fatalf("store not updated correctly: replaced=%v lang=%q entries=%+v", fs.replaced, fs.lastLang, fs.lastEntries)
	}
}

func TestDictionary_POST_InvalidJSON(t *testing.T) {
	h := New(&fakeDictStore{})
	r := withRole(httptest.NewRequest(http.MethodPost, "/v1/vocabulary/dictionary", strings.NewReader("{not json")), "admin")
	rec := httptest.NewRecorder()
	h.dictionary(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestDictionary_MethodNotAllowed(t *testing.T) {
	h := New(&fakeDictStore{})
	rec := httptest.NewRecorder()
	h.dictionary(rec, httptest.NewRequest(http.MethodDelete, "/v1/vocabulary/dictionary", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) || !strings.Contains(allow, http.MethodPost) {
		t.Errorf("Allow header = %q, want GET+POST", allow)
	}
}
