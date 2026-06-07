//go:build linux

// Package wakewordtraining mounts the v0.37.5 REST endpoints that
// accept wake-word activation training-data uploads from device
// clients. The endpoints are gated behind
// `[server.training_data].accept_uploads` — when false the mount
// returns 503 with a clear "feature disabled" payload so the
// device-side uploader can back off gracefully.
//
// All endpoints scope every read and write to the caller's
// Identity{UserID, OrgID} (see internal/server/middleware/auth.go).
// One tenant cannot see or delete another tenant's recordings even
// when the audio dir is shared across organisations.
//
// See docs/wakeword-training-data.md for the data shape, privacy
// contract, and end-to-end flow.
package wakewordtraining

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
)

// maxWakewordIDLen caps the client-supplied activation ID. Even on
// filesystems that accept arbitrarily long names (ext4: 255 bytes,
// NTFS: 255 chars), an attacker with a writeable upload surface could
// burn disk via padding alone — and the ID becomes part of the audio
// filename. 64 chars covers UUIDs, slug-style IDs, and short hashes.
const maxWakewordIDLen = 64

// wakewordIDPattern locks the activation ID to a small alphabet that
// is safe on every filesystem (no slashes, dots, spaces, control
// chars) and small enough that path-resolution can be reasoned about
// without surprises.
var wakewordIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// Options configures the handler.
type Options struct {
	// Store satisfies the WakewordActivationStore extension. Nil
	// makes the mount return 503 because there's no persistence
	// layer available.
	Store store.WakewordActivationStore

	// AudioDir is the filesystem root under which uploaded WAV
	// bytes are written. Files land at
	// <AudioDir>/<owner_org>/<owner_user>/<activation_id>.wav.
	// Must already exist + be writable.
	AudioDir string

	// MaxUploadBytes caps the size of a single uploaded audio
	// part. Keeps a misbehaving client from filling the disk in
	// one request. Default 10 MiB.
	MaxUploadBytes int64

	// PerUserQuotaBytes — when > 0, uploads from a user whose
	// existing sum-of-audio-bytes exceeds this value are rejected
	// with 413. Set via [server.training_data].per_user_quota_bytes.
	PerUserQuotaBytes int64

	// AcceptUploads gates the entire feature. When false, every
	// endpoint returns 503. Mirrors the
	// [server.training_data].accept_uploads config knob.
	AcceptUploads bool

	// Now overrides time.Now for tests.
	Now func() time.Time
}

// Handler is the HTTP mount.
type Handler struct {
	store             store.WakewordActivationStore
	audioDir          string
	maxUploadBytes    int64
	perUserQuotaBytes int64
	acceptUploads     bool
	now               func() time.Time
	// quotaLocks serialises the (quota-read → write-file → save-DB)
	// section per OwnerUserID so two concurrent uploads from the same
	// user cannot both pass a stale "below quota" check and silently
	// blow past the cap. Empty value is a sync.Map ready for use.
	// Multi-pod deployments need a DB-level reserve; documented as
	// out-of-scope for OSS v1 in audit S-6.
	quotaLocks sync.Map
}

// lockOwner acquires the per-owner mutex. The mutex is created lazily on
// first use and lives for the process lifetime — memory is bounded by
// the user count, not by request count. Callers MUST defer the returned
// unlock func.
func (h *Handler) lockOwner(orgID, userID string) func() {
	key := orgID + "/" + userID
	actual, _ := h.quotaLocks.LoadOrStore(key, &sync.Mutex{})
	m, ok := actual.(*sync.Mutex)
	if !ok {
		// Defensive: an impossible-state corruption can't be allowed to
		// crash a request handler. Return a no-op unlock so the upload
		// proceeds without the mutex — worse than expected (potential
		// race window) but safer than a panic in production.
		return func() {}
	}
	m.Lock()
	return m.Unlock
}

