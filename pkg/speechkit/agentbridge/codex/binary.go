package codex

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// resolveBinary returns the codex binary to use: an explicitly configured
// absolute path wins; otherwise PATH lookup ("codex", which LookPath resolves
// to codex.exe on Windows). The resolved path is part of Status so hosts can
// log/audit exactly which binary is driven.
func resolveBinary(configured string) (string, error) {
	if strings.TrimSpace(configured) != "" {
		return exec.LookPath(configured)
	}
	return exec.LookPath("codex")
}

// probeVersion runs `codex --version` with a short deadline and returns the
// first output line ("codex-cli 0.47.0" style). A probe failure is not fatal
// for detection — the caller degrades Status.Detail instead.
func probeVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return line, nil
}
