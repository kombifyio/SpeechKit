package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/kombifyio/SpeechKit/app/internal/safepath"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// SubjectRightsStore is the optional extension a hosted deployment uses to
// export and erase everything one account (identified by the subject string
// the server stored as owner_user_id and scope user_id) holds. It complements
// ScopePrivacyStore, which addresses one storage scope, not one account.
type SubjectRightsStore interface {
	// SubjectIDs lists every non-empty account subject the store holds data for.
	SubjectIDs(ctx context.Context) ([]string, error)
	// CountSubject reports the rows held per table for the subject.
	CountSubject(ctx context.Context, subject string) (map[string]int64, error)
	// DeleteSubject removes every row of the subject and returns the file paths
	// the rows referenced. Managed audio and snapshot files are removed here;
	// WakewordAudioPaths are relative to the wake-word audio directory, which
	// only the caller knows.
	DeleteSubject(ctx context.Context, subject string) (SubjectDeleteResult, error)
}

// SubjectDeleteResult reports an account erasure.
type SubjectDeleteResult struct {
	Rows               map[string]int64
	WakewordAudioPaths []string
}

var _ SubjectRightsStore = (*SQLiteStore)(nil)
var _ SubjectRightsStore = (*PostgresStore)(nil)

// subjectTable is one owner-attributed table. Where is evaluated with the
// subject bound once per `?`.
type subjectTable struct {
	name  string
	where string
}

const (
	ownedTranscriptions = `(SELECT id FROM transcriptions WHERE owner_user_id = ?)`
	ownedVoiceSessions  = `(SELECT id FROM voice_agent_sessions WHERE owner_user_id = ?)`
	ownedRecordings     = `(SELECT id FROM recording_sessions WHERE owner_user_id = ?)`
)

// subjectTables lists the rows attributable to an account through the owner
// columns, children first so every delete respects foreign keys. Rows kept in
// a per-user storage scope are handled through DeleteScope.
var subjectTables = []subjectTable{
	{"transcription_audio_assets", `transcription_id IN ` + ownedTranscriptions},
	{"audio_assets", `owner_kind = 'transcription' AND owner_id IN ` + ownedTranscriptions},
	{"voice_agent_session_turns", `session_id IN ` + ownedVoiceSessions},
	{"voice_agent_session_summary_items", `session_id IN ` + ownedVoiceSessions},
	{"recording_session_segments", `session_id IN ` + ownedRecordings},
	{"recording_session_notes", `session_id IN ` + ownedRecordings},
	{"recording_session_snapshots", `session_id IN ` + ownedRecordings},
	{"recording_session_enhancements", `session_id IN ` + ownedRecordings},
	{"meeting_summary_batches", `session_id IN ` + ownedRecordings},
	{"recording_session_imports", `session_id IN ` + ownedRecordings},
	{"recording_session_exports", `session_id IN ` + ownedRecordings},
	{"recording_sessions", `owner_user_id = ?`},
	{"voice_agent_sessions", `owner_user_id = ?`},
	{"transcriptions", `owner_user_id = ?`},
	{"wakeword_activations", `owner_user_id = ?`},
}

func countPlaceholders(where string) int { return strings.Count(where, "?") }

func repeatArg(subject string, n int) []any {
	args := make([]any, n)
	for i := range args {
		args[i] = subject
	}
	return args
}

