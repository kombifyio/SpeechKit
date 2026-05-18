package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/auditlogtest"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// newPrivacyTestStore opens a fresh in-process SQLite store in the test's
// temp directory and seeds it with data for the given scope.
func newPrivacyTestStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "privacy_http_test.db")
	s, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath:        dbPath,
		SaveAudio:         false,
		MaxAudioStorageMB: 0,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func seedTranscription(t *testing.T, s store.Store, scope speechstorage.Scope, text string) {
	t.Helper()
	ctx := speechstorage.WithScope(t.Context(), scope)
	if err := s.SaveTranscription(ctx, text, "en", "test-provider", "test-model", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription(%q): %v", text, err)
	}
}

func postPrivacy(t *testing.T, handler http.Handler, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestPrivacyExportReturnsScopedJSON verifies that POST /api/v1/privacy/export
// returns all scoped records as a JSON payload.
func TestPrivacyExportReturnsScopedJSON(t *testing.T) {
	s := newPrivacyTestStore(t)
	// Use the same scope the handler produces for scope=user + user_sid=export-user.
	scope := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "export-user"}
	seedTranscription(t, s, scope, "export test transcription")

	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope":    "user",
		"user_sid": "export-user",
		"format":   "json",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp struct {
		Export *store.ScopeExport `json:"export"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.Export == nil {
		t.Fatal("export is nil in response")
	}
	if len(resp.Export.Transcriptions) != 1 {
		t.Errorf("Transcriptions: want 1, got %d", len(resp.Export.Transcriptions))
	}
	if resp.Export.Transcriptions[0].Text != "export test transcription" {
		t.Errorf("Transcription text = %q, want %q", resp.Export.Transcriptions[0].Text, "export test transcription")
	}
}

// TestPrivacyExportRejectsUnknownScope verifies that an unknown scope value
// returns HTTP 400.
func TestPrivacyExportRejectsUnknownScope(t *testing.T) {
	s := newPrivacyTestStore(t)
	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope": "galaxy",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestPrivacyExportRejectsUserScopeWithoutSID verifies that scope=user without
// user_sid returns HTTP 400.
func TestPrivacyExportRejectsUserScopeWithoutSID(t *testing.T) {
	s := newPrivacyTestStore(t)
	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope": "user",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestPrivacyDeleteRequiresConfirm verifies that the delete endpoint returns
// HTTP 400 when confirm is absent or false.
func TestPrivacyDeleteRequiresConfirm(t *testing.T) {
	s := newPrivacyTestStore(t)
	handler := handlePrivacyDelete(s)

	// No confirm field at all.
	rec := postPrivacy(t, handler, "/api/v1/privacy/delete", map[string]any{
		"scope": "install",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("missing confirm: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rec.Body.String(), "confirm") {
		t.Errorf("missing confirm: response body should mention 'confirm', got %q", rec.Body.String())
	}

	// Explicit false.
	rec = postPrivacy(t, handler, "/api/v1/privacy/delete", map[string]any{
		"scope":   "install",
		"confirm": false,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("confirm=false: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

// TestPrivacyDeleteEmitsAuditEvent verifies that a successful delete emits a
// privacy.delete event in the audit log with outcome=success.
func TestPrivacyDeleteEmitsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	s := newPrivacyTestStore(t)
	scope := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "delete-user"}
	seedTranscription(t, s, scope, "data to erase")

	handler := handlePrivacyDelete(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/delete", map[string]any{
		"scope":    "user",
		"user_sid": "delete-user",
		"confirm":  true,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	var resp struct {
		RowsDeleted int    `json:"rows_deleted"`
		ScopeKey    string `json:"scope_key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.RowsDeleted < 1 {
		t.Errorf("rows_deleted: want >= 1, got %d", resp.RowsDeleted)
	}

	// Verify audit log contains privacy.delete with outcome=success.
	dateKey := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(dir, "audit-"+dateKey+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"privacy.delete"`) {
		t.Errorf("want privacy.delete event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"outcome":"success"`) {
		t.Errorf("want outcome=success in audit log, got:\n%s", data)
	}
}

