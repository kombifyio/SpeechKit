package pairing

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNewRoundTripBecomesSelfHostMaterial(t *testing.T) {
	got, err := New("http://192.168.1.20:8080/", "sk-server-pairing", "wohnzimmer")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	parsed, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if parsed.V != Version || parsed.Auth != AuthBearer {
		t.Fatalf("contract = v %q auth %q", parsed.V, parsed.Auth)
	}
	if parsed.ServerURL != "http://192.168.1.20:8080" {
		t.Fatalf("server_url = %q", parsed.ServerURL)
	}
	if parsed.Token != "sk-server-pairing" {
		t.Fatalf("token missing from parsed payload")
	}
	if parsed.Name != "wohnzimmer" {
		t.Fatalf("name = %q", parsed.Name)
	}
}

func TestParseRejectsMissingURLOrToken(t *testing.T) {
	if _, err := New("", "sk-server-pairing", "wohnzimmer"); err == nil {
		t.Fatal("missing URL must not encode")
	}
	if _, err := New("http://192.168.1.20:8080", "", "wohnzimmer"); err == nil {
		t.Fatal("missing token must not encode")
	}
	if _, err := Parse([]byte(`{"v":"speechkit.pairing.v1","server_url":"","auth":"bearer","token":"sk"}`)); err == nil {
		t.Fatal("empty server_url must not parse")
	}
	if _, err := Parse([]byte(`{"v":"speechkit.pairing.v1","server_url":"http://192.168.1.20:8080","auth":"bearer","token":""}`)); err == nil {
		t.Fatal("empty token must not parse")
	}
}

func TestQRSVGEncodesAPairingPayload(t *testing.T) {
	payload, err := New("http://192.168.1.20:8080", "sk-server-pairing", "wohnzimmer")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	svg, err := QRSVG(string(raw))
	if err != nil {
		t.Fatalf("QRSVG: %v", err)
	}
	if !strings.Contains(svg, `<svg xmlns="http://www.w3.org/2000/svg"`) {
		t.Fatal("pairing QR must be an SVG")
	}
	if !strings.Contains(svg, "shape-rendering") {
		t.Fatal("pairing QR must render as crisp modules")
	}
}

func TestQRSVGRejectsEmptyContent(t *testing.T) {
	if _, err := QRSVG("  "); err == nil {
		t.Fatal("empty content must not produce a QR")
	}
}

func TestParseRejectsWrongVersionAndAuth(t *testing.T) {
	if _, err := Parse([]byte(`{"v":"speechkit.pairing.v0","server_url":"http://192.168.1.20:8080","auth":"bearer","token":"sk"}`)); err == nil {
		t.Fatal("unknown version must not parse")
	}
	if _, err := Parse([]byte(`{"v":"speechkit.pairing.v1","server_url":"http://192.168.1.20:8080","auth":"oidc","token":"sk"}`)); err == nil {
		t.Fatal("non-bearer auth must not parse")
	}
}

