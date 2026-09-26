//go:build linux

package wakewordtraining

import (
	"testing"
)

func TestSafePathSegment(t *testing.T) {
	cases := map[string]string{
		"abc":              "abc",
		"hey_quby-01":      "hey_quby-01",
		"hey_quby-01.wav":  "hey_quby-01wav",
		"../../etc/passwd": "etcpasswd",
		"..":               "_",
		".":                "_",
		"":                 "_",
		"  ":               "_",
		"a/b/c":            "abc",
		"a\\b":             "ab",
	}
	for in, want := range cases {
		got := safePathSegment(in)
		if got != want {
			t.Errorf("safePathSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitItemPath(t *testing.T) {
	cases := []struct{ in, id, sub string }{
		{"a1", "a1", ""},
		{"a1/audio", "a1", "audio"},
		{"a1/audio/extra", "a1", "audio/extra"},
		{"", "", ""},
	}
	for _, c := range cases {
		id, sub := splitItemPath(c.in)
		if id != c.id || sub != c.sub {
			t.Errorf("splitItemPath(%q) = (%q,%q), want (%q,%q)", c.in, id, sub, c.id, c.sub)
		}
	}
}
