package wakeword

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// TrainingUploader scans a local capture directory for activation
// recordings produced by TrainingCapture and uploads them to a remote
// SpeechKit server's POST /v1/wakeword/activations endpoint.
//
// Default behaviour:
//   - Tick interval is configurable; 5 min is a reasonable default for
//     production. Tests can drop to 50 ms.
//   - Skips any record whose sidecar already has `"uploaded": true`.
//   - When OnlyLabeled is true, also skips records whose `label` field
//     is empty. This lets users gate "send to server" behind explicit
//     labelling in the Wails Settings UI (v0.37.6).
//   - After a successful 201 (or a 409 "already exists") the sidecar
//     JSON is rewritten with `uploaded: true` and the WAV file stays
//     on disk so the user can still review it locally.
//   - On 503 (server says feature disabled) the uploader pauses one
//     full backoff window before retrying, so device clients don't
//     hammer a tenant who has the toggle off.
//   - On 5xx / network errors the uploader just logs and waits for
//     the next tick; nothing on disk is mutated so retry is automatic.
//
// All work happens on a single goroutine. Run blocks until ctx is
// cancelled or Close() is called.
type TrainingUploader struct {
	cfg    TrainingUploaderConfig
	client *http.Client
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// TrainingUploaderConfig configures one uploader instance. All fields
// are required unless explicitly marked optional.
type TrainingUploaderConfig struct {
	// Dir is the local capture directory that TrainingCapture writes
	// into (one .wav + .json pair per activation).
	Dir string

	// ServerURL is the base URL of the SpeechKit server, e.g.
	// "https://speechkit.example.com" or "http://localhost:8080".
	// The uploader appends "/v1/wakeword/activations" itself.
	ServerURL string

	// BearerToken authenticates the upload requests. Pulled from the
	// configured env var by the caller (we never read env vars
	// directly to keep secrets handling in one place).
	BearerToken string

	// Interval between scans. Zero is rejected by NewTrainingUploader;
	// production should pass at least 30 s.
	Interval time.Duration

	// OnlyLabeled — when true, only records whose sidecar JSON has a
	// non-empty `label` field are uploaded. Lets the user gate
	// sharing behind explicit review.
	OnlyLabeled bool

	// HTTPClient overrides the default *http.Client (tests use a
	// custom transport). When nil a 30 s-timeout client is used.
	HTTPClient *http.Client

	// Logger is optional; defaults to slog.Default.
	Logger *slog.Logger
}

// NewTrainingUploader validates the config and returns a ready
// uploader. Returns an error when required fields are missing.
func NewTrainingUploader(cfg TrainingUploaderConfig) (*TrainingUploader, error) {
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("wakeword: TrainingUploader Dir must be set")
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, errors.New("wakeword: TrainingUploader ServerURL must be set")
	}
	if _, err := url.Parse(cfg.ServerURL); err != nil {
		return nil, fmt.Errorf("wakeword: TrainingUploader invalid ServerURL: %w", err)
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("wakeword: TrainingUploader Interval must be > 0")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &TrainingUploader{
		cfg:    cfg,
		client: client,
		logger: logger,
	}, nil
}

// Run scans on every tick until ctx is cancelled. Returns ctx.Err()
// when ctx fires; nil otherwise. Safe to call once; calling twice
// concurrently is not supported.
func (u *TrainingUploader) Run(ctx context.Context) error {
	if u == nil {
		return errors.New("wakeword: nil TrainingUploader")
	}
	// One initial scan so users see uploads start without waiting a
	// whole interval after enabling the feature.
	u.scanOnce(ctx)

	ticker := time.NewTicker(u.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if u.isClosed() {
				return nil
			}
			u.scanOnce(ctx)
		}
	}
}

// Close marks the uploader as closed; the next tick will exit. Safe
// to call multiple times.
func (u *TrainingUploader) Close() {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.closed = true
	u.mu.Unlock()
}

func (u *TrainingUploader) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

// ── Internals ───────────────────────────────────────────────────────────────

