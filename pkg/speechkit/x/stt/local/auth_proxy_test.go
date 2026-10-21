package local

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

func startTestProxy(t *testing.T) (*authProxy, *httptest.Server) {
	t.Helper()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(upstream.Close)
	u, _ := url.Parse(upstream.URL)
	_, portStr, _ := net.SplitHostPort(u.Host)
	port, _ := strconv.Atoi(portStr)
	proxy, err := startAuthProxy(context.Background(), 0, port)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(proxy.close)
	return proxy, upstream
}

func proxyGet(t *testing.T, proxy *authProxy, key, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, proxy.baseURL+"/health", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// The whisper port refuses a request without the per-start key and a browser
// request even with it; the keyed request reaches the child.
func TestWhisperProxyRefusesUnkeyedRequests(t *testing.T) {
	proxy, _ := startTestProxy(t)

	if got := proxyGet(t, proxy, "", "").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("unkeyed status = %d, want 401", got)
	}
	if got := proxyGet(t, proxy, "wrong", "").StatusCode; got != http.StatusUnauthorized {
		t.Fatalf("wrong key status = %d, want 401", got)
	}
	if got := proxyGet(t, proxy, proxy.key, "https://evil.example").StatusCode; got != http.StatusForbidden {
		t.Fatalf("browser-origin status = %d, want 403", got)
	}
	if got := proxyGet(t, proxy, proxy.key, "").StatusCode; got != http.StatusOK {
		t.Fatalf("keyed status = %d, want 200", got)
	}
}

// whisper-server answers every origin; the proxy must not pass that on.
func TestWhisperProxyStripsCORS(t *testing.T) {
	proxy, _ := startTestProxy(t)
	resp := proxyGet(t, proxy, proxy.key, "")
	if v := resp.Header.Get("Access-Control-Allow-Origin"); v != "" {
		t.Fatalf("CORS header leaked through the proxy: %q", v)
	}
}
