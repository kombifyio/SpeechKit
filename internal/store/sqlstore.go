package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	speechstorage "github.com/kombifyio/SpeechKit/pkg/speechkit/storage"
)

// sqlStore holds the database-agnostic Store implementation shared by the
// SQLite and PostgreSQL backends. The two backends differ only in connection
// setup and a small set of SQL-syntax details captured by sqlDialect; all query
// logic lives here exactly once. SQLiteStore and PostgresStore embed *sqlStore,
// so their backend-specific helpers in other files keep accessing these fields
// via Go's field promotion.
type sqlStore struct {
	db                      *sql.DB
	dialect                 sqlDialect
	audioDir                string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	transcriptionModelHints map[string]string
	defaultScope            speechstorage.Scope
	scopePolicy             speechstorage.ScopePolicy
}

// DB exposes the underlying *sql.DB so adjacent packages can build their own
// table-scoped persisters without the base Store interface enumerating every
// optional capability. Callers must treat the handle as read-mostly: it is
// owned by the Store and must not be closed.
func (s *sqlStore) DB() *sql.DB { return s.db }

func (s *sqlStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *sqlStore) SemanticCapabilities(context.Context) SemanticCapabilities {
	return SemanticCapabilities{
		Provider:     SemanticProviderNone,
		FullText:     false,
		Embeddings:   false,
		VectorSearch: false,
	}
}

func (s *sqlStore) scopeID(ctx context.Context) (int64, error) {
	scope, err := effectiveStoreScope(ctx, s.defaultScope, s.scopePolicy)
	if err != nil {
		return 0, err
	}
	return s.scopeIDForScope(ctx, scope)
}

func (s *sqlStore) scopeIDForScope(ctx context.Context, scope speechstorage.Scope) (int64, error) {
	if s.dialect.isPostgres() {
		return ensurePostgresScopeID(ctx, s.db, scope)
	}
	return ensureSQLiteScopeID(ctx, s.db, scope)
}

func (s *sqlStore) transcriptionModelHint(provider string) string {
	if len(s.transcriptionModelHints) == 0 {
		return ""
	}
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" {
		return ""
	}
	if model := s.transcriptionModelHints[provider]; model != "" {
		return model
	}
	switch provider {
	case "hf":
		return s.transcriptionModelHints["huggingface"]
	case "huggingface":
		return s.transcriptionModelHints["hf"]
	default:
		return ""
	}
}

// persistAudio writes raw audio bytes to the backend's audio directory and
// returns the file path, or "" when audio saving is disabled or there is no
// audio. The filename is "<prefix><unix-nano><ext>".
func (s *sqlStore) persistAudio(audio AudioAssetInput, prefix string) (string, error) {
	audio = normalizeAudioAssetInput(audio)
	if !s.saveAudio || len(audio.Data) == 0 {
		return "", nil
	}
	if err := os.MkdirAll(s.audioDir, 0o700); err != nil {
		return "", fmt.Errorf("create audio dir: %w", err)
	}
	filename := fmt.Sprintf("%s%d%s", prefix, time.Now().UnixNano(), audio.Extension)
	audioPath := filepath.Join(s.audioDir, filename)
	if err := os.WriteFile(audioPath, audio.Data, 0o600); err != nil {
		return "", fmt.Errorf("save audio: %w", err)
	}
	return audioPath, nil
}

func (s *sqlStore) scheduleMaintenance() {
	if s.saveAudio && s.maxStorageMB > 0 {
		go s.enforceStorageLimit()
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention()
	}
}

// ── Transcriptions ───────────────────────────────────────────────────────────

func (s *sqlStore) SaveTranscription(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audioData []byte) error {
	return s.SaveTranscriptionWithAudio(ctx, text, language, provider, model, durationMs, latencyMs, audioAssetInputFromBytes(audioData))
}

func (s *sqlStore) SaveTranscriptionWithAudio(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput) error {
	return s.SaveTranscriptionWithAudioAndSpeakers(ctx, text, language, provider, model, durationMs, latencyMs, audio, nil)
}

