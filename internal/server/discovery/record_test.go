package discovery

import (
	"net"
	"testing"

	"github.com/hashicorp/mdns"
)

func TestParseTXTAnnouncerContract(t *testing.T) {
	rec, ok := ParseTXT("wohnzimmer", []string{
		"url=http://192.168.1.20:8080",
		"modes=dictation,assist,voiceagent",
		"version=0.60.0",
	})
	if !ok {
		t.Fatal("expected a dialable record")
	}
	if rec.URL != "http://192.168.1.20:8080" {
		t.Fatalf("url = %q", rec.URL)
	}
	if rec.Version != "0.60.0" {
		t.Fatalf("version = %q", rec.Version)
	}
	if len(rec.Modes) != 3 || rec.Modes[2] != "voiceagent" {
		t.Fatalf("modes = %#v", rec.Modes)
	}
}

func TestParseTXTMissingURL(t *testing.T) {
	if _, ok := ParseTXT("ghost", []string{"modes=dictation", "version=0.60.0"}); ok {
		t.Fatal("record without url must not be dialable")
	}
}

func TestParseTXTDropsCredentialKeys(t *testing.T) {
	rec, ok := ParseTXT("wohnzimmer", []string{
		"url=http://192.168.1.20:8080",
		"token=svc-secret",
		"auth=Bearer abc",
		"password=nope",
	})
	if !ok {
		t.Fatal("expected a dialable record")
	}
	if rec.URL != "http://192.168.1.20:8080" {
		t.Fatalf("url = %q", rec.URL)
	}
}

func TestRecordFromEntryFallsBackToHostPort(t *testing.T) {
	rec, ok := recordFromEntry(&mdns.ServiceEntry{
		Name:   "wohnzimmer._speechkit._tcp.local.",
		AddrV4: net.ParseIP("192.168.1.20"),
		Port:   8080,
	})
	if !ok {
		t.Fatal("expected host:port fallback")
	}
	if rec.InstanceName != "wohnzimmer" {
		t.Fatalf("instance = %q", rec.InstanceName)
	}
	if rec.URL != "http://192.168.1.20:8080" {
		t.Fatalf("url = %q", rec.URL)
	}
}
