package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
	snapshotDir             string
	maxStorageMB            int
	saveAudio               bool
	audioRetentionDays      int
	meetingRetentionDays    int
	transcriptionModelHints map[string]string
	defaultScope            speechstorage.Scope
	scopePolicy             speechstorage.ScopePolicy
	// maintenanceStop ends the periodic maintenance goroutine on Close;
	// maintenanceStopOnce guards against a double Close closing it twice.
	maintenanceStop     chan struct{}
	maintenanceStopOnce sync.Once
}

// DB exposes the underlying *sql.DB so adjacent packages can build their own
// table-scoped persisters without the base Store interface enumerating every
// optional capability. Callers must treat the handle as read-mostly: it is
// owned by the Store and must not be closed.
func (s *sqlStore) DB() *sql.DB { return s.db }

func (s *sqlStore) Close() error {
	if s.maintenanceStop != nil {
		s.maintenanceStopOnce.Do(func() { close(s.maintenanceStop) })
	}
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
	if s.meetingRetentionDays > 0 {
		go s.enforceMeetingRetention()
	}
	if s.saveAudio && s.audioRetentionDays > 0 {
		go s.enforceAudioRetention()
	}
	// Retention is a promise about how long data may live, so it cannot run
	// only at startup: a desktop app that stays open for weeks would never
	// delete anything. Re-run the enforcement daily until the store closes.
	if s.meetingRetentionDays > 0 || (s.saveAudio && (s.maxStorageMB > 0 || s.audioRetentionDays > 0)) {
		s.maintenanceStop = make(chan struct{})
		go s.runPeriodicMaintenance()
	}
}

func (s *sqlStore) runPeriodicMaintenance() {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-s.maintenanceStop:
			return
		case <-ticker.C:
			if s.meetingRetentionDays > 0 {
				s.enforceMeetingRetention()
			}
			if s.saveAudio && s.audioRetentionDays > 0 {
				s.enforceAudioRetention()
			}
			if s.saveAudio && s.maxStorageMB > 0 {
				s.enforceStorageLimit()
			}
		}
	}
}
