package training

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

	"github.com/kombifyio/SpeechKit/pkg/speechkit/internal/logutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// Uploader scans a local capture directory for activation recordings
// produced by Capture and uploads them to a remote SpeechKit server's
// POST /v1/wakeword/activations endpoint.
//
// Behaviour:
//   - The scan interval is configurable; 5 min is a reasonable production
//     default, tests can drop to 50 ms.
//   - Records whose sidecar already has `"uploaded": true` are skipped.
//   - When OnlyLabeled is true, records whose `label` field is empty are
//     skipped too, so a host can gate "send to server" behind explicit
//     labelling.
//   - After a 201 (or a 409 "already exists") the sidecar JSON is rewritten
//     with `uploaded: true`; the WAV stays on disk for local review.
//   - On 503 (server has the feature disabled) and on 5xx / network errors
//     the uploader logs and waits for the next tick; nothing on disk is
//     mutated, so retry is automatic.
//
// All work happens on the goroutine that calls Run, which blocks until ctx
// is cancelled or Close is called.
type Uploader struct {
	cfg    UploaderConfig
	client *http.Client
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// UploaderConfig configures one Uploader. Fields are required unless marked
// optional.
type UploaderConfig struct {
	// Dir is the local capture directory that Capture writes into (one .wav
	// + .json pair per activation).
	Dir string

	// ServerURL is the base URL of the SpeechKit server, e.g.
	// "https://speechkit.example.com". The uploader appends
	// "/v1/wakeword/activations" itself.
	ServerURL string

	// BearerToken authenticates the upload requests. Optional; the caller
	// resolves it from its own secret source so secrets handling stays in
	// one place.
	BearerToken string

	// Interval between scans. Zero is rejected by NewUploader; production
	// should pass at least 30 s.
	Interval time.Duration

	// OnlyLabeled uploads only records whose sidecar JSON has a non-empty
	// `label` field.
	OnlyLabeled bool

	// HTTPClient overrides the default *http.Client (optional; tests use a
	// custom transport). When nil a 30 s-timeout netsec client is used that
	// validates every dial target against the URL policy (see Validation)
	// and follows no redirects. A caller-supplied client is used as is.
	HTTPClient *http.Client

	// Validation optionally replaces the default URL policy for ServerURL
	// (also applied to every dial by the default client). The default
	// accepts https to any host and plain http only to loopback or
	// private-network addresses, because the clips are raw microphone audio
	// and the request carries a bearer token. Hosts pass their network-scope
	// policy here (e.g. local-network-only).
	Validation *netsec.ValidationOptions

	// Logger is optional; defaults to slog.Default.
	Logger *slog.Logger
}

// NewUploader validates cfg and returns a ready uploader.
func NewUploader(cfg UploaderConfig) (*Uploader, error) {
	if strings.TrimSpace(cfg.Dir) == "" {
		return nil, errors.New("training: uploader Dir must be set")
	}
	if strings.TrimSpace(cfg.ServerURL) == "" {
		return nil, errors.New("training: uploader ServerURL must be set")
	}
	validation, err := uploadURLPolicy(cfg)
	if err != nil {
		return nil, err
	}
	if err := netsec.ValidateProviderURL(strings.TrimSpace(cfg.ServerURL), validation); err != nil {
		return nil, fmt.Errorf("training: uploader ServerURL rejected: %w", err)
	}
	if cfg.Interval <= 0 {
		return nil, errors.New("training: uploader Interval must be > 0")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = netsec.NewSafeHTTPClient(netsec.ClientOptions{
			Timeout:        30 * time.Second,
			DialValidation: &validation,
		})
		// Never follow a redirect: it would re-send the clip and the bearer
		// token to a host nobody configured.
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return errors.New("training: uploader does not follow redirects")
		}
	}
	logger := cfg.Logger
	if logger == nil {
		logger = logutil.Resolve(nil)
	}
	return &Uploader{
		cfg:    cfg,
		client: client,
		logger: logger,
	}, nil
}

