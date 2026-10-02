//go:build linux

package wakewordtraining

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNew_RequiresStoreWhenEnabled(t *testing.T) {
	_, err := New(Options{AcceptUploads: true, AudioDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected error when AcceptUploads=true with nil Store")
	}
	if !strings.Contains(err.Error(), "Store") {
		t.Errorf("error %q does not mention Store", err)
	}
}

func TestNew_RequiresAudioDirWhenEnabled(t *testing.T) {
	_, err := New(Options{AcceptUploads: true, Store: newFakeStore()})
	if err == nil {
		t.Fatal("expected error when AcceptUploads=true with empty AudioDir")
	}
}

func TestNew_DisabledNeedsNoStoreOrDir(t *testing.T) {
	h, err := New(Options{AcceptUploads: false})
	if err != nil {
		t.Fatalf("expected no error when AcceptUploads=false: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestDisabled_Returns503ForAllRoutes(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: false})
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	cases := []struct {
		method, path string
	}{
		{http.MethodGet, "/v1/wakeword/activations"},
		{http.MethodPost, "/v1/wakeword/activations"},
		{http.MethodGet, "/v1/wakeword/activations/foo"},
		{http.MethodGet, "/v1/wakeword/activations/foo/audio"},
		{http.MethodPatch, "/v1/wakeword/activations/foo"},
		{http.MethodDelete, "/v1/wakeword/activations/foo"},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, http.NoBody)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", c.method, c.path, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status = %d, want 503", c.method, c.path, resp.StatusCode)
		}
	}
}

func TestUnsupportedMethod_Returns405(t *testing.T) {
	h, _ := testHandler(t, Options{AcceptUploads: true, Store: newFakeStore()})

	req := httptest.NewRequest(http.MethodPut, "/v1/wakeword/activations", http.NoBody)
	rec := httptest.NewRecorder()
	h.collection(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if rec.Header().Get("Allow") == "" {
		t.Error("expected Allow header")
	}
}