// New validates the options and returns a ready-to-mount handler.
// When AcceptUploads is false the returned handler still mounts the
// routes; every request gets 503 so device-side uploaders can detect
// and back off.
func New(opts Options) (*Handler, error) {
	h := &Handler{
		store:             opts.Store,
		audioDir:          strings.TrimSpace(opts.AudioDir),
		maxUploadBytes:    opts.MaxUploadBytes,
		perUserQuotaBytes: opts.PerUserQuotaBytes,
		acceptUploads:     opts.AcceptUploads,
		now:               opts.Now,
	}
	if h.now == nil {
		h.now = time.Now
	}
	if h.maxUploadBytes <= 0 {
		h.maxUploadBytes = 10 << 20 // 10 MiB
	}
	if h.acceptUploads {
		if h.store == nil {
			return nil, errors.New("wakewordtraining: AcceptUploads=true requires a Store")
		}
		if h.audioDir == "" {
			return nil, errors.New("wakewordtraining: AcceptUploads=true requires AudioDir")
		}
		if err := os.MkdirAll(h.audioDir, 0o700); err != nil {
			return nil, fmt.Errorf("wakewordtraining: ensure audio dir: %w", err)
		}
	}
	return h, nil
}

// Mount wires the handler onto mux.
//
//	POST    /v1/wakeword/activations             — multipart upload
//	GET     /v1/wakeword/activations             — list caller's activations
//	GET     /v1/wakeword/activations/{id}        — fetch one
//	GET     /v1/wakeword/activations/{id}/audio  — fetch WAV bytes
//	PATCH   /v1/wakeword/activations/{id}        — update label
//	DELETE  /v1/wakeword/activations/{id}        — delete
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/v1/wakeword/activations", h.collection)
	mux.HandleFunc("/v1/wakeword/activations/", h.item)
}

// ── handlers ────────────────────────────────────────────────────────────────

func (h *Handler) collection(w http.ResponseWriter, r *http.Request) {
	if !h.acceptUploads {
		h.writeDisabled(w)
		return
	}
	switch r.Method {
	case http.MethodPost:
		h.upload(w, r)
	case http.MethodGet:
		h.list(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"only GET and POST are accepted at this endpoint")
	}
}

func (h *Handler) item(w http.ResponseWriter, r *http.Request) {
	if !h.acceptUploads {
		h.writeDisabled(w)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/wakeword/activations/")
	id, subresource := splitItemPath(path)
	if id == "" {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "activation id missing from path")
		return
	}
	switch {
	case subresource == "audio" && r.Method == http.MethodGet:
		h.getAudio(w, r, id)
	case subresource == "" && r.Method == http.MethodGet:
		h.getMetadata(w, r, id)
	case subresource == "" && r.Method == http.MethodPatch:
		h.patchLabel(w, r, id)
	case subresource == "" && r.Method == http.MethodDelete:
		h.delete(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"unsupported method or sub-resource")
	}
}