func (s *sqlStore) SubjectIDs(ctx context.Context) ([]string, error) {
	seen := map[string]struct{}{}
	for _, q := range []string{
		`SELECT DISTINCT owner_user_id FROM transcriptions WHERE owner_user_id <> ''`,
		`SELECT DISTINCT owner_user_id FROM voice_agent_sessions WHERE owner_user_id <> ''`,
		`SELECT DISTINCT owner_user_id FROM recording_sessions WHERE owner_user_id <> ''`,
		`SELECT DISTINCT owner_user_id FROM wakeword_activations WHERE owner_user_id <> ''`,
		`SELECT DISTINCT user_id FROM storage_scopes WHERE user_id <> ''`,
	} {
		rows, err := s.db.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			seen[id] = struct{}{}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out, nil
}

// subjectScopes returns every storage scope keyed to the subject's user id.
func (s *sqlStore) subjectScopes(ctx context.Context, subject string) ([]speechstorage.Scope, error) {
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(
		`SELECT install_id, device_id, user_id, tenant_id, labels_json FROM storage_scopes WHERE user_id = ?`), subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // read-only cursor
	var scopes []speechstorage.Scope
	for rows.Next() {
		var sc speechstorage.Scope
		var labels string
		if err := rows.Scan(&sc.InstallID, &sc.DeviceID, &sc.UserID, &sc.TenantID, &labels); err != nil {
			return nil, err
		}
		if strings.TrimSpace(labels) != "" && labels != "{}" {
			if err := json.Unmarshal([]byte(labels), &sc.Labels); err != nil {
				return nil, fmt.Errorf("scope labels: %w", err)
			}
		}
		scopes = append(scopes, sc)
	}
	return scopes, rows.Err()
}

func (s *sqlStore) CountSubject(ctx context.Context, subject string) (map[string]int64, error) {
	counts := map[string]int64{}
	for _, t := range subjectTables {
		var n int64
		q := `SELECT COUNT(*) FROM ` + t.name + ` WHERE ` + t.where
		if err := s.db.QueryRowContext(ctx, s.dialect.rebind(q), repeatArg(subject, countPlaceholders(t.where))...).Scan(&n); err != nil {
			return nil, err
		}
		counts[t.name] += n
	}
	scopes, err := s.subjectScopes(ctx, subject)
	if err != nil {
		return nil, err
	}
	for _, sc := range scopes {
		export, err := s.ExportScope(ctx, sc)
		if err != nil {
			return nil, err
		}
		counts["scoped_transcriptions"] += int64(len(export.Transcriptions))
		counts["scoped_quick_notes"] += int64(len(export.QuickNotes))
		counts["scoped_voice_agent_sessions"] += int64(len(export.VoiceAgentSessions))
		counts["scoped_recording_sessions"] += int64(len(export.RecordingSessions))
		counts["scoped_notebooks"] += int64(len(export.Notebooks))
		counts["scoped_dictionary_entries"] += int64(len(export.DictionaryEntries))
	}
	return counts, nil
}

func (s *sqlStore) DeleteSubject(ctx context.Context, subject string) (SubjectDeleteResult, error) {
	result := SubjectDeleteResult{Rows: map[string]int64{}}
	var filePaths []string

	// Owner-attributed rows (shared scopes) in one transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	collect := func(q string, args ...any) ([]string, error) {
		rows, err := tx.QueryContext(ctx, s.dialect.rebind(q), args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close() //nolint:errcheck // read-only cursor
		var out []string
		for rows.Next() {
			var p string
			if err := rows.Scan(&p); err != nil {
				return nil, err
			}
			if strings.TrimSpace(p) != "" {
				out = append(out, p)
			}
		}
		return out, rows.Err()
	}
	audio, err := collect(`SELECT path FROM audio_assets WHERE owner_kind = 'transcription' AND owner_id IN `+ownedTranscriptions, subject)
	if err != nil {
		return result, fmt.Errorf("collect audio paths: %w", err)
	}
	snaps, err := collect(`SELECT path FROM recording_session_snapshots WHERE session_id IN `+ownedRecordings, subject)
	if err != nil {
		return result, fmt.Errorf("collect snapshot paths: %w", err)
	}
	result.WakewordAudioPaths, err = collect(`SELECT audio_path FROM wakeword_activations WHERE owner_user_id = ?`, subject)
	if err != nil {
		return result, fmt.Errorf("collect wake-word paths: %w", err)
	}
	filePaths = append(append(filePaths, audio...), snaps...)

	for _, t := range subjectTables {
		res, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM `+t.name+` WHERE `+t.where), repeatArg(subject, countPlaceholders(t.where))...)
		if err != nil {
			return result, fmt.Errorf("delete %s: %w", t.name, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return result, err
		}
		result.Rows[t.name] += n
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}

	// Rows kept in a per-user storage scope.
	scopes, err := s.subjectScopes(ctx, subject)
	if err != nil {
		return result, err
	}
	for _, sc := range scopes {
		res, err := s.DeleteScope(ctx, sc)
		if err != nil {
			return result, err
		}
		result.Rows["scoped_rows"] += int64(res.RowsDeleted)
		filePaths = append(filePaths, res.AudioFilePaths...)
		filePaths = append(filePaths, res.SnapshotFilePaths...)
	}
	// The scope rows carry the account id itself; drop them with their data.
	if len(scopes) > 0 {
		if _, err := s.db.ExecContext(ctx, s.dialect.rebind(`DELETE FROM storage_scopes WHERE user_id = ?`), subject); err != nil {
			return result, fmt.Errorf("delete storage scopes: %w", err)
		}
	}

	// Managed files go after the rows; a file that cannot be removed fails the
	// erasure so the caller reports it instead of claiming completion.
	var failed []string
	for _, p := range filePaths {
		resolved, err := safepath.Contained(p, s.ManagedFileRoots()...)
		if err != nil {
			// Gone already, or not one of this store's own files: a row must
			// not turn an erasure into deleting an arbitrary file.
			if !errors.Is(err, fs.ErrNotExist) {
				slog.Warn("subject erase: file outside the store directories not removed")
			}
			continue
		}
		if err := os.Remove(resolved); err != nil && !errors.Is(err, fs.ErrNotExist) {
			failed = append(failed, p)
		}
	}
	if len(failed) > 0 {
		return result, fmt.Errorf("store: %d managed files could not be removed", len(failed))
	}
	return result, nil
}
