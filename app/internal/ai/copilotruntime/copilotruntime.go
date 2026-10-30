// Package copilotruntime installs the GitHub Copilot CLI on demand.
//
// The installer does not bundle copilot.exe (144 MB). SpeechKit downloads the
// pinned npm release the first time the Copilot integration needs it, checks
// the tarball and the executable against pinned digests and writes the file
// atomically under the machine-local data directory. Only Windows x64 has a
// pin; every other platform reports that the runtime is not available.
package copilotruntime

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/app/internal/runtimepath"
)

// pin describes one installable release for one platform.
type pin struct {
	Version string
	URL     string
	// Size and SHA256 describe the downloaded .tgz; Integrity is the npm
	// registry's sha512 value for the same file.
	Size      int64
	SHA256    string
	Integrity string
	// ExeEntry is the one tar entry that is extracted.
	ExeEntry  string
	ExeName   string
	ExeSize   int64
	ExeSHA256 string
}

// windowsAMD64 is @github/copilot-win32-x64 1.0.82. The executable digest is
// the one the Windows installer pinned while it bundled copilot.exe.
var windowsAMD64 = pin{
	Version:   "1.0.82",
	URL:       "https://registry.npmjs.org/@github/copilot-win32-x64/-/copilot-win32-x64-1.0.82.tgz",
	Size:      136877000,
	SHA256:    "022d0097f2223d92f230e0c43fc54cc726b9dbef1392f4248fe9cc3ab20f5d49",
	Integrity: "sha512-m4iSROOMEPp1yRIQQwbnO0CDfwB4syESyHoeSLLFuynMR4/ApghAdyBrBQQcTKGsUu3R5rOE64RYlVmyKmuA9A==",
	ExeEntry:  "package/copilot.exe",
	ExeName:   "copilot.exe",
	ExeSize:   143927584,
	ExeSHA256: "af47bd92079e989cb88bc3d5c1f4351a0bbe0cf3f423e600acd79d544a592d56",
}

// installTimeout bounds one install job; the download is ~130 MiB.
const installTimeout = 30 * time.Minute

// State is where the runtime stands.
type State string

const (
	StateReady       State = "ready"
	StateMissing     State = "missing"
	StateInstalling  State = "installing"
	StateFailed      State = "failed"
	StateUnsupported State = "unsupported"
)

// Status is a point-in-time view of the runtime for the settings page.
type Status struct {
	State   State  `json:"state"`
	Version string `json:"version,omitempty"`
	// DownloadBytes is the size of the download, 0 where none exists.
	DownloadBytes int64 `json:"downloadBytes"`
	DoneBytes     int64 `json:"doneBytes"`
	TotalBytes    int64 `json:"totalBytes"`
	// Error is the last install failure, until the next attempt starts.
	Error string `json:"error,omitempty"`
}

func currentPin() (pin, bool) {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return windowsAMD64, true
	}
	return pin{}, false
}

// Supported reports whether a runtime exists for this platform.
func Supported() bool {
	_, ok := currentPin()
	return ok
}

// Path returns the installed executable. The size check keeps the hot path
// cheap; the digest was verified when the file was installed.
func Path() (string, bool) {
	p, ok := currentPin()
	if !ok {
		return "", false
	}
	dest, err := destination(p)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(dest)
	if err != nil || info.IsDir() || info.Size() != p.ExeSize {
		return "", false
	}
	return dest, true
}

func destination(p pin) (string, error) {
	local, err := runtimepath.LocalDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(local, "runtime", "copilot-cli", p.Version, p.ExeName), nil
}

// manager is the single-flight job state.
type manager struct {
	mu      sync.Mutex
	done    chan struct{} // non-nil while a job runs
	doneB   int64
	totalB  int64
	lastErr string
}

var jobs manager

// Snapshot reports the runtime state and any running install's progress.
func Snapshot() Status {
	p, ok := currentPin()
	if !ok {
		return Status{State: StateUnsupported}
	}
	status := Status{Version: p.Version, DownloadBytes: p.Size}
	jobs.mu.Lock()
	running := jobs.done != nil
	status.DoneBytes, status.TotalBytes, status.Error = jobs.doneB, jobs.totalB, jobs.lastErr
	jobs.mu.Unlock()
	switch {
	case running:
		status.State = StateInstalling
		status.Error = ""
	default:
		if _, installed := Path(); installed {
			status.State = StateReady
			status.Error = ""
			status.DoneBytes, status.TotalBytes = 0, 0
		} else if status.Error != "" {
			status.State = StateFailed
		} else {
			status.State = StateMissing
		}
	}
	return status
}

// Start begins an install in the background and returns at once. It does
// nothing when the runtime is installed, unsupported or already installing.
func Start() {
	if _, ok := currentPin(); !ok {
		return
	}
	if _, installed := Path(); installed {
		return
	}
	jobs.start()
}

// Install installs the runtime and blocks until the job ends or ctx is
// cancelled. Concurrent callers share one job; cancelling ctx only stops this
// caller's wait. progress (optional) receives the bytes done and the total.
func Install(ctx context.Context, progress func(done, total int64)) error {
	p, ok := currentPin()
	if !ok {
		return errors.New("the GitHub Copilot runtime is not available on this platform")
	}
	if _, installed := Path(); installed {
		return nil
	}
	done := jobs.start()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			if progress != nil {
				progress(p.Size, p.Size)
			}
			if _, installed := Path(); installed {
				return nil
			}
			jobs.mu.Lock()
			msg := jobs.lastErr
			jobs.mu.Unlock()
			if msg == "" {
				msg = "install did not complete"
			}
			return errors.New(msg)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if progress != nil {
				s := Snapshot()
				progress(s.DoneBytes, s.TotalBytes)
			}
		}
	}
}