func (s *sqlStore) SaveTranscriptionWithAudioAndSpeakers(ctx context.Context, text, language, provider, model string, durationMs, latencyMs int64, audio AudioAssetInput, speakers *speaker.DiarizationResult) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	audio = normalizeAudioAssetInput(audio)
	if audio.DurationMs > 0 {
		durationMs = audio.DurationMs
	}
	audioPath, err := s.persistAudio(audio, "")
	if err != nil {
		return fmt.Errorf("%s: persist transcription audio: %w", s.dialect.name, err)
	}
	if strings.TrimSpace(model) == "" {
		model = s.transcriptionModelHint(provider)
	}
	speakerJSON, err := marshalSpeakerJSON(speakers)
	if err != nil {
		return fmt.Errorf("marshal speaker metadata: %w", err)
	}
	owner, _ := RecordOwnerFromContext(ctx)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	id, err := s.dialect.insertReturningID(ctx, tx,
		`INSERT INTO transcriptions (scope_id, text, language, language_base, provider, model, duration_ms, latency_ms, word_count, speaker_json, owner_user_id, owner_org_id, owner_source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, model, durationMs, latencyMs,
		countWords(text), speakerJSON, owner.UserID, owner.OrgID, owner.Source,
	)
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "transcription", id, audioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transcription: %w", err)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return nil
}

// transcriptionSelectSQL is the shared projection + audio-asset join used by
// GetTranscription and ListTranscriptions.
const transcriptionSelectSQL = `SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), COALESCE(t.latency_ms, 0),
		COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
		t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, ''), COALESCE(t.speaker_json, '')
	 FROM transcriptions t
	 LEFT JOIN audio_assets a ON a.id = (
		SELECT link.audio_asset_id
		FROM transcription_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.transcription_id = t.id AND link.role = 'source'
		ORDER BY link.created_at DESC, link.audio_asset_id DESC
		LIMIT 1
	 )`

func (s *sqlStore) scanTranscription(sc interface{ Scan(...any) error }) (Transcription, error) {
	var t Transcription
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	var speakerJSON string
	if err := sc.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
		&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
		&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource, &speakerJSON); err != nil {
		return Transcription{}, err
	}
	if strings.TrimSpace(t.Model) == "" {
		t.Model = s.transcriptionModelHint(t.Provider)
	}
	t.Audio = buildAudioAsset(audioStorageKind, t.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	t.Speakers = unmarshalSpeakerJSON(speakerJSON)
	return t, nil
}

func marshalSpeakerJSON(result *speaker.DiarizationResult) (string, error) {
	if result == nil || (len(result.Segments) == 0 && len(result.Words) == 0 && len(result.Speakers) == 0) {
		return "", nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalSpeakerJSON(raw string) *speaker.DiarizationResult {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var result speaker.DiarizationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		slog.Warn("store: failed to parse transcription speaker metadata", "err", err)
		return nil
	}
	return &result
}

func (s *sqlStore) GetTranscription(ctx context.Context, id int64) (*Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(transcriptionSelectSQL+` WHERE t.id = ? AND t.scope_id = ?`), id, scopeID)
	t, err := s.scanTranscription(row)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *sqlStore) ListTranscriptions(ctx context.Context, opts ListOpts) ([]Transcription, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := transcriptionSelectSQL
	args := []any{scopeID}
	clauses := []string{"t.scope_id = ?"}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, opts.Language)
	clauses, args = appendOwnerFilter(clauses, args, opts)
	if !opts.After.IsZero() {
		clauses = append(clauses, "t.created_at > ?")
		args = append(args, s.dialect.timeArg(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY t.created_at DESC, t.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	results := make([]Transcription, 0)
	for rows.Next() {
		t, err := s.scanTranscription(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

func (s *sqlStore) TranscriptionCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT COUNT(*) FROM transcriptions WHERE scope_id = ?`), scopeID).Scan(&count)
	return count, err
}

// ── User dictionary ──────────────────────────────────────────────────────────

