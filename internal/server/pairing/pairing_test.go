package pairing

import (
	"encoding/json"
	"strings"
	"testing"
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
