package discovery

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestStartDisabledIsNoop(t *testing.T) {
	a, err := Start(&config.Config{}, "test", nil)
	if err != nil {
		t.Fatalf("disabled discovery must not error: %v", err)
	}
	if a != nil {
		t.Fatal("disabled discovery must not return an announcer")
	}
	a.Shutdown() // nil-safe
}

func TestStartNilConfig(t *testing.T) {
	a, err := Start(nil, "test", nil)
	if err != nil || a != nil {
		t.Fatalf("nil config must be a no-op, got a=%v err=%v", a, err)
	}
}

func TestTXTLinesStayCredentialFree(t *testing.T) {
	lines := txtLines("http://192.168.1.20:8080", "0.60.0", []string{"dictation", "assist", "voiceagent"})
	rec, ok := ParseTXT("wohnzimmer", lines)
	if !ok {
		t.Fatal("announcer TXT must stay dialable")
	}
	if rec.URL != "http://192.168.1.20:8080" {
		t.Fatalf("url = %q", rec.URL)
	}
	for _, line := range lines {
		key, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if _, bad := credentialTXTKeys[strings.ToLower(key)]; bad {
			t.Fatalf("announcer emitted credential key %q", key)
		}
	}
}

func TestListenPort(t *testing.T) {
	cases := map[string]int{
		":8080":         8080,
		"0.0.0.0:9000":  9000,
		"127.0.0.1:443": 443,
		"":              8080,
		"garbage":       8080,
		":0":            8080,
	}
	for in, want := range cases {
		if got := listenPort(in); got != want {
			t.Errorf("listenPort(%q) = %d, want %d", in, got, want)
		}
	}
}