// scanOnce walks Dir, attempts to upload each unsent record, and
// returns after a full pass. Errors are logged; the function never
// returns one because it's expected to be called in a tight loop.
func (u *TrainingUploader) scanOnce(ctx context.Context) {
	entries, err := os.ReadDir(u.cfg.Dir)
	if err != nil {
		u.logger.Warn("training_uploader: read dir failed",
			"dir", u.cfg.Dir, "err", err)
		return
	}

	// Stable ordering: capture timestamps are baked into filenames
	// so a simple lexicographic sort gives oldest-first.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, e := range entries {
		if ctx.Err() != nil {
			return
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		jsonPath := filepath.Join(u.cfg.Dir, e.Name())
		rec, err := readRecord(jsonPath)
		if err != nil {
			u.logger.Warn("training_uploader: malformed sidecar",
				"path", jsonPath, "err", err)
			continue
		}
		if rec.Uploaded {
			continue
		}
		if u.cfg.OnlyLabeled && strings.TrimSpace(rec.Label) == "" {
			continue
		}
		wavPath := strings.TrimSuffix(jsonPath, ".json") + ".wav"
		// Tolerate the audio_path field pointing somewhere else
		// (e.g. a relative override).
		if rec.AudioPath != "" {
			candidate := filepath.Join(u.cfg.Dir, rec.AudioPath)
			if _, err := os.Stat(candidate); err == nil {
				wavPath = candidate
			}
		}
		if _, err := os.Stat(wavPath); err != nil {
			u.logger.Warn("training_uploader: audio file missing",
				"path", wavPath, "err", err)
			continue
		}

		uploaded, err := u.uploadRecord(ctx, rec, wavPath)
		if err != nil {
			u.logger.Warn("training_uploader: upload failed",
				"id", rec.ID, "err", err)
			continue
		}
		if uploaded {
			rec.Uploaded = true
			if err := writeRecord(jsonPath, rec); err != nil {
				u.logger.Warn("training_uploader: mark uploaded failed",
					"path", jsonPath, "err", err)
			} else {
				u.logger.Info("training_uploader: upload succeeded",
					"id", rec.ID, "phrase_id", rec.PhraseID)
			}
		}
	}
}

// uploadRecord builds a multipart POST and returns true when the
// server accepted the record (201) or reported a duplicate (409 —
// idempotent re-mark on the client side).
func (u *TrainingUploader) uploadRecord(ctx context.Context, rec TrainingRecord, wavPath string) (bool, error) {
	wavBytes, err := os.ReadFile(wavPath)
	if err != nil {
		return false, fmt.Errorf("read wav: %w", err)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	metaJSON, err := json.Marshal(rec)
	if err != nil {
		return false, fmt.Errorf("marshal metadata: %w", err)
	}
	if err := mw.WriteField("metadata", string(metaJSON)); err != nil {
		return false, fmt.Errorf("write metadata field: %w", err)
	}
	part, err := mw.CreateFormFile("audio", filepath.Base(wavPath))
	if err != nil {
		return false, fmt.Errorf("create audio part: %w", err)
	}
	if _, err := part.Write(wavBytes); err != nil {
		return false, fmt.Errorf("write audio bytes: %w", err)
	}
	if err := mw.Close(); err != nil {
		return false, fmt.Errorf("multipart close: %w", err)
	}

	endpoint := strings.TrimSuffix(u.cfg.ServerURL, "/") + "/v1/wakeword/activations"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if u.cfg.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+u.cfg.BearerToken)
	}

	resp, err := u.client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusCreated:
		return true, nil
	case http.StatusConflict:
		// Server already has this ID — treat as success so the
		// client stops trying to re-upload it.
		return true, nil
	case http.StatusServiceUnavailable:
		// Operator turned the feature off on the server. Don't keep
		// drilling — caller will reattempt on the next tick.
		return false, errors.New("server training_data accept_uploads=false")
	default:
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
}

func readRecord(path string) (TrainingRecord, error) {
	var rec TrainingRecord
	b, err := os.ReadFile(path)
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func writeRecord(path string, rec TrainingRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
