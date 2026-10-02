//go:build !windows

package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// The local-only guarantee holds only if every credential the server would use
// to reach a hosted provider also stops scripts/install-server.sh
// --strict-local-only. The install-E2E gates clear the script's list, so a name
// missing there would let a leaked key pass the gate.
func TestStrictLocalOnlyInstallRefusesEveryCloudCredential(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(file), "..", "..", "..", "scripts", "install-server.sh")

	for _, name := range CloudCredentialEnvNames(nil) {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command("sh", script, "--strict-local-only", "--no-up", "--dir", dir)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH"), name + "=leaked"}
			out, err := cmd.CombinedOutput()

			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				t.Fatalf("install-server.sh accepted %s: err=%v\n%s", name, err, out)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("install-server.sh wrote into %s before refusing %s", dir, name)
			}
		})
	}
}
