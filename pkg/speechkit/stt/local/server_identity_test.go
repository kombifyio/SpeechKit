package local

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// M8: whisper-server loads its model before it binds the port, so another
// process holding that port answers /health first. Readiness must wait for
// the listener that serves this start's nonce, never the squatter.
func TestWaitForReadyTrustsOnlyTheListenerServingItsNonce(t *testing.T) {
	id, err := newServerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(id.remove)

	squatter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(squatter.Close)
	p := New(Options{Port: 8080, ModelPath: "unused", GPU: "cpu"})
	p.BaseURL = squatter.URL
	p.identity = id
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if err := p.waitForReady(ctx); err == nil {
		t.Fatal("readiness accepted a listener that does not serve the nonce")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.Handle("/", http.FileServer(http.Dir(id.dir)))
	ours := httptest.NewServer(mux)
	t.Cleanup(ours.Close)
	p.BaseURL = ours.URL
	if err := p.waitForReady(context.Background()); err != nil {
		t.Fatalf("readiness refused the child serving its nonce: %v", err)
	}
}
