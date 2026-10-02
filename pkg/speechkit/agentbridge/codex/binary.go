package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// errBinaryNotNative marks a codex target the bridge refuses to launch: on
// Windows anything but a native .exe (npm installs put codex.cmd/codex.ps1
// shims on PATH, and CreateProcess runs .cmd/.bat through cmd.exe, whose
// quoting rules Go's argument escaping does not cover).
var errBinaryNotNative = errors.New("codex binary is not a native executable")

// resolveBinary returns the codex binary to use: an explicitly configured
// path wins (it must be absolute); otherwise PATH lookup ("codex"). On
// Windows only a native codex.exe is accepted — a PATH hit on the npm
// codex.cmd shim is redirected to the codex.exe bundled inside the npm
// package, or refused with a speakable hint to configure binary_path. The
// resolved path is part of Status so hosts can log/audit exactly which
// binary is driven.
func resolveBinary(configured string) (string, error) {
	if configured = strings.TrimSpace(configured); configured != "" {
		if !filepath.IsAbs(configured) {
			return "", fmt.Errorf("agent_bridge.codex.binary_path %q must be an absolute path", configured)
		}
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", err
		}
		if err := checkNativeBinary(path); err != nil {
			return "", err
		}
		return path, nil
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		return "", err
	}
	if checkNativeBinary(path) == nil {
		return path, nil
	}
	if native, ok := npmNativeBinary(path, runtime.GOARCH); ok {
		return native, nil
	}
	return "", fmt.Errorf("%w: found %s; set agent_bridge.codex.binary_path to the absolute path of codex.exe", errBinaryNotNative, filepath.Base(path))
}

// checkNativeBinary refuses targets the OS would run through a script host.
// Only Windows has that problem; other platforms execute the file directly.
func checkNativeBinary(path string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	return windowsNativeBinary(path)
}

// windowsNativeBinary is the Windows rule: every extension except .exe is
// refused (.cmd/.bat go through cmd.exe, .ps1/.vbs/.js through their
// interpreters).
func windowsNativeBinary(path string) error {
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		return fmt.Errorf("%w: %s (only codex.exe is launched on Windows)", errBinaryNotNative, filepath.Base(path))
	}
	return nil
}

// npmNativeBinary locates the native codex.exe that `npm i -g @openai/codex`
// installs next to the codex.cmd shim: the shim lives in the npm prefix and
// the package vendors per-target binaries under
// node_modules/@openai/codex/vendor/<target-triple>/codex/codex.exe
// (optionally inside a per-platform @openai/codex-* dependency). Best effort:
// false means the caller must ask for an explicit binary_path.
func npmNativeBinary(shimPath, goarch string) (string, bool) {
	prefix := filepath.Dir(shimPath)
	pkg := filepath.Join(prefix, "node_modules", "@openai", "codex")
	triples := []string{"x86_64-pc-windows-msvc", "aarch64-pc-windows-msvc"}
	if goarch == "arm64" {
		triples[0], triples[1] = triples[1], triples[0]
	}
	var candidates []string
	for _, triple := range triples {
		candidates = append(candidates, filepath.Join(pkg, "vendor", triple, "codex", "codex.exe"))
	}
	for _, pattern := range []string{
		filepath.Join(pkg, "node_modules", "@openai", "codex-win32-*", "vendor", "*", "codex", "codex.exe"),
		filepath.Join(prefix, "node_modules", "@openai", "codex-win32-*", "vendor", "*", "codex", "codex.exe"),
	} {
		if matches, err := filepath.Glob(pattern); err == nil {
			candidates = append(candidates, matches...)
		}
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate, true
		}
	}
	return "", false
}

// codexEnv returns the child environment for a codex launch. The npm
// launcher (bin/codex.js) prepends the package's vendored helper directory
// (vendor/<triple>/path, which carries ripgrep) to PATH; launching codex.exe
// directly keeps that behavior when the binary sits in that vendored layout
// (…/vendor/<triple>/codex/codex[.exe]) and the directory exists; any other
// location never gets a PATH entry added. nil means "inherit the parent
// environment unchanged".
func codexEnv(binary string) []string {
	archRoot := filepath.Dir(filepath.Dir(binary))
	if filepath.Base(filepath.Dir(binary)) != "codex" || !strings.EqualFold(filepath.Base(filepath.Dir(archRoot)), "vendor") {
		return nil
	}
	helperDir := filepath.Join(archRoot, "path")
	if info, err := os.Stat(helperDir); err != nil || !info.IsDir() {
		return nil
	}
	env := os.Environ()
	for i, kv := range env {
		if name, value, ok := strings.Cut(kv, "="); ok && strings.EqualFold(name, "PATH") {
			env[i] = name + "=" + helperDir + string(os.PathListSeparator) + value
			return env
		}
	}
	return append(env, "PATH="+helperDir)
}

// probeVersion runs `codex --version` with a short deadline and returns the
// first output line ("codex-cli 0.47.0" style). A probe failure is not fatal
// for detection — the caller degrades Status.Detail instead.
func probeVersion(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--version")
	cmd.Env = codexEnv(binary)
	configureSysProcAttr(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return line, nil
}
