// Package downloads manages model downloads for SpeechKit â€” HTTP file
// downloads and Ollama model pulls with progress tracking.
package downloads

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// DownloadURLValidation controls which URLs httpDownload accepts. Production
// default is strict (public https only). Tests relax this to allow loopback.
var DownloadURLValidation = netsec.ValidationOptions{}

// downloadClient fetches model files with a hardened TLS + redacting
// transport and a long-running timeout for large downloads.
var downloadClient = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Minute, DialValidation: &DownloadURLValidation})

// ollamaPullClient is used for streaming Ollama pulls (local loopback).
var ollamaPullClient = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Minute, DialValidation: &ollamaValidation})

// Kind identifies the download mechanism.
type Kind string

const (
	KindHTTP   Kind = "http"
	KindOllama Kind = "ollama"
)

// Status of a download job.
type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusDone      Status = "done"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// Item describes a model that can be pulled into SpeechKit.
type Item struct {
	ID             string `json:"id"`
	ProfileID      string `json:"profileId"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	SizeLabel      string `json:"sizeLabel"`
	SizeBytes      int64  `json:"sizeBytes"`
	Kind           Kind   `json:"kind"`
	URL            string `json:"url,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	OllamaModel    string `json:"ollamaModel,omitempty"`
	License        string `json:"license"`
	Available      bool   `json:"available"`
	Selected       bool   `json:"selected"`
	RuntimeReady   bool   `json:"runtimeReady,omitempty"`
	RuntimeProblem string `json:"runtimeProblem,omitempty"`
	Recommended    bool   `json:"recommended,omitempty"`
}

// JobView is the mutex-free snapshot used for JSON serialization.
type JobView struct {
	ID         string  `json:"id"`
	ModelID    string  `json:"modelId"`
	ProfileID  string  `json:"profileId"`
	Status     Status  `json:"status"`
	Progress   float64 `json:"progress"`
	BytesDone  int64   `json:"bytesDone"`
	TotalBytes int64   `json:"totalBytes"`
	StatusText string  `json:"statusText"`
	Error      string  `json:"error,omitempty"`
}

// job tracks progress of a single in-flight or completed download.
type job struct {
	mu         sync.Mutex
	ID         string
	ModelID    string
	ProfileID  string
	Status     Status
	Progress   float64
	BytesDone  int64
	TotalBytes int64
	StatusText string
	Error      string
	cancel     context.CancelFunc
}

func (j *job) snapshot() JobView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobView{
		ID:         j.ID,
		ModelID:    j.ModelID,
		ProfileID:  j.ProfileID,
		Status:     j.Status,
		Progress:   j.Progress,
		BytesDone:  j.BytesDone,
		TotalBytes: j.TotalBytes,
		StatusText: j.StatusText,
		Error:      j.Error,
	}
}

// Manager coordinates download jobs.
type Manager struct {
	mu   sync.Mutex
	jobs map[string]*job
}

// NewManager creates a Manager ready to track downloads.
func NewManager() *Manager {
	return &Manager{jobs: make(map[string]*job)}
}

// AllJobs returns a snapshot of every job (safe to JSON-encode).
func (m *Manager) AllJobs() []JobView {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobView, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j.snapshot())
	}
	return out
}

// Start queues and launches a download. onDone is called (in a goroutine) on success.
// Returns the initial job snapshot.
func (m *Manager) Start(item Item, destDir string, onDone func(Item)) JobView {
	id := fmt.Sprintf("dl-%d-%s", time.Now().UnixMilli(), randHex())
	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // cancel stored in struct field, called on job cancellation
	j := &job{
		ID:         id,
		ModelID:    item.ID,
		ProfileID:  item.ProfileID,
		Status:     StatusPending,
		TotalBytes: item.SizeBytes,
		StatusText: "Startingâ€¦",
		cancel:     cancel,
	}
	m.mu.Lock()
	m.jobs[id] = j
	m.mu.Unlock()
	snap := j.snapshot()
	go func() {
		m.run(ctx, j, item, destDir)
		if j.Status == StatusDone && onDone != nil {
			onDone(item)
		}
	}()
	return snap
}

// CancelJob stops an in-progress download by job ID.
func (m *Manager) CancelJob(jobID string) bool {
	m.mu.Lock()
	j, ok := m.jobs[jobID]
	m.mu.Unlock()
	if !ok || j.cancel == nil {
		return false
	}
	j.cancel()
	return true
}

