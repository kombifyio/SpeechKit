//go:build linux

package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ResolveWhisperBinary returns the path to the whisper.cpp server binary,
// preferring the configured value and falling back to well-known Linux paths.
// Returns an error if no candidate exists on disk.
//
// Kept in a Linux-only file so the Windows desktop build does not pick up
// these conventions.
func ResolveWhisperBinary(configured string) (string, error) {
	candidates := []string{
		strings.TrimSpace(configured),
		"/usr/local/bin/whisper-server",
		"/usr/local/bin/whisper.cpp",
		"/opt/whisper.cpp/whisper-server",
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	return "", errors.New("whisper binary not found on any expected path")
}

// ResolveModelDir returns the configured model directory, creating it if it
// does not exist. Used by whisper.cpp for downloaded model artifacts.
func ResolveModelDir(configured string) (string, error) {
	dir := strings.TrimSpace(configured)
	if dir == "" {
		dir = "/var/lib/speechkit/models"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Normalize to absolute for logging clarity.
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, nil //nolint:nilerr // original dir still usable even if Abs failed
	}
	return abs, nil
}
