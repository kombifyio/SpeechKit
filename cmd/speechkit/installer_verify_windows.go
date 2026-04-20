//go:build windows

package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func verifyInstallerSignature(path string) error {
	cmd := exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"$sig = Get-AuthenticodeSignature -LiteralPath $args[0]; if ($sig.Status -ne 'Valid') { [Console]::Error.WriteLine($sig.StatusMessage); exit 1 }",
		path,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("authenticode signature is not valid: %s", msg)
	}
	return nil
}