// TestPrivacyExportEmitsAuditEvent verifies that a successful export emits a
// privacy.export event in the audit log with outcome=success.
func TestPrivacyExportEmitsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	s := newPrivacyTestStore(t)
	scope := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "audit-export-user"}
	seedTranscription(t, s, scope, "audit export data")

	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope":    "user",
		"user_sid": "audit-export-user",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	dateKey := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(dir, "audit-"+dateKey+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	if !strings.Contains(string(data), `"event":"privacy.export"`) {
		t.Errorf("want privacy.export event in audit log, got:\n%s", data)
	}
	if !strings.Contains(string(data), `"outcome":"success"`) {
		t.Errorf("want outcome=success in audit log, got:\n%s", data)
	}
}

// TestPrivacyDeleteRemovesDataAndResponds200 is an end-to-end test that seeds
// data, deletes it, and verifies both the response and that the data is gone.
func TestPrivacyDeleteRemovesDataAndResponds200(t *testing.T) {
	s := newPrivacyTestStore(t)
	scope := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: "e2e-delete-user"}
	seedTranscription(t, s, scope, "to be erased")

	handler := handlePrivacyDelete(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/delete", map[string]any{
		"scope":    "user",
		"user_sid": "e2e-delete-user",
		"confirm":  true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	// Data must be gone after deletion.
	export, err := s.ExportScope(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExportScope after delete: %v", err)
	}
	if len(export.Transcriptions) != 0 {
		t.Errorf("transcriptions should be gone after delete: %d remain", len(export.Transcriptions))
	}
}

