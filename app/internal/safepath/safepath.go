// Package safepath validates file system paths that reach SpeechKit from
// configuration, the settings control plane or the history store before the
// app executes, reads, serves or deletes what they name.
//
// The threat it answers: a value such as azure_cli_path, piper_binary or a
// stored audio path is attacker-influenced whenever the settings API or the
// store is (script in the webview, a shared Postgres backend). A path that
// is relative (resolved against PATH or the working directory), on a network
// share (\\host\share\x.exe is executable over SMB), or outside the app's own
// data directory must not be accepted as if the user had picked it.
package safepath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Errors callers can branch on with errors.Is.
var (
	ErrNotAbsolute    = errors.New("safepath: path must be absolute")
	ErrNetworkPath    = errors.New("safepath: network and device paths are not allowed")
	ErrUnexpectedName = errors.New("safepath: unexpected executable name")
	ErrNotRegularFile = errors.New("safepath: not a regular file")
	ErrOutsideAllowed = errors.New("safepath: path is outside the allowed directories")
	ErrNoAllowedRoots = errors.New("safepath: no allowed directories configured")
	errEmptyPath      = errors.New("safepath: empty path")
)

// IsNetworkPath reports whether path names something other than a plain
// local file: a UNC path (\\server\share, //server/share), a Win32 device or
// extended-length path (\\?\, \\.\, which can also reach UNC shares), or, on
// Windows, a drive letter mapped to a network share. Both separator styles
// are checked on every OS, so a Windows-style value is refused even when it
// is validated elsewhere.
func IsNetworkPath(path string) bool {
	p := strings.TrimSpace(path)
	if len(p) >= 2 && isSeparator(p[0]) && isSeparator(p[1]) {
		return true
	}
	return isRemoteVolume(p)
}

func isSeparator(c byte) bool { return c == '\\' || c == '/' }

// LocalAbsolute requires an absolute path on a local volume.
func LocalAbsolute(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return errEmptyPath
	}
	if IsNetworkPath(p) {
		return fmt.Errorf("%w: %q", ErrNetworkPath, p)
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("%w: %q", ErrNotAbsolute, p)
	}
	return nil
}

// Executable checks a configured executable path: absolute, local, an
// existing regular file (after following symlinks), and a base name equal to
// one of names, compared case-insensitively ("piper", "piper.exe").
func Executable(path string, names ...string) error {
	p := strings.TrimSpace(path)
	if err := LocalAbsolute(p); err != nil {
		return err
	}
	if !nameAllowed(filepath.Base(p), names) {
		return fmt.Errorf("%w: %q (expected %s)", ErrUnexpectedName, filepath.Base(p), strings.Join(names, ", "))
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	// The link target has to satisfy the same rules: a local piper.exe
	// symlink must not point at \\host\share\payload.exe.
	if IsNetworkPath(resolved) {
		return fmt.Errorf("%w: %q", ErrNetworkPath, resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q", ErrNotRegularFile, p)
	}
	return nil
}

// ExecutableSetting checks what may be saved as an executable setting: a
// bare name from names (resolved later by the caller from its own trusted
// locations, never from PATH by default) or a path passing Executable.
func ExecutableSetting(value string, names ...string) error {
	value = strings.TrimSpace(value)
	if !strings.ContainsAny(value, `/\:`) {
		if nameAllowed(value, names) {
			return nil
		}
		return fmt.Errorf("%w: %q (expected %s)", ErrUnexpectedName, value, strings.Join(names, ", "))
	}
	return Executable(value, names...)
}

func nameAllowed(base string, names []string) bool {
	for _, name := range names {
		if strings.EqualFold(base, name) {
			return true
		}
	}
	return false
}

// Contained resolves path (made absolute, symlinks followed) and returns
// the resolved form when it lies strictly inside one of roots. A path that
// no longer exists is resolved through its parent directory, so a caller
// deleting an already-removed file still gets a containment answer; the
// returned error then wraps fs.ErrNotExist only when the parent is gone too.
func Contained(path string, roots ...string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", errEmptyPath
	}
	if IsNetworkPath(p) {
		return "", fmt.Errorf("%w: %q", ErrNetworkPath, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := resolveExistingPrefix(abs)
	if err != nil {
		return "", err
	}
	if IsNetworkPath(resolved) {
		return "", fmt.Errorf("%w: %q", ErrNetworkPath, resolved)
	}
	haveRoot := false
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		haveRoot = true
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if r, err := filepath.EvalSymlinks(rootAbs); err == nil {
			rootAbs = r
		}
		if within(resolved, rootAbs) {
			return resolved, nil
		}
	}
	if !haveRoot {
		return "", ErrNoAllowedRoots
	}
	return "", fmt.Errorf("%w: %q", ErrOutsideAllowed, p)
}

// resolveExistingPrefix follows symlinks for path, or for its parent when
// path itself does not exist (yet or any more).
func resolveExistingPrefix(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent, perr := filepath.EvalSymlinks(filepath.Dir(path))
	if perr != nil {
		return "", perr
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

// within reports whether path is strictly below root. filepath.Rel compares
// case-insensitively on Windows.
func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