func (m *Manager) run(ctx context.Context, j *job, item Item, destDir string) {
	j.mu.Lock()
	j.Status = StatusRunning
	j.StatusText = "Downloadingâ€¦"
	j.mu.Unlock()

	var err error
	switch item.Kind {
	case KindHTTP:
		err = httpDownload(ctx, j, item, destDir)
	case KindOllama:
		err = ollamaPull(ctx, j, item)
	default:
		err = fmt.Errorf("unsupported download kind: %s", item.Kind)
	}

	j.mu.Lock()
	if err != nil {
		if ctx.Err() != nil {
			j.Status = StatusCancelled
			j.StatusText = "Cancelled"
		} else {
			j.Status = StatusFailed
			j.Error = err.Error()
			j.StatusText = "Failed"
		}
	} else {
		j.Status = StatusDone
		j.Progress = 1.0
		j.StatusText = "Complete"
	}
	j.mu.Unlock()
}

func httpDownload(ctx context.Context, j *job, item Item, destDir string) error {
	expectedSHA256 := strings.ToLower(strings.TrimSpace(item.SHA256))
	if expectedSHA256 == "" {
		return fmt.Errorf("SHA256 is required for HTTP downloads")
	}

	// Validate URL with strict defaults: only public https.
	// Catalog URLs come from the hardcoded catalog or from a config source â€”
	// we still verify here so a malformed or SSRF-redirected URL never reaches
	// the HTTP layer.
	if err := netsec.ValidateProviderURL(item.URL, DownloadURLValidation); err != nil {
		return fmt.Errorf("invalid download url: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return fmt.Errorf("create model dir: %w", err)
	}
	filename := filepath.Base(item.URL)
	dest := filepath.Join(destDir, filename)
	tmp := dest + ".download"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, item.URL, http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d for %s", resp.StatusCode, item.URL)
	}

	if resp.ContentLength > 0 {
		j.mu.Lock()
		j.TotalBytes = resp.ContentLength
		j.mu.Unlock()
	}

	f, err := os.Create(tmp) //nolint:gosec // G304: tmp is app-controlled temp path, not user input
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	hasher := sha256.New()
	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(tmp)
				return fmt.Errorf("write: %w", writeErr)
			}
			hasher.Write(buf[:n])
			done += int64(n)
			j.mu.Lock()
			j.BytesDone = done
			total := j.TotalBytes
			if total > 0 {
				j.Progress = float64(done) / float64(total)
				j.StatusText = fmt.Sprintf("%.0f / %.0f MB", float64(done)/1e6, float64(total)/1e6)
			} else {
				j.StatusText = fmt.Sprintf("%.0f MB", float64(done)/1e6)
			}
			j.mu.Unlock()
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return fmt.Errorf("read: %w", readErr)
		}
		if ctx.Err() != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
			return ctx.Err()
		}
	}
	_ = f.Close()

	got := hex.EncodeToString(hasher.Sum(nil))
	if got != expectedSHA256 {
		_ = os.Remove(tmp)
		return fmt.Errorf("SHA256 mismatch: expected %s, got %s â€” file corrupt or tampered", expectedSHA256, got)
	}

	return os.Rename(tmp, dest)
}

type ollamaLine struct {
	Status    string `json:"status"`
	Total     int64  `json:"total"`
	Completed int64  `json:"completed"`
	Error     string `json:"error"`
}

func ollamaPull(ctx context.Context, j *job, item Item) error {
	endpoint, err := netsec.BuildEndpoint(OllamaBaseURL, "api/pull", ollamaValidation)
	if err != nil {
		return fmt.Errorf("ollama endpoint: %w", err)
	}
	payload, _ := json.Marshal(map[string]any{"model": item.OllamaModel, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ollamaPullClient.Do(req)
	if err != nil {
		return fmt.Errorf("ollama not reachable â€” is Ollama running? (%w)", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned %d â€” is Ollama installed and running?", resp.StatusCode)
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var line ollamaLine
		if decErr := dec.Decode(&line); decErr == io.EOF {
			break
		} else if decErr != nil {
			return fmt.Errorf("parse ollama stream: %w", decErr)
		}
		if line.Error != "" {
			return fmt.Errorf("ollama: %s", line.Error)
		}
		j.mu.Lock()
		if line.Total > 0 {
			j.TotalBytes = line.Total
			j.BytesDone = line.Completed
			j.Progress = float64(line.Completed) / float64(line.Total)
			j.StatusText = fmt.Sprintf("%s â€” %.0f%%", line.Status, j.Progress*100)
		} else if line.Status != "" {
			j.StatusText = line.Status
		}
		j.mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// randHex returns a short random hex string for job IDs.
func randHex() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano()&0xffffff)
	}
	return fmt.Sprintf("%x", b)
}
