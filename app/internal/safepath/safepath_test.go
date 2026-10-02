package safepath

import (
	"os"
	"path/filepath"
	"testing"
)

// A configured executable runs as the user, so only an absolute local path
// to a regular file with the expected name is accepted.
func TestExecutableAcceptsOnlyLocalAbsoluteExpectedBinary(t *testing.T) {
	dir := t.TempDir()
	piper := filepath.Join(dir, "piper")
	other := filepath.Join(dir, "calc.exe")
	for _, p := range []string{piper, other} {
		if err := os.WriteFile(p, []byte("x"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		path string
		ok   bool
	}{
		{"unc backslash", `\\attacker.example\share\piper`, false},
		{"unc slash", `//attacker.example/share/piper`, false},
		{"device path", `\\?\UNC\attacker.example\share\piper`, false},
		{"relative", "piper", false},
		{"wrong name", other, false},
		{"directory", dir, false},
		{"missing", filepath.Join(dir, "missing", "piper"), false},
		{"local binary", piper, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Executable(tc.path, "piper", "piper.exe")
			if (err == nil) != tc.ok {
				t.Fatalf("Executable(%q) = %v, want ok=%v", tc.path, err, tc.ok)
			}
		})
	}
}