// uploadMetadata is the JSON part of the multipart upload. Field
// names mirror the wakeword.TrainingRecord JSON sidecar produced by
// the sidecar's capture pipeline so the client-side uploader can
// re-use the bytes verbatim.
type uploadMetadata struct {
	ID         string    `json:"id"`
	PhraseID   string    `json:"phrase_id"`
	Phrase     string    `json:"phrase"`
	Backend    string    `json:"backend"`
	Score      float64   `json:"score"`
	CapturedAt time.Time `json:"captured_at"`
	PreRollMs  int       `json:"pre_roll_ms"`
	PostRollMs int       `json:"post_roll_ms"`
	SampleRate int       `json:"sample_rate"`
	Label      string    `json:"label,omitempty"`
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadBytes+(64<<10))
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_multipart",
			"failed to parse multipart body: "+err.Error())
		return
	}

	metadataPart := strings.TrimSpace(r.FormValue("metadata"))
	if metadataPart == "" {
		httpx.WriteError(w, http.StatusBadRequest, "missing_metadata",
			"multipart body must include a 'metadata' JSON form field")
		return
	}
	var meta uploadMetadata
	if err := json.Unmarshal([]byte(metadataPart), &meta); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_metadata",
			"metadata field must be valid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(meta.ID) == "" || strings.TrimSpace(meta.PhraseID) == "" {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_metadata",
			"metadata.id and metadata.phrase_id are required")
		return
	}
	// Audit S-6: cap and constrain the client-supplied ID before it
	// becomes part of the audio filename and the DB primary key.
	// Without this, an attacker can request a 64-KB ID, which fails
	// on some filesystems and burns disk via padding alone.
	if len(meta.ID) > maxWakewordIDLen {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_metadata",
			fmt.Sprintf("metadata.id must be at most %d characters", maxWakewordIDLen))
		return
	}
	if !wakewordIDPattern.MatchString(meta.ID) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_metadata",
			"metadata.id must match ^[A-Za-z0-9_-]{1,64}$")
		return
	}
	if !store.ValidWakewordLabel(meta.Label) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_label",
			"metadata.label must be 'correct', 'false_positive', or omitted")
		return
	}
	if meta.CapturedAt.IsZero() {
		meta.CapturedAt = h.now()
	}
	if meta.SampleRate == 0 {
		meta.SampleRate = 16000
	}

	// Audit S-6: serialise quota-check -> write-file -> save-DB per
	// owner. Without this lock two concurrent uploads from the same
	// user can both read "used < quota", both write, and both succeed,
	// silently doubling the per-user storage budget. Released after
	// SaveWakewordActivation either commits or the error path
	// returns. Single-process scope; multi-pod fix (DB-level
	// SELECT FOR UPDATE reserve) deferred per audit out-of-scope.
	unlock := h.lockOwner(id.OrgID, id.UserID)
	defer unlock()

	// Quota check before reading audio bytes — fails fast.
	if h.perUserQuotaBytes > 0 {
		used, err := h.store.SumWakewordActivationBytesForUser(r.Context(), id.UserID, id.OrgID)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "quota_check_failed", err.Error())
			return
		}
		if used >= h.perUserQuotaBytes {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "quota_exceeded",
				fmt.Sprintf("user has %d bytes stored, quota is %d", used, h.perUserQuotaBytes))
			return
		}
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "missing_audio",
			"multipart body must include an 'audio' file part")
		return
	}
	defer func() { _ = file.Close() }()
	if header.Size > 0 && header.Size > h.maxUploadBytes {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large",
			fmt.Sprintf("audio file %d bytes exceeds maximum %d bytes", header.Size, h.maxUploadBytes))
		return
	}

	// Persist audio to <audio_dir>/<org>/<user>/<id>.wav. We use the
	// client-supplied ID as filename so the row + filesystem stay
	// trivially co-located.
	relPath := filepath.Join(safePathSegment(id.OrgID), safePathSegment(id.UserID), safePathSegment(meta.ID)+".wav")
	absPath, err := h.audioPath(relPath)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_audio_path", err.Error())
		return
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "storage_init_failed", err.Error())
		return
	}
	out, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- absPath is validated by audioPath to stay under Handler.audioDir.
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			httpx.WriteError(w, http.StatusConflict, "duplicate_activation",
				fmt.Sprintf("activation id %q already exists for this owner", meta.ID))
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "storage_write_failed", err.Error())
		return
	}
	n, err := io.Copy(out, file)
	_ = out.Close()
	if err != nil {
		_ = os.Remove(absPath)
		httpx.WriteError(w, http.StatusInternalServerError, "storage_write_failed", err.Error())
		return
	}

	row := store.WakewordActivation{
		ID:           meta.ID,
		OwnerUserID:  id.UserID,
		OwnerOrgID:   id.OrgID,
		PhraseID:     meta.PhraseID,
		Phrase:       meta.Phrase,
		Backend:      meta.Backend,
		Score:        meta.Score,
		CapturedAt:   meta.CapturedAt,
		UploadedAt:   h.now(),
		Label:        meta.Label,
		AudioPath:    relPath,
		AudioBytes:   n,
		SampleRate:   meta.SampleRate,
		PreRollMs:    meta.PreRollMs,
		PostRollMs:   meta.PostRollMs,
		MetadataJSON: metadataPart,
	}
	saved, err := h.store.SaveWakewordActivation(r.Context(), row)
	if err != nil {
		// Persistence failed; unlink the orphaned audio file so the
		// next upload doesn't trip the O_EXCL duplicate check.
		_ = os.Remove(absPath)
		httpx.WriteError(w, http.StatusInternalServerError, "store_save_failed", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(saved)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	id := middleware.IdentityFromContext(r.Context())
	if id.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := h.store.ListWakewordActivations(r.Context(), id.UserID, id.OrgID, store.ListOpts{Limit: limit})
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"activations": rows,
		"count":       len(rows),
	})
}

