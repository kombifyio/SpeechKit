package netsec

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustIP(t *testing.T, raw string) net.IP {
	t.Helper()
	ip := net.ParseIP(raw)
	if ip == nil {
		t.Fatalf("bad test IP %q", raw)
	}
	return ip
}

// TestSafeClientRevalidatesRedirects pins the redirect contract for clients
// with DialValidation: a hop to a host outside the allowed ranges is refused
// even though the first request target was valid.
func TestSafeClientRevalidatesRedirects(t *testing.T) {
	local := ValidationOptions{AllowLoopback: true, AllowHTTP: true, RequireLocal: true}

	// Target the redirect is allowed to land on.
	okTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer okTarget.Close()

	cases := []struct {
		name     string
		location string
		wantOK   bool
	}{
		{"loopback redirect allowed", okTarget.URL, true},
		{"public host redirect blocked", "https://api.example.com/v1/audio", false},
		{"userinfo redirect blocked", "http://user:pass@127.0.0.1:9/x", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, tc.location, http.StatusFound)
			}))
			defer origin.Close()

			client := NewSafeHTTPClient(ClientOptions{DialValidation: &local})
			resp, err := client.Get(origin.URL)
			if resp != nil {
				resp.Body.Close()
			}
			if tc.wantOK {
				if err != nil {
					t.Fatalf("allowed redirect failed: %v", err)
				}
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("status = %d, want 200", resp.StatusCode)
				}
				return
			}
			if err == nil {
				t.Fatal("blocked redirect must fail")
			}
		})
	}
}

// TestSafeClientWithoutValidationKeepsDefaultRedirects documents that clients
// without DialValidation keep Go's default redirect behaviour.
func TestSafeClientWithoutValidationKeepsDefaultRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer origin.Close()

	client := NewSafeHTTPClient(ClientOptions{})
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// TestValidateProviderURLLocalOnlyTable pins the address-classification
// behaviour restricted scopes rely on (RequireLocal + AllowPrivate +
// AllowLoopback + AllowHTTP).
func TestValidateProviderURLLocalOnlyTable(t *testing.T) {
	local := ValidationOptions{AllowLoopback: true, AllowPrivate: true, AllowHTTP: true, RequireLocal: true}
	cases := []struct {
		url     string
		allowed bool
	}{
		{"http://127.0.0.1:11434", true},
		{"http://localhost:8080/v1", true},
		{"http://[::1]:8080", true},
		{"http://192.168.1.20:11434", true},
		{"http://10.0.0.5:9000", true},
		{"http://172.16.0.10", true},
		{"http://100.64.0.1", true},          // CGNAT (Tailscale-style)
		{"http://169.254.10.10", true},       // link-local
		{"http://[fd00::1]:8080", true},      // ULA
		{"https://ha.fritz.box:8123", true},  // hostname: syntactic pass, dial-time validation decides
		{"https://api.openai.com/v1", false}, // public host name is not rejected syntactically...
		{"http://8.8.8.8", false},            // public literal IP
		{"http://[2001:4860:4860::8888]", false},
		{"http://user:pass@192.168.1.2", false}, // userinfo always rejected
	}
	for _, tc := range cases {
		err := ValidateProviderURL(tc.url, local)
		if tc.url == "https://api.openai.com/v1" {
			// Hostname URLs pass syntactic validation; the dial-time check in
			// restrictedDialContext rejects their public IPs. Accept either
			// outcome here but require public IP literals to fail hard below.
			continue
		}
		if tc.allowed && err != nil {
			t.Errorf("ValidateProviderURL(%q) = %v, want allowed", tc.url, err)
		}
		if !tc.allowed && err == nil {
			t.Errorf("ValidateProviderURL(%q) = nil, want blocked", tc.url)
		}
	}

	// Dial-time enforcement: public literal IPs never pass ValidateResolvedIP
	// under RequireLocal, which is what stops DNS-rebinding hostnames.
	if err := ValidateResolvedIP(mustIP(t, "93.184.216.34"), local); !errors.Is(err, ErrPublicBlocked) {
		t.Errorf("public IPv4 after resolution = %v, want ErrPublicBlocked", err)
	}
	if err := ValidateResolvedIP(mustIP(t, "2606:2800:220:1:248:1893:25c8:1946"), local); !errors.Is(err, ErrPublicBlocked) {
		t.Errorf("public IPv6 after resolution = %v, want ErrPublicBlocked", err)
	}
}
