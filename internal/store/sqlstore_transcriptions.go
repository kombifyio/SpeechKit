package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

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
// GetTranscription and ListTranscriptions. The one verb takes the dialect's
// false literal for the pinned fallback, so pass it through fmt.Sprintf.
const transcriptionSelectSQL = `SELECT t.id, t.text, t.language, t.provider, COALESCE(t.model, ''), COALESCE(t.duration_ms, 0), COALESCE(t.latency_ms, 0),
		COALESCE(a.path, ''), COALESCE(a.storage_kind, ''), COALESCE(a.mime_type, ''), COALESCE(a.size_bytes, 0), COALESCE(a.duration_ms, 0),
		t.created_at, COALESCE(t.owner_user_id, ''), COALESCE(t.owner_org_id, ''), COALESCE(t.owner_source, ''), COALESCE(t.speaker_json, ''),
		COALESCE(t.pinned, %s)
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
	var pinned boolValue
	if err := sc.Scan(&t.ID, &t.Text, &t.Language, &t.Provider, &t.Model, &t.DurationMs, &t.LatencyMs,
		&t.AudioPath, &audioStorageKind, &audioMimeType, &audioSizeBytes, &audioDurationMs, &t.CreatedAt,
		&t.OwnerUserID, &t.OwnerOrgID, &t.OwnerSource, &speakerJSON, &pinned); err != nil {
		return Transcription{}, err
	}
	t.Pinned = bool(pinned)
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
	query := fmt.Sprintf(transcriptionSelectSQL, s.dialect.boolLit(false)) + ` WHERE t.id = ? AND t.scope_id = ?`
	row := s.db.QueryRowContext(ctx, s.dialect.rebind(query), id, scopeID)
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

	query := fmt.Sprintf(transcriptionSelectSQL, s.dialect.boolLit(false))
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
	query += " ORDER BY t.pinned DESC, t.created_at DESC, t.id DESC LIMIT ? OFFSET ?"
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

// PinTranscription marks a dictation transcript as kept. Pinned transcripts
// sort ahead of the rolling history so the Library keeps them in reach no
// matter how far back the stream has moved.
func (s *sqlStore) PinTranscription(ctx context.Context, id int64, pinned bool) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return fmt.Errorf("%s: resolve scope: %w", s.dialect.name, err)
	}
	result, err := s.db.ExecContext(ctx, s.dialect.rebind(`UPDATE transcriptions SET pinned = ? WHERE id = ? AND scope_id = ?`), pinned, id, scopeID)
	if err != nil {
		return fmt.Errorf("pin transcription: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("transcription %d not found", id)
	}
	return nil
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