func TestDeviceTokenIsStoredAsHashAndRevocationIsImmediate(t *testing.T) {
	path := t.TempDir() + "/paired-devices.json"
	reg, err := OpenDeviceRegistry(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Unix(1_800_000_000, 0)
	dev, token, err := reg.Issue("Kitchen Box", "box", "kbx-a1", "192.168.1.50", now)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !strings.HasPrefix(token, DeviceTokenPrefix) || dev.TokenSHA256 != "" {
		t.Fatalf("token prefix or public view leaked the hash")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	if strings.Contains(string(raw), token) || !strings.Contains(string(raw), HashDeviceToken(token)) {
		t.Fatalf("registry must hold only the token hash")
	}
	reloaded, err := OpenDeviceRegistry(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got, ok := reloaded.Verify(token, now); !ok || got.ID != dev.ID {
		t.Fatalf("reloaded registry does not accept the token")
	}
	if _, ok := reloaded.Verify(token+"x", now); ok {
		t.Fatalf("altered token accepted")
	}
	if _, ok := reloaded.Verify("sk-server-shared-bearer-0123456789abcdef0123", now); ok {
		t.Fatalf("non-device bearer accepted as device token")
	}
	if _, err := reloaded.Revoke(dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := reloaded.Verify(token, now); ok {
		t.Fatalf("revoked token still accepted")
	}
	again, _ := OpenDeviceRegistry(path)
	if _, ok := again.Verify(token, now); ok {
		t.Fatalf("revocation not persisted")
	}
}

func TestRepairingSameHardwareReplacesOldToken(t *testing.T) {
	reg, _ := OpenDeviceRegistry("")
	now := time.Unix(1_800_000_000, 0)
	_, first, _ := reg.Issue("Box", "box", "kbx-a1", "192.168.1.50", now)
	_, second, _ := reg.Issue("Box", "box", "kbx-a1", "192.168.1.50", now)
	if _, ok := reg.Verify(first, now); ok {
		t.Fatalf("old token of re-paired hardware still accepted")
	}
	if _, ok := reg.Verify(second, now); !ok {
		t.Fatalf("new token refused")
	}
}

func TestPairingRequestReleasesTokenOnlyAfterApprovalAndOnce(t *testing.T) {
	book := NewRequestBook()
	now := time.Unix(1_800_000_000, 0)
	req, secret, err := book.Create("Kitchen Box", "box", "kbx-a1", "192.168.1.50", now)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(req.Code) != 6 {
		t.Fatalf("code = %q", req.Code)
	}
	if _, err := book.Poll(req.ID, "wrong", now); err == nil {
		t.Fatalf("wrong poll secret accepted")
	}
	if _, err := book.Take(req.ID, secret, now); err == nil {
		t.Fatalf("token released before approval")
	}
	other := "999999"
	if req.Code == other {
		other = "111111"
	}
	if _, err := book.Decide(other, true, now); err == nil {
		t.Fatalf("unknown code approved")
	}
	if _, err := book.Decide(req.Code[:3]+"-"+req.Code[3:], true, now); err != nil {
		t.Fatalf("approve by displayed code: %v", err)
	}
	if _, err := book.Take(req.ID, secret, now); err != nil {
		t.Fatalf("take approved: %v", err)
	}
	if _, err := book.Take(req.ID, secret, now); err == nil {
		t.Fatalf("approved request released twice")
	}
	late, lateSecret, _ := book.Create("Late Box", "box", "", "192.168.1.51", now)
	_, _ = book.Decide(late.Code, true, now)
	if _, err := book.Take(late.ID, lateSecret, now.Add(RequestTTL)); err == nil {
		t.Fatalf("expired request released a token")
	}
}

func TestPairingRequestsAreBoundedPerSource(t *testing.T) {
	book := NewRequestBook()
	now := time.Unix(1_800_000_000, 0)
	for i := 0; i < maxPendingPerSource; i++ {
		if _, _, err := book.Create("Box", "box", "", "192.168.1.50", now); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	// Decided but uncollected requests still occupy the source's pool slots.
	requests := book.Pending(now)
	_, _ = book.Decide(requests[0].Code, true, now)
	_, _ = book.Decide(requests[1].Code, false, now)
	if _, _, err := book.Create("Box", "box", "", "192.168.1.50", now); err != ErrTooManyRequests {
		t.Fatalf("per-source cap not enforced: %v", err)
	}
	if _, _, err := book.Create("Other Box", "box", "", "192.168.1.51", now); err != nil {
		t.Fatalf("one source prevented another from pairing: %v", err)
	}
}

func TestDeviceRequestRequiresJSONWithoutOrigin(t *testing.T) {
	origin, empty := "https://attacker.example", ""
	cases := []struct {
		name, contentType string
		origin            *string
		want              bool
	}{
		{"json", "application/json", nil, true},
		{"json charset", "application/json; charset=utf-8", nil, true},
		{"missing type", "", nil, false},
		{"simple browser post", "text/plain", nil, false},
		{"malformed type", "application/json; broken", nil, false},
		{"browser json", "application/json", &origin, false},
		{"empty origin header", "application/json", &empty, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/v1/pairing/device-requests", nil)
			if tc.contentType != "" {
				r.Header.Set("Content-Type", tc.contentType)
			}
			if tc.origin != nil {
				r.Header.Set("Origin", *tc.origin)
			}
			if got := DeviceRequestHeadersValid(r); got != tc.want {
				t.Fatalf("admitted = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLANSourceRefusesInternetAndUntrustedProxies(t *testing.T) {
	trusted := func(*http.Request) bool { return true }
	untrusted := func(*http.Request) bool { return false }
	cases := []struct {
		name    string
		remote  string
		headers map[string]string
		trust   func(*http.Request) bool
		want    bool
	}{
		{"loopback without listed proxy", "127.0.0.1:5000", nil, untrusted, false},
		{"loopback listed but no chain", "127.0.0.1:5000", nil, trusted, false},
		{"ipv6 loopback without chain", "[::1]:5000", nil, trusted, false},
		{"loopback unlisted with chain", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.9"}, untrusted, false},
		{"loopback listed with chain", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.9, 10.0.0.2"}, trusted, true},
		{"ipv6 loopback listed with forwarded", "[::1]:5000", map[string]string{"Forwarded": "for=192.168.1.9"}, trusted, true},
		{"forged real ip unlisted", "127.0.0.1:5000", map[string]string{"X-Real-IP": "192.168.1.9"}, untrusted, false},
		{"real ip alone listed", "127.0.0.1:5000", map[string]string{"X-Real-IP": "192.168.1.9"}, trusted, false},
		{"empty chain listed", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": ""}, trusted, false},
		{"forwarded without client", "127.0.0.1:5000", map[string]string{"Forwarded": "by=10.0.0.2"}, trusted, false},
		{"chain plus public real ip", "127.0.0.1:5000", map[string]string{"X-Forwarded-For": "192.168.1.9", "X-Real-IP": "198.51.100.7"}, trusted, false},
		{"lan peer", "192.168.1.50:5000", nil, untrusted, true},
		{"ula peer", "[fd00::5]:5000", nil, untrusted, true},
		{"link-local peer", "[fe80::1%eth0]:5000", nil, untrusted, true},
		{"public peer", "203.0.113.9:5000", nil, trusted, false},
		{"cgnat peer", "100.64.0.9:5000", nil, trusted, false},
		{"tunnel on loopback forwards public client", "127.0.0.1:5000", map[string]string{"CF-Connecting-IP": "198.51.100.7"}, trusted, false},
		{"trusted proxy forwards public client", "10.0.0.2:5000", map[string]string{"X-Forwarded-For": "192.168.1.9, 198.51.100.7"}, trusted, false},
		{"untrusted proxy headers", "10.0.0.2:5000", map[string]string{"X-Forwarded-For": "192.168.1.9"}, untrusted, false},
		{"trusted proxy lan client", "10.0.0.2:5000", map[string]string{"X-Forwarded-For": "192.168.1.9"}, trusted, true},
		{"forwarded obfuscated", "10.0.0.2:5000", map[string]string{"Forwarded": "for=_hidden"}, trusted, false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodPost, "/v1/pairing/device-requests", nil)
		r.RemoteAddr = tc.remote
		for k, v := range tc.headers {
			r.Header.Set(k, v)
		}
		if _, got := LANSource(r, tc.trust); got != tc.want {
			t.Errorf("%s: LANSource = %v, want %v", tc.name, got, tc.want)
		}
	}
}
