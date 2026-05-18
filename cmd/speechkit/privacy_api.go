package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/store"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// privacyRequest is the JSON body accepted by both privacy endpoints.
type privacyRequest struct {
	// Scope selects the data boundary: "install" | "user" | "device".
	Scope string `json:"scope"`
	// UserSID is the Windows SID (or any stable user identifier) used when
	// Scope == "user". Required in that case; ignored otherwise.
	UserSID string `json:"user_sid,omitempty"`
	// Format selects the response encoding: "json" (default) | "zip".
	// "zip" bundles the JSON manifest plus any referenced audio files.
	Format string `json:"format,omitempty"`
	// Confirm must be true for the delete endpoint. Acts as a safety gate so
	// that an accidental POST cannot erase data.
	Confirm bool `json:"confirm,omitempty"`
}

// privacyExportResponse is the JSON envelope returned by the export endpoint
// when Format == "json" (the default).
type privacyExportResponse struct {
	Export *store.ScopeExport `json:"export"`
}

// privacyDeleteResponse is the JSON envelope returned by the delete endpoint.
type privacyDeleteResponse struct {
	RowsDeleted         int    `json:"rows_deleted"`
	AudioFilesAttempted int    `json:"audio_files_attempted"`
	AudioFilesRemoved   int    `json:"audio_files_removed"`
	ScopeKey            string `json:"scope_key"`
}

// scopeFromPrivacyRequest converts the request scope descriptor into a
// Storage-3.0 Scope value. Returns an HTTP-400-worthy error for unknown scopes.
func scopeFromPrivacyRequest(req privacyRequest) (speechstorage.Scope, error) {
	switch strings.TrimSpace(req.Scope) {
	case "install":
		return speechstorage.Scope{InstallID: speechstorage.LocalInstallID}, nil
	case "user":
		userSID := strings.TrimSpace(req.UserSID)
		if userSID == "" {
			return speechstorage.Scope{}, fmt.Errorf("user_sid is required when scope is \"user\"")
		}
		return speechstorage.Scope{InstallID: speechstorage.LocalInstallID, UserID: userSID}, nil
	case "device":
		return speechstorage.Scope{InstallID: speechstorage.LocalInstallID, DeviceID: "local-device"}, nil
	case "":
		return speechstorage.Scope{}, fmt.Errorf("scope field is required (install | user | device)")
	default:
		return speechstorage.Scope{}, fmt.Errorf("unknown scope %q (accepted: install | user | device)", req.Scope)
	}
}

// handlePrivacyExport implements POST /api/v1/privacy/export (GDPR Art. 15).
//
// Request body (JSON):
//
//	{"scope":"install"|"user"|"device", "user_sid":"...", "format":"json"|"zip"}
//
// Response (Format==json, default):
//
//	200 application/json — {"export": { ... ScopeExport ... }}
//
// Response (Format==zip):
//
//	200 application/zip — ZIP containing export.json + audio files
//
// Emits audit event: privacy.export (outcome=success|failure).
func handlePrivacyExport(feedbackStore store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1 MiB body cap
		var req privacyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}

		scope, err := scopeFromPrivacyRequest(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		privacyStore, ok := feedbackStore.(store.ScopePrivacyStore)
		if !ok {
			_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
				Event:    auditlog.EventPrivacyExport,
				Actor:    auditlog.Actor{UserSID: req.UserSID},
				Resource: map[string]any{"scope": scope.Key(), "error": "backend does not support privacy operations"},
				Outcome:  auditlog.OutcomeFailure,
			})
			http.Error(w, "storage backend does not support privacy operations", http.StatusNotImplemented)
			return
		}

		export, err := privacyStore.ExportScope(r.Context(), scope)
		if err != nil {
			slog.Error("privacy export failed", "scope", scope.Key(), "err", err)
			_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
				Event:    auditlog.EventPrivacyExport,
				Actor:    auditlog.Actor{UserSID: req.UserSID},
				Resource: map[string]any{"scope": scope.Key(), "error": err.Error()},
				Outcome:  auditlog.OutcomeFailure,
			})
			http.Error(w, "export failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		recordCount := len(export.Transcriptions) + len(export.QuickNotes) +
			len(export.VoiceAgentSessions) + len(export.DictionaryEntries)
		_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
			Event: auditlog.EventPrivacyExport,
			Actor: auditlog.Actor{UserSID: req.UserSID},
			Resource: map[string]any{
				"scope":        scope.Key(),
				"record_count": recordCount,
				"format":       normalizedPrivacyFormat(req.Format),
			},
			Outcome: auditlog.OutcomeSuccess,
		})

		if normalizedPrivacyFormat(req.Format) == "zip" {
			writeZipExport(w, export)
			return
		}

		writeJSON(w, privacyExportResponse{Export: export})
	}
}