func (h *Handler) getMetadata(w http.ResponseWriter, r *http.Request, id string) {
	caller := middleware.IdentityFromContext(r.Context())
	if caller.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	row, err := h.store.GetWakewordActivation(r.Context(), id, caller.UserID, caller.OrgID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(row)
}

func (h *Handler) getAudio(w http.ResponseWriter, r *http.Request, id string) {
	caller := middleware.IdentityFromContext(r.Context())
	if caller.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	row, err := h.store.GetWakewordActivation(r.Context(), id, caller.UserID, caller.OrgID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	absPath, err := h.audioPath(row.AudioPath)
	if err != nil {
		httpx.WriteError(w, http.StatusGone, "audio_missing", "stored audio path is invalid")
		return
	}
	f, err := os.Open(absPath) // #nosec G304 -- absPath is validated by audioPath to stay under Handler.audioDir.
	if err != nil {
		httpx.WriteError(w, http.StatusGone, "audio_missing", "audio file no longer on disk")
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(row.AudioPath)))
	_, _ = io.Copy(w, f)
}

type patchLabelRequest struct {
	Label string `json:"label"`
}

func (h *Handler) patchLabel(w http.ResponseWriter, r *http.Request, id string) {
	caller := middleware.IdentityFromContext(r.Context())
	if caller.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	var req patchLabelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if !store.ValidWakewordLabel(req.Label) {
		httpx.WriteError(w, http.StatusBadRequest, "invalid_label",
			"label must be 'correct', 'false_positive', or empty")
		return
	}
	if err := h.store.UpdateWakewordActivationLabel(r.Context(), id, caller.UserID, caller.OrgID, req.Label); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, id string) {
	caller := middleware.IdentityFromContext(r.Context())
	if caller.UserID == "" {
		httpx.WriteError(w, http.StatusUnauthorized, "unauthenticated", "identity not available on context")
		return
	}
	audioPath, err := h.store.DeleteWakewordActivation(r.Context(), id, caller.UserID, caller.OrgID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if audioPath != "" {
		if absPath, err := h.audioPath(audioPath); err == nil {
			_ = os.Remove(absPath) // #nosec G703 -- absPath is validated by audioPath to stay under Handler.audioDir.
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── helpers ────────────────────────────────────────────────────────────────

func (h *Handler) writeDisabled(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusServiceUnavailable, "training_data_disabled",
		"[server.training_data].accept_uploads is false; ask the operator to enable it")
}

// splitItemPath separates "{id}" from "{id}/{subresource}" in the
// request path.
func splitItemPath(path string) (id, subresource string) {
	if slash := strings.Index(path, "/"); slash >= 0 {
		return path[:slash], path[slash+1:]
	}
	return path, ""
}

// safePathSegment scrubs client-supplied IDs down to a strictly
// alphanumeric+`-`+`_` token. Dots are stripped because keeping them
// lets the literal `..` parent-traversal token survive and the
// segment is meant to be a flat filename component, not a path. The
// `.wav` extension is appended by the handler itself after this
// scrub. Empty results become "_" so the join never produces a
// hidden file on POSIX.
func safePathSegment(s string) string {
	s = strings.TrimSpace(s)
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-', c == '_':
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return "_"
	}
	return string(out)
}

func (h *Handler) audioPath(rel string) (string, error) {
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("audio path %q escapes audio_dir", rel)
	}
	baseAbs, err := filepath.Abs(h.audioDir)
	if err != nil {
		return "", fmt.Errorf("resolve audio_dir: %w", err)
	}
	targetAbs, err := filepath.Abs(filepath.Join(baseAbs, rel))
	if err != nil {
		return "", fmt.Errorf("resolve audio path: %w", err)
	}
	within, err := pathWithinBase(baseAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if !within {
		return "", fmt.Errorf("audio path %q escapes audio_dir", rel)
	}
	return targetAbs, nil
}

func pathWithinBase(baseAbs, targetAbs string) (bool, error) {
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return false, fmt.Errorf("compare audio path: %w", err)
	}
	return rel != "." && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)), nil
}

// writeStoreError maps the small set of store errors we care about
// to HTTP status codes. Anything else is 500.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrInvalidWakewordActivation):
		httpx.WriteError(w, http.StatusBadRequest, "invalid_activation", err.Error())
	case errors.Is(err, sql.ErrNoRows):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "activation not found for this owner")
	case errors.Is(err, context.DeadlineExceeded):
		httpx.WriteError(w, http.StatusGatewayTimeout, "store_timeout", err.Error())
	default:
		httpx.WriteError(w, http.StatusInternalServerError, "store_error", err.Error())
	}
}