func (s *sqlStore) ReplaceUserDictionaryEntries(ctx context.Context, language string, entries []UserDictionaryEntry) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	language = normalizeDictionaryLanguage(language)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx,
		s.dialect.rebind(`DELETE FROM user_dictionary_entries WHERE scope_id = ? AND language = ? AND source = ?`),
		scopeID, language, userDictionarySettingsSource,
	); err != nil {
		return fmt.Errorf("clear user dictionary entries: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`INSERT INTO user_dictionary_entries (scope_id, spoken, canonical, language, source, enabled)
		 VALUES (?, ?, ?, ?, ?, %s)
		 ON CONFLICT(scope_id, spoken, canonical, language, source)
		 DO UPDATE SET enabled = %s, updated_at = %s`,
		s.dialect.boolLit(true), s.dialect.boolLit(true), s.dialect.now())),
	)
	if err != nil {
		return fmt.Errorf("prepare user dictionary insert: %w", err)
	}
	defer stmt.Close() //nolint:errcheck // statement close during transaction cleanup

	for _, entry := range entries {
		entry, ok := normalizeUserDictionaryEntry(entry, language)
		if !ok {
			continue
		}
		if _, err = stmt.ExecContext(ctx, scopeID, entry.Spoken, entry.Canonical, entry.Language, entry.Source); err != nil {
			return fmt.Errorf("insert user dictionary entry: %w", err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit user dictionary entries: %w", err)
	}
	if err := s.replaceDictionaryCustomizationProjection(ctx, language, entries); err != nil {
		return fmt.Errorf("sync customization projection: %w", err)
	}
	return nil
}

func (s *sqlStore) ListUserDictionaryEntries(ctx context.Context, language string) ([]UserDictionaryEntry, error) {
	projected, projectionErr := s.listProjectedUserDictionaryEntries(ctx, language)
	if projectionErr == nil && len(projected) > 0 {
		return projected, nil
	}
	if projectionErr != nil && !isMissingCustomizationTable(projectionErr) {
		return nil, projectionErr
	}

	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	language = normalizeDictionaryLanguage(language)
	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`SELECT id, spoken, canonical, language, source, enabled, usage_count, created_at, updated_at
		 FROM user_dictionary_entries
		 WHERE scope_id = ? AND enabled = %s AND (? = '' OR language = ? OR language = '')
		 ORDER BY id ASC`, s.dialect.boolLit(true))),
		scopeID, language, language,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	entries := make([]UserDictionaryEntry, 0)
	for rows.Next() {
		var entry UserDictionaryEntry
		var enabled boolValue
		if err := rows.Scan(
			&entry.ID, &entry.Spoken, &entry.Canonical, &entry.Language, &entry.Source,
			&enabled, &entry.UsageCount, &entry.CreatedAt, &entry.UpdatedAt,
		); err != nil {
			return nil, err
		}
		entry.Enabled = bool(enabled)
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *sqlStore) RecordUserDictionaryUsage(ctx context.Context, canonical, language string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	canonical = strings.TrimSpace(canonical)
	language = normalizeDictionaryLanguage(language)
	if canonical == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE user_dictionary_entries
		 SET usage_count = usage_count + 1, updated_at = %s
		 WHERE scope_id = ? AND enabled = %s AND lower(canonical) = lower(?) AND (? = '' OR language = ? OR language = '')`,
		s.dialect.now(), s.dialect.boolLit(true))),
		scopeID, canonical, language, language,
	)
	if err != nil {
		return fmt.Errorf("record user dictionary usage: %w", err)
	}
	if err := s.RecordWordUsage(ctx, canonical, language); err != nil && !isMissingCustomizationTable(err) {
		return fmt.Errorf("record customization word usage: %w", err)
	}
	_, err = s.db.ExecContext(ctx, s.dialect.rebind(fmt.Sprintf(
		`UPDATE customization_replacements
		 SET usage_count = usage_count + 1, updated_at = %s
		 WHERE scope_id = ? AND enabled = %s AND lower(output_text) = lower(?) AND (? = '' OR language = ? OR language = '') AND stage = 'post_stt'`,
		s.dialect.now(), s.dialect.boolLit(true))),
		scopeID, canonical, language, language,
	)
	if err != nil && !isMissingCustomizationTable(err) {
		return fmt.Errorf("record customization replacement usage: %w", err)
	}
	return nil
}

