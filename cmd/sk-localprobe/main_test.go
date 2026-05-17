package main

import "testing"

func TestMatchTranscriptCaseInsensitive(t *testing.T) {
	cases := []struct {
		name     string
		actual   string
		expected string
		want     bool
	}{
		{"exact", "Hello, World!", "hello, world!", true},
		{"trim whitespace", "  hello world  ", "hello world", true},
		{"regex prefix marker", "hello world good morning", "(?i)hello,?\\s+world", true},
		{"caret regex anchor", "hello world", "^hello", true},
		{"miss case insensitive", "goodbye", "(?i)hello,?\\s+world", false},
		{"miss plain", "goodbye", "hello world", false},
		{"empty expected matches anything", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := matchTranscript(c.actual, c.expected)
			if got != c.want {
				t.Fatalf("matchTranscript(%q, %q) = %v, want %v", c.actual, c.expected, got, c.want)
			}
		})
	}
}

func TestFreePortReturnsUsableEphemeralPort(t *testing.T) {
	p, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if p <= 0 || p > 65535 {
		t.Fatalf("freePort = %d; not a valid port", p)
	}
}
