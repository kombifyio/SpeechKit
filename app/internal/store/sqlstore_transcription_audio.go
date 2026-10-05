package store

import (
	"context"
	"fmt"
)

// UpdateTranscriptionAudio completes an existing live entry using the same
// scoped asset persistence and retention policy as batch dictation.
func (s *sqlStore) UpdateTranscriptionAudio(ctx context.Context, id, durationMs int64, audioData []byte) error {
	scopeID, err := s.scopeID(ctx)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is harmless
	result, err := tx.ExecContext(ctx, s.dialect.rebind(`UPDATE transcriptions SET duration_ms = ? WHERE id = ? AND scope_id = ?`), durationMs, id, scopeID)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return fmt.Errorf("transcription %d not found", id)
	}
	var existing int
	if err := tx.QueryRowContext(ctx, s.dialect.rebind(`SELECT COUNT(*) FROM transcription_audio_assets WHERE transcription_id = ? AND role = 'source'`), id).Scan(&existing); err != nil {
		return err
	}
	var audioPath string
	if existing == 0 {
		audioPath, err = s.persistAudio(audioAssetInputFromBytes(audioData), "")
		if err != nil {
			return err
		}
	}
	committed := false
	defer func() { s.discardUncommittedAudio(audioPath, committed) }()
	if audioPath != "" {
		if err := recordScopedAudioAsset(ctx, tx, s.dialect.name, scopeID, "transcription", id, audioPath, durationMs); err != nil {
			return err
		}
	}
	if err := refreshStoreStats(ctx, tx, s.dialect, scopeID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	s.scheduleMaintenance() //nolint:contextcheck // maintenance is independent of this request
	return nil
}
