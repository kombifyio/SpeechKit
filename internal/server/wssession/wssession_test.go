package wssession

import (
	"net/http"
	"testing"
	"time"
)

func TestSessionManager_TicketExpiresAt(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	m, err := NewSessionManager(Options{
		TicketSecret: []byte("0123456789abcdef"),
		TicketTTL:    45 * time.Second,
		Clock:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewSessionManager: %v", err)
	}
	want := now.UTC().Add(45 * time.Second)
	if got := m.TicketExpiresAt(); !got.Equal(want) {
		t.Fatalf("TicketExpiresAt = %v, want %v", got, want)
	}
}

func TestExtractTicket(t *testing.T) {
	r, _ := http.NewRequest(http.MethodGet, "/", nil)
	r.Header.Add("Sec-WebSocket-Protocol", "chat, ticket.abc123")
	ticket, subproto := ExtractTicket(r)
	if ticket != "abc123" || subproto != "ticket.abc123" {
		t.Fatalf("ExtractTicket = (%q, %q)", ticket, subproto)
	}
	r2, _ := http.NewRequest(http.MethodGet, "/", nil)
	if ticket, _ := ExtractTicket(r2); ticket != "" {
		t.Fatalf("expected empty ticket, got %q", ticket)
	}
}

func TestTicketSubprotocol(t *testing.T) {
	if got := TicketSubprotocol("  x  "); got != "ticket.x" {
		t.Fatalf("TicketSubprotocol = %q", got)
	}
	if got := TicketSubprotocol("   "); got != "" {
		t.Fatalf("TicketSubprotocol(blank) = %q", got)
	}
}

func TestNormalizeAllowedOrigins_DropsWildcardByDefault(t *testing.T) {
	got := NormalizeAllowedOrigins([]string{"https://a.example", "*", "  ", "https://b.example"})
	if len(got) != 2 || got[0] != "https://a.example" || got[1] != "https://b.example" {
		t.Fatalf("NormalizeAllowedOrigins = %#v", got)
	}
}

func TestOriginAllowed(t *testing.T) {
	if OriginAllowed("", []string{"https://a.example"}) {
		t.Fatal("empty origin must be denied without the env opt-in")
	}
	if !OriginAllowed("https://a.example", []string{"https://a.example"}) {
		t.Fatal("exact origin match must be allowed")
	}
	if OriginAllowed("https://evil.example", []string{"https://a.example"}) {
		t.Fatal("unlisted origin must be denied")
	}
}

func TestWebSocketURL(t *testing.T) {
	tests := []struct {
		name            string
		host            string
		apiPrefixHeader string
		publicURL       string
		https           bool
		relPath         string
		want            string
	}{
		{
			name:    "request derived plain",
			host:    "server.local:8080",
			relPath: "/dictation/stream/sessions/abc/ws",
			want:    "ws://server.local:8080/v1/dictation/stream/sessions/abc/ws",
		},
		{
			name:            "api alias prefix",
			host:            "server.local:8080",
			apiPrefixHeader: "/api",
			relPath:         "/voiceagent/sessions/abc/ws",
			want:            "ws://server.local:8080/api/v1/voiceagent/sessions/abc/ws",
		},
		{
			name:      "public url wins",
			host:      "container:8080",
			publicURL: "https://speechkit.example.com/api",
			relPath:   "/dictation/stream/sessions/abc/ws",
			want:      "wss://speechkit.example.com/api/v1/dictation/stream/sessions/abc/ws",
		},
		{
			name:    "https request",
			host:    "server.local",
			https:   true,
			relPath: "/dictation/stream/sessions/abc/ws",
			want:    "wss://server.local/v1/dictation/stream/sessions/abc/ws",
		},
		{
			name:    "hostile host falls back to localhost",
			host:    "bad host/with@junk",
			relPath: "/x/ws",
			want:    "ws://localhost/v1/x/ws",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WebSocketURL(tt.host, tt.apiPrefixHeader, tt.publicURL, tt.https, tt.relPath)
			if got != tt.want {
				t.Fatalf("WebSocketURL = %q, want %q", got, tt.want)
			}
		})
	}
}
