package netsec

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestValidateProviderURL(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		opts    ValidationOptions
		wantErr error
	}{
		// Baseline — safe defaults
		{"empty", "", ValidationOptions{}, ErrEmptyURL},
		{"garbage", "::not a url::", ValidationOptions{}, ErrInvalidURL},
		{"https public ok", "https://api.openai.com", ValidationOptions{}, nil},
		{"https google", "https://generativelanguage.googleapis.com", ValidationOptions{}, nil},
		{"http public rejected", "http://api.example.com", ValidationOptions{}, ErrInsecureHTTP},
		{"ftp rejected", "ftp://example.com", ValidationOptions{}, ErrUnsupportedScheme},
		{"no scheme", "api.example.com/foo", ValidationOptions{}, ErrMissingScheme},
		{"no host", "https:///foo", ValidationOptions{}, ErrMissingHost},
		{"userinfo", "https://user:pass@example.com", ValidationOptions{}, ErrUserInfoForbidden},

		// Loopback
		{"loopback blocked by default", "http://127.0.0.1:8080", ValidationOptions{}, ErrLoopbackBlocked},
		{"localhost blocked by default", "http://localhost:8080", ValidationOptions{}, ErrLoopbackBlocked},
		{"loopback allowed", "http://127.0.0.1:8080", ValidationOptions{AllowLoopback: true}, nil},
		{"ipv6 loopback allowed", "http://[::1]:8080", ValidationOptions{AllowLoopback: true}, nil},
		{"localhost name allowed", "http://localhost:9090", ValidationOptions{AllowLoopback: true}, nil},

		// Private ranges
		{"rfc1918 blocked", "https://192.168.1.10", ValidationOptions{}, ErrPrivateBlocked},
		{"rfc1918 blocked 10", "https://10.0.0.5", ValidationOptions{}, ErrPrivateBlocked},
		{"rfc1918 blocked 172.16", "https://172.16.5.5", ValidationOptions{}, ErrPrivateBlocked},
		{"cgnat blocked", "https://100.64.1.1", ValidationOptions{}, ErrPrivateBlocked},
		{"link-local blocked", "https://169.254.169.254", ValidationOptions{}, ErrPrivateBlocked},
		{"rfc1918 allowed", "https://192.168.1.10", ValidationOptions{AllowPrivate: true}, nil},

		// AllowHTTP edge case (disallowed for non-loopback unless opted in)
		{"http allowed explicit", "http://api.example.com", ValidationOptions{AllowHTTP: true}, nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateProviderURL(tc.raw, tc.opts)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.wantErr)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected error %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestBuildEndpoint(t *testing.T) {
	cases := []struct {
		name    string
		base    string
		path    string
		opts    ValidationOptions
		want    string
		wantErr bool
	}{
		{"plain join", "https://api.openai.com", "v1/audio/transcriptions", ValidationOptions{}, "https://api.openai.com/v1/audio/transcriptions", false},
		{"slash variants", "https://api.openai.com/", "/v1/models", ValidationOptions{}, "https://api.openai.com/v1/models", false},
		{"base with subpath", "https://api.groq.com/openai", "v1/chat/completions", ValidationOptions{}, "https://api.groq.com/openai/v1/chat/completions", false},
		{"loopback builds", "http://127.0.0.1:8080", "inference", ValidationOptions{AllowLoopback: true}, "http://127.0.0.1:8080/inference", false},
		{"dotdot baseurl rejected", "https://api.example.com/foo/..", "v1", ValidationOptions{}, "", true},
		{"http rejected", "http://api.example.com", "v1", ValidationOptions{}, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := BuildEndpoint(tc.base, tc.path, tc.opts)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%s)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-secret-key-xxxx")
	h.Set("X-Api-Key", "hf_foobarbaz")
	h.Set("Content-Type", "application/json")
	h.Add("Set-Cookie", "session=verysecret")

	out := RedactHeaders(h)

	if got := out.Get("Authorization"); got != "[REDACTED]" {
		t.Errorf("Authorization not redacted: %q", got)
	}
	if got := out.Get("X-Api-Key"); got != "[REDACTED]" {
		t.Errorf("X-Api-Key not redacted: %q", got)
	}
	if got := out.Get("Set-Cookie"); got != "[REDACTED]" {
		t.Errorf("Set-Cookie not redacted: %q", got)
	}
	if got := out.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type wrongly redacted: %q", got)
	}

	// Ensure no secret substring leaked.
	for _, vs := range out {
		for _, v := range vs {
			if strings.Contains(v, "sk-secret") || strings.Contains(v, "hf_foo") || strings.Contains(v, "verysecret") {
				t.Fatalf("secret leaked in redacted header: %q", v)
			}
		}
	}

	// Original map must be untouched.
	if h.Get("Authorization") != "Bearer sk-secret-key-xxxx" {
		t.Errorf("original header was mutated")
	}
}

func TestRedactBearer(t *testing.T) {
	if RedactBearer("") != "" {
		t.Fatal("empty should stay empty")
	}
	if got := RedactBearer("Bearer abc.def.ghi"); got != "Bearer [REDACTED]" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestNewSafeHTTPClient(t *testing.T) {
	c := NewSafeHTTPClient(ClientOptions{})
	if c.Timeout == 0 {
		t.Fatal("expected non-zero default timeout")
	}
	rt, ok := c.Transport.(*RedactingRoundTripper)
	if !ok {
		t.Fatalf("expected RedactingRoundTripper, got %T", c.Transport)
	}
	if rt.Base == nil {
		t.Fatal("base transport must not be nil")
	}
}
