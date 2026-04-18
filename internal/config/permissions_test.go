package config

import (
	"os"
	"strings"
	"testing"
)

func TestConfigPermissionWarning(t *testing.T) {
	cases := []struct {
		name    string
		perm    os.FileMode
		wantMsg bool
	}{
		{"0600 owner-only", 0o600, false},
		{"0400 owner-read-only", 0o400, false},
		{"0000 no access", 0o000, false},
		{"0644 world readable", 0o644, true},
		{"0660 group readable/writable", 0o660, true},
		{"0777 world writable", 0o777, true},
		{"0604 world readable", 0o604, true},
		{"0640 group readable", 0o640, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := configPermissionWarning("/tmp/config.toml", tc.perm)
			if tc.wantMsg && got == "" {
				t.Fatalf("perm %#o: want a warning, got empty", tc.perm)
			}
			if !tc.wantMsg && got != "" {
				t.Fatalf("perm %#o: want empty, got %q", tc.perm, got)
			}
			if tc.wantMsg && !strings.Contains(got, "/tmp/config.toml") {
				t.Fatalf("warning should mention the path, got %q", got)
			}
		})
	}
}