// ── Quick notes ──────────────────────────────────────────────────────────────

const quickNoteSelectSQL = `SELECT q.id, q.text, q.language, q.provider, COALESCE(q.duration_ms, 0), COALESCE(q.latency_ms, 0),
		COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
		COALESCE(q.pinned, %s), q.created_at, q.updated_at
	 FROM quick_notes q
	 LEFT JOIN audio_assets a ON a.id = (
		SELECT link.audio_asset_id
		FROM quick_note_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.quick_note_id = q.id AND link.role = 'source'
		ORDER BY link.created_at DESC, link.audio_asset_id DESC
		LIMIT 1
	 )`

func (s *sqlStore) scanQuickNote(sc interface{ Scan(...any) error }) (QuickNote, error) {
	var n QuickNote
	var pinned boolValue
	var audioStorageKind, audioMimeType string
	var audioSizeBytes, audioDurationMs int64
	if err := sc.Scan(&n.ID, &n.Text, &n.Language, &n.Provider, &n.DurationMs, &n.LatencyMs,
		&n.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &pinned, &n.CreatedAt, &n.UpdatedAt); err != nil {
		return QuickNote{}, err
	}
	n.Pinned = bool(pinned)
	n.Audio = buildAudioAsset(audioStorageKind, n.AudioPath, audioMimeType, audioSizeBytes, audioDurationMs)
	return n, nil
}

func (s *sqlStore) SaveQuickNote(ctx context.Context, text, language, provider string, durationMs, latencyMs int64, audioData []byte) (int64, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	audioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return 0, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	id, err := s.dialect.insertReturningID(ctx, tx,
		`INSERT INTO quick_notes (scope_id, text, language, language_base, provider, duration_ms, latency_ms, word_count)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		scopeID, text, language, normalizeDictionaryLanguage(language), provider, durationMs, latencyMs, countWords(text),
	)
	if err != nil {
		return 0, fmt.Errorf("insert quick note: %w", err)
	}
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "quick_note", id, audioPath, durationMs); err != nil {
			return 0, fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return 0, fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit quick note: %w", err)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return id, nil
}

func (s *sqlStore) GetQuickNote(ctx context.Context, id int64) (*QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	query := fmt.Sprintf(quickNoteSelectSQL, s.dialect.boolLit(false)) + ` WHERE q.id = ? AND q.scope_id = ?`
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(query), id, scopeID)
	n, err := s.scanQuickNote(row)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func (s *sqlStore) ListQuickNotes(ctx context.Context, opts ListOpts) ([]QuickNote, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return nil, err
	}
	limit, offset := normalizedListPagination(opts)

	query := fmt.Sprintf(quickNoteSelectSQL, s.dialect.boolLit(false))
	args := []any{scopeID}
	clauses := []string{"q.scope_id = ?"}
	clauses, args = appendNormalizedLanguageFilter(clauses, args, opts.Language)
	if !opts.After.IsZero() {
		clauses = append(clauses, "q.created_at > ?")
		args = append(args, s.dialect.timeArg(opts.After))
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ") // #nosec G202 -- clauses are fixed internal snippets; values are parameterized.
	}
	query += " ORDER BY q.pinned DESC, q.created_at DESC, q.id DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, s.dialect.rebind(query), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	results := make([]QuickNote, 0)
	for rows.Next() {
		n, err := s.scanQuickNote(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, n)
	}
	return results, rows.Err()
}

func (s *sqlStore) UpdateQuickNote(ctx context.Context, id int64, text string) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless

	result, err := tx.ExecContext(ctx,
		s.dialect.rebind(fmt.Sprintf(`UPDATE quick_notes SET text = ?, word_count = ?, updated_at = %s WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		text, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note update: %w", err)
	}
	return nil
}

