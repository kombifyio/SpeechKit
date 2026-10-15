package main

import (
	"net/url"
	"testing"
)

func TestParseLLMBaseURL(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
		want    string
	}{
		{raw: "https://api.openai.com/v1", want: "https://api.openai.com/v1"},
		{raw: "https://llm.example.com/v1/", want: "https://llm.example.com/v1"},
		{raw: "http://127.0.0.1:8080/v1", want: "http://127.0.0.1:8080/v1"},
		{raw: "http://localhost:11434/v1", want: "http://localhost:11434/v1"},
		{raw: "http://[::1]:8080/v1", want: "http://[::1]:8080/v1"},
		{raw: "http://llm.example.com/v1", wantErr: true},
		{raw: "http://192.168.1.10:8080/v1", wantErr: true},
		{raw: "ftp://llm.example.com/v1", wantErr: true},
		{raw: "https://user:pass@llm.example.com/v1", wantErr: true},
		{raw: "https://llm.example.com/v1?x=1", wantErr: true},
		{raw: "/v1", wantErr: true},
		{raw: "://bad", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseLLMBaseURL(tc.raw)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseLLMBaseURL(%q) = %v, want an error", tc.raw, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseLLMBaseURL(%q): %v", tc.raw, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("parseLLMBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
		if chat := got.JoinPath("chat", "completions").String(); chat != tc.want+"/chat/completions" {
			t.Errorf("chat URL for %q = %q", tc.raw, chat)
		}
	}
}

func TestLLMAPIKeyGoesOnlyWhereAllowed(t *testing.T) {
	mustURL := func(raw string) *url.URL {
		u, err := parseLLMBaseURL(raw)
		if err != nil {
			t.Fatalf("parseLLMBaseURL(%q): %v", raw, err)
		}
		return u
	}
	const key = "sk-test"
	cases := []struct {
		name, base, sendTo, want string
	}{
		{name: "openai", base: "https://api.openai.com/v1", want: key},
		{name: "custom host without opt-in", base: "https://llm.example.com/v1", want: ""},
		{name: "local server without opt-in", base: "http://127.0.0.1:8080/v1", want: ""},
		{name: "custom host opted in", base: "https://llm.example.com/v1", sendTo: "llm.example.com", want: key},
		{name: "opt-in names another host", base: "https://llm.example.com/v1", sendTo: "other.example.com", want: ""},
		{name: "openai lookalike", base: "https://api.openai.com.evil.example/v1", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := llmAPIKey(mustURL(tc.base), key, tc.sendTo); got != tc.want {
				t.Fatalf("llmAPIKey = %q, want %q", got, tc.want)
			}
		})
	}
}