// start launches the job unless one runs and returns its completion channel.
func (m *manager) start() <-chan struct{} {
	p, _ := currentPin()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done != nil {
		return m.done
	}
	done := make(chan struct{})
	m.done = done
	m.doneB, m.totalB, m.lastErr = 0, 0, ""
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
		defer cancel()
		err := run(ctx, p, func(d, t int64) {
			m.mu.Lock()
			m.doneB, m.totalB = d, t
			m.mu.Unlock()
		})
		m.mu.Lock()
		if err != nil {
			m.lastErr = err.Error()
		}
		m.done = nil
		m.mu.Unlock()
		close(done)
	}()
	return done
}

// run downloads, verifies and installs one release.
func run(ctx context.Context, p pin, progress func(done, total int64)) (err error) {
	dest, err := destination(p)
	if err != nil {
		return fmt.Errorf("locate install directory: %w", err)
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create install directory: %w", err)
	}
	// A job cut off by quitting the app leaves its temp file behind; only one
	// job runs at a time, so every leftover is stale.
	if stale, _ := filepath.Glob(filepath.Join(dir, p.ExeName+".*.part")); len(stale) > 0 {
		for _, leftover := range stale {
			_ = os.Remove(leftover)
		}
	}

	client := &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != "https" {
			return errors.New("refusing a redirect to a non-https URL")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download GitHub Copilot runtime: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download GitHub Copilot runtime: server returned %d", resp.StatusCode)
	}
	progress(0, p.Size)

	// The body is capped one byte above the pinned size, so a longer stream is
	// detected rather than read to the end.
	counter := &countingReader{r: io.LimitReader(resp.Body, p.Size+1), progress: progress, total: p.Size}
	sum256, sum512 := sha256.New(), sha512.New()
	body := io.TeeReader(counter, io.MultiWriter(sum256, sum512))

	tmp, err := extractExecutable(body, p, dir)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmp)
		}
	}()
	// extractExecutable stops reading at the end of the executable's entry;
	// the digests cover the whole tarball.
	if _, err = io.Copy(io.Discard, body); err != nil {
		return fmt.Errorf("download GitHub Copilot runtime: %w", err)
	}
	if counter.n != p.Size {
		return fmt.Errorf("GitHub Copilot runtime download is %d bytes, expected %d", counter.n, p.Size)
	}
	if got := hex.EncodeToString(sum256.Sum(nil)); got != p.SHA256 {
		return fmt.Errorf("GitHub Copilot runtime SHA-256 mismatch: expected %s, got %s", p.SHA256, got)
	}
	if got := "sha512-" + base64.StdEncoding.EncodeToString(sum512.Sum(nil)); got != p.Integrity {
		return errors.New("GitHub Copilot runtime integrity (sha512) mismatch")
	}
	// Rename replaces an existing file, so a truncated earlier copy is healed.
	if err = os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("install GitHub Copilot runtime: %w", err)
	}
	return nil
}

// extractExecutable copies the pinned entry of the gzip tar stream into a
// temp file in dir and returns its path once the size and SHA-256 match. Any
// other entry is skipped, never written; an entry name that is absolute or
// climbs out of the archive root fails the install.
func extractExecutable(body io.Reader, p pin, dir string) (string, error) {
	zr, err := gzip.NewReader(body)
	if err != nil {
		return "", fmt.Errorf("read GitHub Copilot runtime archive: %w", err)
	}
	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", errors.New("GitHub Copilot runtime archive does not contain " + p.ExeEntry)
		}
		if err != nil {
			return "", fmt.Errorf("read GitHub Copilot runtime archive: %w", err)
		}
		if unsafeEntry(header.Name) {
			return "", fmt.Errorf("GitHub Copilot runtime archive has an unsafe entry %q", header.Name)
		}
		if header.Name != p.ExeEntry || header.Typeflag != tar.TypeReg {
			continue
		}
		if header.Size != p.ExeSize {
			return "", fmt.Errorf("%s is %d bytes, expected %d", p.ExeEntry, header.Size, p.ExeSize)
		}
		return writeVerified(tr, p, dir)
	}
}

func unsafeEntry(name string) bool {
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, `\`) || strings.Contains(name, `\`) || strings.Contains(name, ":") {
		return true
	}
	for _, part := range strings.Split(path.Clean(name), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func writeVerified(src io.Reader, p pin, dir string) (string, error) {
	f, err := os.CreateTemp(dir, p.ExeName+".*.part")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmp := f.Name()
	fail := func(err error) (string, error) {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, p.ExeSize+1))
	if err != nil {
		return fail(fmt.Errorf("write %s: %w", p.ExeName, err))
	}
	if n != p.ExeSize {
		return fail(fmt.Errorf("%s is %d bytes, expected %d", p.ExeName, n, p.ExeSize))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.ExeSHA256 {
		return fail(fmt.Errorf("%s SHA-256 mismatch: expected %s, got %s", p.ExeName, p.ExeSHA256, got))
	}
	if err := f.Sync(); err != nil {
		return fail(fmt.Errorf("flush %s: %w", p.ExeName, err))
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("close %s: %w", p.ExeName, err)
	}
	return tmp, nil
}

type countingReader struct {
	r        io.Reader
	n        int64
	total    int64
	progress func(done, total int64)
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	if n > 0 {
		c.progress(min(c.n, c.total), c.total)
	}
	return n, err
}
