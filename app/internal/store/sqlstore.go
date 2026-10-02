package store

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
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
	transcriptRetentionDays int
	transcriptionModelHints map[string]string
	defaultScope            speechstorage.Scope
	scopePolicy             speechstorage.ScopePolicy
	// sweepOrphanAudio enables sweepOrphanedAudio. Only a store that can be
	// sure it is the sole owner of audioDir may delete files no row of its
	// own references; see NewSQLiteStore.
	sweepOrphanAudio bool
	// maintenanceMu guards the periodic maintenance loop: it starts at most
	// once per store (the first write that needs it), and Close stops it.
	maintenanceMu      sync.Mutex
	maintenanceStop    chan struct{}
	maintenanceStarted bool
	maintenanceClosed  bool
}

// DB exposes the underlying *sql.DB so adjacent packages can build their own
// table-scoped persisters without the base Store interface enumerating every
// optional capability. Callers must treat the handle as read-mostly: it is
// owned by the Store and must not be closed.
func (s *sqlStore) DB() *sql.DB { return s.db }

func (s *sqlStore) Close() error {
	s.maintenanceMu.Lock()
	if !s.maintenanceClosed {
		s.maintenanceClosed = true
		if s.maintenanceStop != nil {
			close(s.maintenanceStop)
		}
	}
	s.maintenanceMu.Unlock()
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
// audio. The filename is "<prefix><unix-nano><ext>"; orphanAudioNamePattern
// matches exactly these names, so keep the two in step.
//
// The file exists before the database row that references it. Callers must
// remove it (discardUncommittedAudio) when their transaction does not commit;
// the orphan sweep catches what a crash in between leaves behind.
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

// discardUncommittedAudio removes a file persistAudio wrote for a row that
// was never committed. Without it a failed insert (SQLITE_BUSY, a cancelled
// request) left a recording on disk that no row, retention pass or erasure
// would ever find again.
func (s *sqlStore) discardUncommittedAudio(path string, committed bool) {
	if committed || path == "" {
		return
	}
	if err := s.removeManagedFile(path); err != nil {
		slog.Warn("store: uncommitted audio not removed", "err", err)
	}
}

func (s *sqlStore) scheduleMaintenance() {
	if !s.maintenanceConfigured() {
		return
	}
	go s.runMaintenancePass()
	// Retention is a promise about how long data may live, so it cannot run
	// only at startup: a desktop app that stays open for weeks would never
	// delete anything. Re-run the enforcement daily until the store closes.
	s.startPeriodicMaintenance()
}

// startMaintenance applies the configured retention once when the store
// opens and keeps applying it daily, whether or not anything is written
// later: a store that is only read from must still forget on schedule.
func (s *sqlStore) startMaintenance() {
	if !s.maintenanceConfigured() {
		return
	}
	s.runMaintenancePass()
	s.startPeriodicMaintenance()
}

// maintenanceConfigured reports whether any retention or size limit applies.
// Audio limits do not depend on save_audio: turning saving off stops new
// recordings, it must not exempt the ones already on disk from deletion.
func (s *sqlStore) maintenanceConfigured() bool {
	return s.meetingRetentionDays > 0 || s.transcriptRetentionDays > 0 ||
		s.maxStorageMB > 0 || s.audioRetentionDays > 0
}

// runMaintenancePass applies every configured retention rule once. The
// orphan sweep (where enabled, see sweepOrphanAudio) runs whenever any rule
// does, because it is what makes the
// audio size cap and retention trustworthy: they only see files a row
// points at.
func (s *sqlStore) runMaintenancePass() {
	if s.meetingRetentionDays > 0 {
		s.enforceMeetingRetention()
	}
	if s.transcriptRetentionDays > 0 {
		s.enforceTranscriptRetention()
	}
	if s.audioRetentionDays > 0 {
		s.enforceAudioRetention()
	}
	if s.sweepOrphanAudio {
		s.sweepOrphanedAudio(time.Now())
	}
	if s.maxStorageMB > 0 {
		s.enforceStorageLimit()
	}
}

// startPeriodicMaintenance starts the daily loop once per store. Every write
// calls scheduleMaintenance, so starting a loop per call would leak one
// goroutine per saved transcript or note.
func (s *sqlStore) startPeriodicMaintenance() {
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.maintenanceStarted || s.maintenanceClosed {
		return
	}
	s.maintenanceStarted = true
	s.maintenanceStop = make(chan struct{})
	go s.runPeriodicMaintenance(s.maintenanceStop)
}

func (s *sqlStore) runPeriodicMaintenance(stop <-chan struct{}) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			s.runMaintenancePass()
		}
	}
}