// quickNoteAudioPathSQL fetches the current source-audio path for a note.
const quickNoteAudioPathSQL = `SELECT COALESCE(a.path, '')
	FROM quick_notes q
	LEFT JOIN audio_assets a ON a.id = (
		SELECT link.audio_asset_id
		FROM quick_note_audio_assets link
		JOIN audio_assets asset ON asset.id = link.audio_asset_id
		WHERE link.quick_note_id = q.id AND link.role = 'source'
		ORDER BY link.created_at DESC, link.audio_asset_id DESC
		LIMIT 1
	)
	WHERE q.id = ? AND q.scope_id = ?
	LIMIT 1`

func (s *sqlStore) UpdateQuickNoteCapture(ctx context.Context, id int64, text, provider string, durationMs, latencyMs int64, audioData []byte) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var currentAudioPath string
	if err := tx.QueryRowContext(ctx, s.dialect.rebind(quickNoteAudioPathSQL), id, scopeID).Scan(&currentAudioPath); err != nil {
		return fmt.Errorf("lookup quick note %d: %w", id, err)
	}

	nextAudioPath, err := s.persistAudio(audioAssetInputFromBytes(audioData), "qn_")
	if err != nil {
		return fmt.Errorf("%s: persist quicknote audio: %w", s.dialect.name, err)
	}
	if nextAudioPath == "" {
		nextAudioPath = currentAudioPath
	}
	createdNewAudio := nextAudioPath != "" && currentAudioPath != nextAudioPath
	defer func() {
		if !committed && createdNewAudio {
			_ = os.Remove(nextAudioPath)
		}
	}()

	result, err := tx.ExecContext(ctx,
		s.dialect.rebind(fmt.Sprintf(`UPDATE quick_notes
		 SET text = ?, provider = ?, duration_ms = ?, latency_ms = ?, word_count = ?, updated_at = %s
		 WHERE id = ? AND scope_id = ?`, s.dialect.now())),
		text, provider, durationMs, latencyMs, countWords(text), id, scopeID,
	)
	if err != nil {
		return fmt.Errorf("update quick note capture: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := deleteAudioAsset(ctx, tx, s.dialect.name, "quick_note", id, currentAudioPath); err != nil {
			return fmt.Errorf("delete previous audio asset: %w", err)
		}
	}
	if nextAudioPath != "" && currentAudioPath != nextAudioPath {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "quick_note", id, nextAudioPath, durationMs); err != nil {
			return fmt.Errorf("record audio asset: %w", err)
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note capture: %w", err)
	}
	committed = true
	if currentAudioPath != "" && currentAudioPath != nextAudioPath {
		_ = os.Remove(currentAudioPath)
	}

	s.scheduleMaintenance() //nolint:contextcheck // maintenance goroutines must not be bound to request context
	return nil
}