// uploadURLPolicy returns the URL / dial policy for cfg.ServerURL: the
// caller's Validation, or the default (https anywhere, plain http only to
// loopback and private addresses).
func uploadURLPolicy(cfg UploaderConfig) (netsec.ValidationOptions, error) {
	if cfg.Validation != nil {
		return *cfg.Validation, nil
	}
	u, err := url.Parse(strings.TrimSpace(cfg.ServerURL))
	if err != nil {
		return netsec.ValidationOptions{}, fmt.Errorf("training: uploader invalid ServerURL: %w", err)
	}
	if strings.EqualFold(u.Scheme, "http") {
		return netsec.ValidationOptions{AllowLoopback: true, AllowPrivate: true, AllowHTTP: true, RequireLocal: true}, nil
	}
	return netsec.ValidationOptions{AllowLoopback: true, AllowPrivate: true}, nil
}

// Run scans once immediately and then on every tick until ctx is cancelled
// (returning ctx.Err()) or Close is called (returning nil). Call it once;
// concurrent Run calls are not supported.
func (u *Uploader) Run(ctx context.Context) error {
	if u == nil {
		return errors.New("training: nil Uploader")
	}
	// One initial scan so users see uploads start without waiting a whole
	// interval after enabling the feature.
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

// Close marks the uploader as closed; the next tick makes Run return. Safe
// to call multiple times.
func (u *Uploader) Close() {
	if u == nil {
		return
	}
	u.mu.Lock()
	u.closed = true
	u.mu.Unlock()
}

func (u *Uploader) isClosed() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.closed
}

// scanOnce walks Dir, attempts to upload each unsent record, and returns
// after a full pass. Errors are logged, never returned, because the caller
// runs it in a loop.
func (u *Uploader) scanOnce(ctx context.Context) {
	entries, err := os.ReadDir(u.cfg.Dir)
	if err != nil {
		u.logger.Warn("training_uploader: read dir failed",
			"dir", u.cfg.Dir, "err", err)
		return
	}

	// Stable ordering: capture timestamps are baked into filenames so a
	// simple lexicographic sort gives oldest-first.
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
		// Tolerate the audio_path field naming another file in the capture
		// directory, but never one outside it: the sidecar JSON is plain
		// data on disk and must not be able to select any readable file
		// for upload.
		if rec.AudioPath != "" {
			candidate := filepath.Join(u.cfg.Dir, rec.AudioPath)
			if filepath.IsAbs(rec.AudioPath) || !pathWithin(u.cfg.Dir, candidate) {
				u.logger.Warn("training_uploader: audio_path outside the capture dir — record skipped",
					"id", rec.ID)
				continue
			}
			if _, err := os.Stat(candidate); err == nil {
				wavPath = candidate
			}
		}
		if _, err := os.Stat(wavPath); err != nil {
			u.logger.Warn("training_uploader: audio file missing",
				"path", wavPath, "err", err)
			continue
		}
		if !resolvedWithin(u.cfg.Dir, wavPath) {
			u.logger.Warn("training_uploader: audio file resolves outside the capture dir — record skipped",
				"id", rec.ID)
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

// uploadRecord builds a multipart POST and returns true when the server
// accepted the record (201) or reported a duplicate (409, which marks the
// record uploaded on the client side too).
func (u *Uploader) uploadRecord(ctx context.Context, rec Record, wavPath string) (bool, error) {
	wavBytes, err := os.ReadFile(wavPath) // #nosec G304 -- wavPath comes from enumerating the configured local wakeword capture directory.
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
		// Server already has this ID; treat as success so the client stops
		// trying to re-upload it.
		return true, nil
	case http.StatusServiceUnavailable:
		// Operator turned the feature off on the server. Don't keep drilling;
		// the caller reattempts on the next tick.
		return false, errors.New("server training_data accept_uploads=false")
	default:
		respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
}

// pathWithin reports whether path, cleaned, is dir or below it.
func pathWithin(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// resolvedWithin is pathWithin after resolving symlinks on both sides, so a
// link planted in the capture dir cannot point the upload elsewhere.
func resolvedWithin(dir, path string) bool {
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false
	}
	return pathWithin(realDir, realPath)
}

func readRecord(path string) (Record, error) {
	var rec Record
	b, err := os.ReadFile(path) // #nosec G304 -- path comes from enumerating the configured local wakeword capture directory.
	if err != nil {
		return rec, err
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func writeRecord(path string, rec Record) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