// TestPrivacyExportInstallScope verifies that scope=install works (no user_sid
// needed) and returns the default-scope data.
func TestPrivacyExportInstallScope(t *testing.T) {
	s := newPrivacyTestStore(t)
	// The default scope is install:local — SaveTranscription without an
	// explicit scope context uses it.
	if err := s.SaveTranscription(t.Context(), "install-scope-data", "en", "p", "m", 1000, 50, nil); err != nil {
		t.Fatalf("SaveTranscription: %v", err)
	}

	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope": "install",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}

	var resp struct {
		Export *store.ScopeExport `json:"export"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Export == nil {
		t.Fatal("export is nil")
	}
	if len(resp.Export.Transcriptions) < 1 {
		t.Errorf("Transcriptions: want >= 1, got %d", len(resp.Export.Transcriptions))
	}
}

// TestPrivacyExportZipFormat verifies that format=zip returns a zip archive
// with Content-Type application/zip.
func TestPrivacyExportZipFormat(t *testing.T) {
	s := newPrivacyTestStore(t)

	handler := handlePrivacyExport(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/export", map[string]any{
		"scope":  "install",
		"format": "zip",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
}

// readAuditLines returns all lines in today's audit log that contain the given
// event string. Helper to avoid repetition in audit-check tests.
func readAuditLines(t *testing.T, dir string) []string {
	t.Helper()
	dateKey := time.Now().UTC().Format("2006-01-02")
	path := filepath.Join(dir, "audit-"+dateKey+".log")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer f.Close() //nolint:errcheck // test helper, read-only close
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines
}

// newPrivacyTestStoreWithAudio creates an SQLite store configured to save
// audio, seeds a transcription with real audio bytes, then returns the store
// and the path of the audio file written to disk.
//
// The audio dir is <dbdir>/audio — a sibling of the SQLite file. The test
// uses t.TempDir() for both dbPath and the audio bytes so everything is
// cleaned up after the test (but we verify the file is unlinked before then).
func newPrivacyTestStoreWithAudio(t *testing.T, userSID string) (*store.SQLiteStore, string) {
	t.Helper()
	// Put the DB in a dedicated temp dir; SaveTranscriptionWithAudio writes
	// audio to <dbdir>/audio/*.wav so we can find the file after seeding.
	dbDir := t.TempDir()
	dbPath := filepath.Join(dbDir, "privacy_audio_test.db")
	s, err := store.NewSQLiteStore(store.StoreConfig{
		SQLitePath:        dbPath,
		SaveAudio:         true,
		MaxAudioStorageMB: 100,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	scope := speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: userSID}
	ctx := speechstorage.WithScope(t.Context(), scope)

	// Save a transcription with actual audio bytes so the store writes a real
	// audio file and creates an audio_assets row with its path.
	audioInput := store.AudioAssetInput{
		Data:       []byte("RIFF....fake-wav-content"),
		MimeType:   "audio/wav",
		Extension:  ".wav",
		DurationMs: 500,
	}
	if err := s.SaveTranscriptionWithAudio(ctx, "audio-linked transcription", "en", "test-provider", "test-model", 500, 20, audioInput); err != nil {
		t.Fatalf("SaveTranscriptionWithAudio: %v", err)
	}

	// Discover the written audio path via ExportScope.
	export, err := s.ExportScope(t.Context(), scope)
	if err != nil {
		t.Fatalf("ExportScope after seed: %v", err)
	}
	if len(export.AudioAssetPaths) == 0 {
		t.Fatalf("expected at least 1 audio asset path after seeding, got 0")
	}
	audioPath := export.AudioAssetPaths[0]

	// Verify the file actually exists on disk before the test exercises delete.
	if _, statErr := os.Stat(audioPath); statErr != nil {
		t.Fatalf("seeded audio file does not exist on disk: %v", statErr)
	}
	return s, audioPath
}

// TestPrivacyDeleteUnlinksAudioFiles verifies that the handler removes audio
// files from disk after deleting DB rows, and that the audit event records
// audio_files_removed and audio_files_attempted.
func TestPrivacyDeleteUnlinksAudioFiles(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(auditlogtest.Reset)
	auditlog.Configure(true, dir, 90, false)

	userSID := "unlink-test-user"
	s, audioPath := newPrivacyTestStoreWithAudio(t, userSID)

	handler := handlePrivacyDelete(s)
	rec := postPrivacy(t, handler, "/api/v1/privacy/delete", map[string]any{
		"scope":    "user",
		"user_sid": userSID,
		"confirm":  true,
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status: want 200, got %d (body: %s)", rec.Code, rec.Body.String())
	}

	// Audio file must be unlinked from disk.
	if _, statErr := os.Stat(audioPath); !os.IsNotExist(statErr) {
		t.Errorf("audio file should be unlinked after delete, but it still exists (stat err: %v)", statErr)
	}

	// Response must include audio_files_removed and audio_files_attempted.
	var resp struct {
		AudioFilesAttempted int `json:"audio_files_attempted"`
		AudioFilesRemoved   int `json:"audio_files_removed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp.AudioFilesAttempted != 1 {
		t.Errorf("audio_files_attempted: want 1, got %d", resp.AudioFilesAttempted)
	}
	if resp.AudioFilesRemoved != 1 {
		t.Errorf("audio_files_removed: want 1, got %d", resp.AudioFilesRemoved)
	}

	// Audit log must contain audio_files_removed:1 and audio_files_attempted:1.
	dateKey := time.Now().UTC().Format("2006-01-02")
	logPath := filepath.Join(dir, "audit-"+dateKey+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	logStr := string(data)
	if !strings.Contains(logStr, `"audio_files_removed":1`) {
		t.Errorf("want audio_files_removed:1 in audit event, got:\n%s", logStr)
	}
	if !strings.Contains(logStr, `"audio_files_attempted":1`) {
		t.Errorf("want audio_files_attempted:1 in audit event, got:\n%s", logStr)
	}
}
