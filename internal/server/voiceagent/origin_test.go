//go:build linux

package voiceagent

import "testing"

func TestNormalizeAllowedOrigins_DropsWildcardByDefault(t *testing.T) {
	got := normalizeAllowedOrigins([]string{"https://a.example", "*", "   ", "https://b.example"})
	want := []string{"https://a.example", "https://b.example"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v (wildcard must be dropped fail-closed)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestNormalizeAllowedOrigins_KeepsWildcardWhenEnvSet(t *testing.T) {
	t.Setenv(envAllowWildcardOriginVar, "1")
	got := normalizeAllowedOrigins([]string{"*"})
	if len(got) != 1 || got[0] != "*" {
		t.Fatalf("got %v, want [*] when %s=1", got, envAllowWildcardOriginVar)
	}
}

func TestOriginAllowedForWebSocket(t *testing.T) {
	allowed := []string{"https://app.example.com"}

	tests := []struct {
		name   string
		origin string
		allow  []string
		want   bool
	}{
		{"exact match allowed", "https://app.example.com", allowed, true},
		{"cross origin denied", "https://evil.example.com", allowed, false},
		{"empty origin denied by default", "", allowed, false},
		{"wildcard matches when present in list", "https://anything.example", []string{"*"}, true},
		{"no wildcard, unknown origin denied", "https://anything.example", allowed, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := originAllowedForWebSocket(tt.origin, tt.allow); got != tt.want {
				t.Fatalf("originAllowedForWebSocket(%q, %v) = %v, want %v", tt.origin, tt.allow, got, tt.want)
			}
		})
	}
}

func TestOriginAllowedForWebSocket_EmptyOriginOptIn(t *testing.T) {
	t.Setenv(envAllowEmptyWSOriginVar, "1")
	if !originAllowedForWebSocket("", []string{"https://app.example.com"}) {
		t.Fatalf("empty origin must be allowed when %s=1", envAllowEmptyWSOriginVar)
	}
}
