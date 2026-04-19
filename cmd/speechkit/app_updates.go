package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/netsec"
)

// installerDownloadClient uses a hardened transport (TLS 1.2+, redacting
// headers) with a long timeout for large installer downloads.
var installerDownloadClient = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Minute})

// appInstallerURLValidation restricts installer download URLs. Strict by
// default (public https only); tests may relax to allow loopback.
var appInstallerURLValidation = netsec.ValidationOptions{}

type appUpdateStatus string

const (
	appUpdateStatusPending   appUpdateStatus = "pending"
	appUpdateStatusRunning   appUpdateStatus = "running"
	appUpdateStatusDone      appUpdateStatus = "done"
	appUpdateStatusFailed    appUpdateStatus = "failed"
	appUpdateStatusCancelled appUpdateStatus = "cancelled"
)

type appUpdateJobView struct {
	ID         string          `json:"id"`
	Version    string          `json:"version"`
	AssetName  string          `json:"assetName"`
	Status     appUpdateStatus `json:"status"`
	Progress   float64         `json:"progress"`
	BytesDone  int64           `json:"bytesDone"`
	TotalBytes int64           `json:"totalBytes"`
	StatusText string          `json:"statusText"`
	FilePath   string          `json:"filePath,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type appUpdateJob struct {
	mu         sync.Mutex
	ID         string
	Version    string
	AssetName  string
	Status     appUpdateStatus
	Progress   float64
	BytesDone  int64
	TotalBytes int64
	StatusText string
	FilePath   string
	Error      string
	cancel     context.CancelFunc
}

func (j *appUpdateJob) snapshot() appUpdateJobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return appUpdateJobView{
		ID:         j.ID,
		Version:    j.Version,
		AssetName:  j.AssetName,
		Status:     j.Status,
		Progress:   j.Progress,
		BytesDone:  j.BytesDone,
		TotalBytes: j.TotalBytes,
		StatusText: j.StatusText,
		FilePath:   j.FilePath,
		Error:      j.Error,
	}
}

type appUpdateManager struct {
	mu   sync.Mutex
	jobs map[string]*appUpdateJob
}

func newAppUpdateManager() *appUpdateManager {
	return &appUpdateManager{
		jobs: make(map[string]*appUpdateJob),
	}
}

func ensureAppUpdateManager(state *appState) *appUpdateManager {
	if state == nil {
		return newAppUpdateManager()
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.appUpdates == nil {
		state.appUpdates = newAppUpdateManager()
	}
	return state.appUpdates
}

func (m *appUpdateManager) AllJobs() []appUpdateJobView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]appUpdateJobView, 0, len(m.jobs))
	for _, job := range m.jobs {
		out = append(out, job.snapshot())
	}
	return out
}

func (m *appUpdateManager) Start(release latestReleaseInfo, destDir string) appUpdateJobView {
	id := fmt.Sprintf("app-update-%d", time.Now().UnixMilli())
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in struct field, called on job cancellation
	job := &appUpdateJob{
		ID:         id,
		Version:    release.Version,
		AssetName:  release.DownloadName,
		Status:     appUpdateStatusPending,
		TotalBytes: release.DownloadSize,
		StatusText: "Starting…",
		cancel:     cancel,
	}

	m.mu.Lock()
	m.jobs[id] = job
	m.mu.Unlock()

	go m.run(ctx, job, release, destDir)
	return job.snapshot()
}

func (m *appUpdateManager) CancelJob(jobID string) bool {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok || job.cancel == nil {
		return false
	}
	job.cancel()
	return true
}

func (m *appUpdateManager) CompletedFile(jobID string) (string, bool) {
	m.mu.Lock()
	job, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok {
		return "", false
	}
	snapshot := job.snapshot()
	if snapshot.Status != appUpdateStatusDone || snapshot.FilePath == "" {
		return "", false
	}
	if _, err := os.Stat(snapshot.FilePath); err != nil {
		return "", false
	}
	return snapshot.FilePath, true
}

func (m *appUpdateManager) run(ctx context.Context, job *appUpdateJob, release latestReleaseInfo, destDir string) {
	job.mu.Lock()
	job.Status = appUpdateStatusRunning
	job.StatusText = "Downloading…"
	job.mu.Unlock()

	err := downloadAppInstaller(ctx, job, release, destDir)

	job.mu.Lock()
	defer job.mu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			job.Status = appUpdateStatusCancelled
			job.StatusText = "Cancelled"
			return
		}
		job.Status = appUpdateStatusFailed
		job.StatusText = "Failed"
		job.Error = err.Error()
		return
	}

	job.Status = appUpdateStatusDone
	job.Progress = 1
	if job.StatusText == "" {
		job.StatusText = "Complete"
	}
}

func downloadAppInstaller(ctx context.Context, job *appUpdateJob, release latestReleaseInfo, destDir string) error {
	if strings.TrimSpace(release.DownloadURL) == "" {
		return fmt.Errorf("download URL unavailable")
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("create update dir: %w", err)
	}

	filename := installerAssetName(release)
	if !isInstallerAssetName(filename) {
		return fmt.Errorf("invalid installer asset name %q", filename)
	}

	destPath := filepath.Join(destDir, filename)
	tmpPath := destPath + ".download"

	if err := netsec.ValidateProviderURL(release.DownloadURL, appInstallerURLValidation); err != nil {
		return fmt.Errorf("invalid installer url: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.DownloadURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := installerDownloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("download installer: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("installer download returned %d", resp.StatusCode)
	}

	if resp.ContentLength > 0 {
		job.mu.Lock()
		job.TotalBytes = resp.ContentLength
		job.mu.Unlock()
	}

	file, err := os.Create(tmpPath) //nolint:gosec // path is application config dir, not user-controlled input
	if err != nil {
		return fmt.Errorf("create installer temp file: %w", err)
	}

	buf := make([]byte, 64*1024)
	var bytesDone int64

	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				_ = file.Close()
				_ = os.Remove(tmpPath)
				return fmt.Errorf("write installer: %w", writeErr)
			}
			bytesDone += int64(n)
			job.mu.Lock()
			job.BytesDone = bytesDone
			if job.TotalBytes > 0 {
				job.Progress = float64(bytesDone) / float64(job.TotalBytes)
				job.StatusText = fmt.Sprintf("%.0f / %.0f MB", float64(bytesDone)/1e6, float64(job.TotalBytes)/1e6)
			} else {
				job.StatusText = fmt.Sprintf("%.0f MB", float64(bytesDone)/1e6)
			}
			job.mu.Unlock()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = file.Close()
			_ = os.Remove(tmpPath)
			return fmt.Errorf("read installer: %w", readErr)
		}
		if ctx.Err() != nil {
			_ = file.Close()
			_ = os.Remove(tmpPath)
			return ctx.Err()
		}
	}

	if err := file.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close installer: %w", err)
	}

	_ = os.Remove(destPath)
	if err := os.Rename(tmpPath, destPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("move installer into place: %w", err)
	}

	job.mu.Lock()
	job.FilePath = destPath
	job.StatusText = "Ready to install"
	job.mu.Unlock()

	return nil
}

func resolveAppUpdateDir(cfgPath string) string {
	if cfgPath != "" {
		return filepath.Join(filepath.Dir(cfgPath), "updates")
	}
	if exeDir := executableDir(); exeDir != "" {
		return filepath.Join(exeDir, "updates")
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		return filepath.Join(localAppData, "SpeechKit", "updates")
	}
	return filepath.Join(os.TempDir(), "SpeechKit", "updates")
}

func installerAssetName(release latestReleaseInfo) string {
	if release.DownloadName != "" {
		return filepath.Base(release.DownloadName)
	}
	if release.DownloadURL != "" {
		if name := filepath.Base(release.DownloadURL); name != "" && name != "." && name != "/" {
			return name
		}
	}
	return fmt.Sprintf("SpeechKit-Setup-v%s.exe", release.Version)
}

func isInstallerAssetName(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".exe" || ext == ".msi"
}
