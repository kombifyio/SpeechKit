//go:build linux

package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrustedProxiesRequestIsHTTPSRequiresTrustedRemote(t *testing.T) {
	proxies, err := NewTrustedProxies([]string{"203.0.113.0/24"})
	if err != nil {
		t.Fatalf("NewTrustedProxies: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("X-Forwarded-Proto", "https")
	if proxies.RequestIsHTTPS(req) {
		t.Fatal("untrusted remote must not be able to mark request as HTTPS")
	}

	req.RemoteAddr = "203.0.113.10:4321"
	if !proxies.RequestIsHTTPS(req) {
		t.Fatal("configured trusted proxy should be able to mark request as HTTPS")
	}
}

func TestTrustedProxiesTrustsLoopbackForwardedProto(t *testing.T) {
	proxies, err := NewTrustedProxies(nil)
	if err != nil {
		t.Fatalf("NewTrustedProxies: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:4321"
	req.Header.Set("X-Forwarded-Proto", "https")
	if !proxies.RequestIsHTTPS(req) {
		t.Fatal("loopback reverse proxy should be trusted for local development")
	}
}

func TestNewTrustedProxiesRejectsInvalidCIDR(t *testing.T) {
	if _, err := NewTrustedProxies([]string{"not-a-cidr"}); err == nil {
		t.Fatal("expected invalid CIDR error")
	}
}