// handlePrivacyDelete implements POST /api/v1/privacy/delete (GDPR Art. 17).
//
// Request body (JSON):
//
//	{"scope":"install"|"user"|"device", "user_sid":"...", "confirm":true}
//
// "confirm":true is mandatory — a missing or false value returns HTTP 400.
//
// Response:
//
//	200 application/json — {"rows_deleted": N, "audio_files_attempted": M, "audio_files_removed": K, "scope_key": "..."}
//
// Emits audit event: privacy.delete (outcome=success|failure).
//
// After DB rows are removed, audio files referenced by the deleted rows are
// unlinked from disk. Unlink failures are non-fatal (logged as warnings) —
// the DB rows are already gone so no PII remains in the database regardless.
func handlePrivacyDelete(feedbackStore store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		var req privacyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON body: "+err.Error(), http.StatusBadRequest)
			return
		}

		if !req.Confirm {
			http.Error(w,
				`"confirm":true is required to prevent accidental erasure`,
				http.StatusBadRequest)
			return
		}

		scope, err := scopeFromPrivacyRequest(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		privacyStore, ok := feedbackStore.(store.ScopePrivacyStore)
		if !ok {
			_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
				Event:    auditlog.EventPrivacyDelete,
				Actor:    auditlog.Actor{UserSID: req.UserSID},
				Resource: map[string]any{"scope": scope.Key(), "error": "backend does not support privacy operations"},
				Outcome:  auditlog.OutcomeFailure,
			})
			http.Error(w, "storage backend does not support privacy operations", http.StatusNotImplemented)
			return
		}

		result, err := privacyStore.DeleteScope(r.Context(), scope)
		if err != nil {
			slog.Error("privacy delete failed", "scope", scope.Key(), "err", err)
			_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
				Event:    auditlog.EventPrivacyDelete,
				Actor:    auditlog.Actor{UserSID: req.UserSID},
				Resource: map[string]any{"scope": scope.Key(), "error": err.Error()},
				Outcome:  auditlog.OutcomeFailure,
			})
			http.Error(w, "delete failed: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Unlink audio files on disk. Non-fatal — DB rows are already gone and
		// any filesystem error here doesn't change the data-controller posture
		// (no DB row = no PII remaining in the database).
		removed := 0
		for _, path := range result.AudioFilePaths {
			if unlinkErr := os.Remove(path); unlinkErr == nil {
				removed++
			} else if !errors.Is(unlinkErr, fs.ErrNotExist) {
				slog.Warn("privacy.delete: failed to unlink audio file", "path", path, "err", unlinkErr)
			}
		}

		_ = auditlog.AppendEvent(r.Context(), auditlog.Record{
			Event: auditlog.EventPrivacyDelete,
			Actor: auditlog.Actor{UserSID: req.UserSID},
			Resource: map[string]any{
				"scope":                 scope.Key(),
				"record_count":          result.RowsDeleted,
				"audio_files_attempted": len(result.AudioFilePaths),
				"audio_files_removed":   removed,
			},
			Outcome: auditlog.OutcomeSuccess,
		})

		writeJSON(w, privacyDeleteResponse{
			RowsDeleted:         result.RowsDeleted,
			AudioFilesAttempted: len(result.AudioFilePaths),
			AudioFilesRemoved:   removed,
			ScopeKey:            scope.Key(),
		})
	}
}

// normalizedPrivacyFormat returns "zip" when the request asks for zip, "json"
// for everything else (including the empty default).
func normalizedPrivacyFormat(format string) string {
	if strings.EqualFold(strings.TrimSpace(format), "zip") {
		return "zip"
	}
	return "json"
}

// writeZipExport builds a ZIP archive response containing:
//   - export.json  — the full ScopeExport as JSON
//   - audio/<filename> — each audio file referenced in AudioAssetPaths (best-effort; missing files are skipped)
func writeZipExport(w http.ResponseWriter, export *store.ScopeExport) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// Write the JSON manifest.
	manifestWriter, err := zw.Create("export.json")
	if err != nil {
		http.Error(w, "zip: create manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(manifestWriter).Encode(export); err != nil {
		http.Error(w, "zip: encode manifest: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Add audio files (best-effort — missing files are skipped with a warning).
	for _, path := range export.AudioAssetPaths {
		if err := addFileToZip(zw, path); err != nil {
			slog.Warn("privacy export: skip audio file", "path", path, "err", err)
		}
	}

	if err := zw.Close(); err != nil {
		http.Error(w, "zip: close: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="speechkit-export.zip"`)
	_, _ = io.Copy(w, &buf)
}

// addFileToZip copies one file from disk into the zip under audio/<basename>.
func addFileToZip(zw *zip.Writer, path string) error {
	f, err := os.Open(path) // #nosec G304 -- path is a store-managed audio asset path, not user-controlled input
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // closing a read-only file; error not actionable

	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("audio path is a directory: %s", path)
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	// Store under audio/ subdir so it doesn't collide with export.json.
	header.Name = "audio/" + info.Name()
	header.Method = zip.Deflate

	entryWriter, err := zw.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(entryWriter, f)
	return err
}