func (s *sqlStore) PinQuickNote(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(`UPDATE quick_notes SET pinned = ? WHERE id = ? AND scope_id = ?`), pinned, id, scopeID)
	if err != nil {
		return fmt.Errorf("pin quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	return nil
}

func (s *sqlStore) DeleteQuickNote(ctx context.Context, id int64) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%s: begin tx: %w", s.dialect.name, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var audioPath string
	_ = tx.QueryRowContext(ctx, s.dialect.rebind(quickNoteAudioPathSQL), id, scopeID).Scan(&audioPath)

	if audioPath != "" {
		if err := deleteAudioAssetsForOwner(ctx, tx, s.dialect.name, "quick_note", id); err != nil {
			return fmt.Errorf("delete quick note audio assets: %w", err)
		}
	}

	result, err := tx.ExecContext(ctx, s.dialect.rebind(`DELETE FROM quick_notes WHERE id = ? AND scope_id = ?`), id, scopeID)
	if err != nil {
		return fmt.Errorf("delete quick note: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("quick note %d not found", id)
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return fmt.Errorf("refresh stats: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit quick note delete: %w", err)
	}
	committed = true
	if audioPath != "" {
		_ = os.Remove(audioPath)
	}
	return nil
}

func (s *sqlStore) QuickNoteCount(ctx context.Context) (int, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT COUNT(*) FROM quick_notes WHERE scope_id = ?`), scopeID).Scan(&count)
	return count, err
}

// ── Stats ────────────────────────────────────────────────────────────────────

func (s *sqlStore) Stats(ctx context.Context) (Stats, error) {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return Stats{}, err
	}
	var stats Stats
	var totalLatency, latencyCount int64
	err = s.db.QueryRowContext(ctx, s.dialect.rebind(`SELECT transcriptions_count, quick_notes_count, total_words,
		total_audio_duration_ms, total_latency_ms, latency_count
		FROM store_stats WHERE scope_id = ?`), scopeID).
		Scan(&stats.Transcriptions, &stats.QuickNotes, &stats.TotalWords, &stats.TotalAudioDurationMs, &totalLatency, &latencyCount)
	if err != nil {
		return Stats{}, err
	}
	if stats.TotalAudioDurationMs > 0 {
		stats.AverageWordsPerMinute = float64(stats.TotalWords) / (float64(stats.TotalAudioDurationMs) / float64(time.Minute/time.Millisecond))
	}
	if latencyCount > 0 {
		stats.AverageLatencyMs = totalLatency / latencyCount
	}
	return stats, nil
}

// ── Maintenance ──────────────────────────────────────────────────────────────

func (s *sqlStore) enforceStorageLimit() {
	s.enforceAudioRetention()

	entries, err := os.ReadDir(s.audioDir)
	if err != nil {
		return
	}
	var totalSize int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		totalSize += info.Size()
	}
	limitBytes := int64(s.maxStorageMB) * 1024 * 1024
	if totalSize <= limitBytes {
		return
	}

	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin cleanup tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> ''
		 ORDER BY created_at ASC, id ASC`,
	)
	if err != nil {
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() && totalSize > limitBytes {
		var kind, path string
		var id int64
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan cleanup row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if err := os.Remove(path); err != nil {
			slog.Warn("store: remove audio", "path", path, "err", err)
			continue
		}
		totalSize -= info.Size()
		if err := deleteAudioAsset(context.Background(), tx, s.dialect.name, kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: cleanup rows error", "err", err)
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit cleanup tx", "err", err)
	}
}

func (s *sqlStore) enforceAudioRetention() {
	if s.audioRetentionDays <= 0 {
		return
	}
	cutoff := s.dialect.timeArg(time.Now().Add(-time.Duration(s.audioRetentionDays) * 24 * time.Hour))

	tx, err := s.db.BeginTx(context.Background(), nil) //nolint:contextcheck // background goroutine should not be bound to request context
	if err != nil {
		slog.Warn("store: begin retention tx", "err", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // deferred rollback, error not actionable

	rows, err := tx.QueryContext(context.Background(), //nolint:contextcheck // background goroutine should not be bound to request context
		s.dialect.rebind(`SELECT owner_kind AS kind, owner_id AS id, path
		 FROM audio_assets
		 WHERE path <> '' AND created_at < ?
		 ORDER BY created_at ASC, id ASC`),
		cutoff,
	)
	if err != nil {
		slog.Warn("store: query retention rows", "err", err)
		return
	}
	defer rows.Close() //nolint:errcheck // deferred rows close, error not actionable

	for rows.Next() {
		var kind, path string
		var id int64
		if err := rows.Scan(&kind, &id, &path); err != nil {
			slog.Warn("store: scan retention row", "err", err)
			continue
		}
		if path == "" {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			slog.Warn("store: remove retained audio", "path", path, "err", err)
			continue
		}
		if err := deleteAudioAsset(context.Background(), tx, s.dialect.name, kind, id, path); err != nil { //nolint:contextcheck // background goroutine should not be bound to request context
			slog.Warn("store: delete retained audio asset", "kind", kind, "id", id, "err", err)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Warn("store: retention rows error", "err", err)
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("store: commit retention tx", "err", err)
	}
}
