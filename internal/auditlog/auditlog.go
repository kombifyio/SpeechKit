package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	mu            sync.Mutex
	currentFile   *os.File
	currentDate   string // YYYY-MM-DD
	logDir        string
	retentionDays int
	enabled       bool
)

// ConfigOptions holds all configuration for the audit-log package.
// Use ConfigureFromOptions instead of the older positional-arg Configure.
// The options struct scales for additional sinks (Phase 3+) without growing
// the positional-arg list further.
type ConfigOptions struct {
	// Enabled gates the entire audit log. When false, AppendEvent is a no-op.
	Enabled bool
	// Dir is the directory under which audit-YYYY-MM-DD.log files are written.
	Dir string
	// RetentionDays is the number of days to keep audit files. Zero → 90 days.
	RetentionDays int
	// EventLogEnabled mirrors records to the Windows Event Log source
	// "kombify SpeechKit". No-op on non-Windows.
	EventLogEnabled bool
	// OTLPEndpoint is the host:port of the OTLP HTTP log receiver. Empty
	// disables the OTLP sink.
	OTLPEndpoint string
	// OTLPCertFile and OTLPKeyFile enable mTLS for the OTLP sink when both
	// are non-empty. OTLPCAFile is optional and adds to the root CA pool.
	OTLPCertFile string
	OTLPKeyFile  string
	OTLPCAFile   string
}

// ConfigureFromOptions configures the audit-log package from a ConfigOptions
// value. Must be called once during startup before any AppendEvent call.
// When Enabled = false, all subsequent AppendEvent calls are no-ops.
// The file sink is always the canonical sink; Event Log and OTLP are mirrors.
// Failure to open optional sinks (Event Log, OTLP) is non-fatal and is logged
// at slog.Warn; the file sink remains active regardless.
func ConfigureFromOptions(opts ConfigOptions) error {
	mu.Lock()
	enabled = opts.Enabled
	logDir = opts.Dir
	if opts.RetentionDays <= 0 {
		retentionDays = 90
	} else {
		retentionDays = opts.RetentionDays
	}
	mu.Unlock()

	if opts.EventLogEnabled {
		if err := OpenEventLog(); err != nil {
			slog.Warn("auditlog: Event Log sink open failed; file sink still active", "err", err)
		}
	}
	if opts.OTLPEndpoint != "" {
		if err := ConfigureOTLP(opts.OTLPEndpoint, opts.OTLPCertFile, opts.OTLPKeyFile, opts.OTLPCAFile); err != nil {
			slog.Warn("auditlog: OTLP sink config failed; file sink still active", "err", err)
		}
	}
	return nil
}

// Configure is a backward-compatible wrapper around ConfigureFromOptions.
// Prefer ConfigureFromOptions for new call sites.
// When eventLogEnabled = true, each AppendEvent call also mirrors the record
// to the Windows Event Log source "kombify SpeechKit" (no-op on non-Windows).
// The source must be registered first via InstallEventLogSource (admin-required).
func Configure(enabled_ bool, dir string, retentionDays_ int, eventLogEnabled bool) {
	_ = ConfigureFromOptions(ConfigOptions{
		Enabled:         enabled_,
		Dir:             dir,
		RetentionDays:   retentionDays_,
		EventLogEnabled: eventLogEnabled,
	})
}

// AppendEvent serialises one Record and appends it to today's audit log.
// Returns an error if the write fails; callers MUST log-and-continue
// rather than abort the user action.
//
// The ctx parameter is reserved for future trace propagation; the current
// implementation ignores it.
func AppendEvent(ctx context.Context, ev Record) error {
	_ = ctx
	mu.Lock()
	defer mu.Unlock()

	if !enabled {
		return nil
	}
	if logDir == "" {
		return fmt.Errorf("auditlog: Configure not called")
	}

	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	ev.SchemaVersion = SchemaVersion
	if ev.Outcome == "" {
		ev.Outcome = OutcomeSuccess
	}

	dateKey := ev.Timestamp.UTC().Format("2006-01-02")
	if err := ensureCurrentFileLocked(dateKey); err != nil {
		return err
	}

	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if _, err := currentFile.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	// Mirror to the Windows Event Log sink when enabled. emitToEventLog is a
	// no-op when eventLogHandle is nil (sink not opened) or on non-Windows.
	// Uses its own eventLogMu — no deadlock risk with the outer mu.
	emitToEventLog(ev)
	// Mirror to the OTLP sink when configured. emitToOTLP is a no-op when
	// otlpProvider is nil. Uses its own otlpMu — no deadlock risk with mu.
	emitToOTLP(ev)
	return nil
}

func ensureCurrentFileLocked(dateKey string) error {
	if currentFile != nil && currentDate == dateKey {
		return nil
	}
	if currentFile != nil {
		_ = currentFile.Close()
		currentFile = nil
	}

	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return fmt.Errorf("mkdir audit dir: %w", err)
	}

	path := filepath.Join(logDir, "audit-"+dateKey+".log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- path composed under logDir
	if err != nil {
		return fmt.Errorf("open audit file: %w", err)
	}
	currentFile = f
	currentDate = dateKey

	pruneOldAuditFilesLocked()
	return nil
}

// pruneOldAuditFilesLocked removes audit-YYYY-MM-DD.log files older than
// retentionDays. Errors are silent — pruning failure must never block writes.
func pruneOldAuditFilesLocked() {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	type fileInfo struct {
		path string
		date string
	}
	var files []fileInfo
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		date := strings.TrimSuffix(strings.TrimPrefix(name, "audit-"), ".log")
		files = append(files, fileInfo{path: filepath.Join(logDir, name), date: date})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].date < files[j].date })

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format("2006-01-02")
	for _, f := range files {
		if f.date < cutoff {
			_ = os.Remove(f.path)
		}
	}
}

// ResetForTests releases the current file handle and clears all package
// state. INTENDED FOR TEST USE ONLY. Call from internal/auditlogtest.Reset
// (not directly from outside this module). A direct production call
// silently destroys the audit stream — a compliance bug.
func ResetForTests() {
	CloseEventLog() // must run outside mu to avoid lock-order inversion with eventLogMu
	ShutdownOTLP()  // must run outside mu to avoid lock-order inversion with otlpMu
	mu.Lock()
	defer mu.Unlock()
	if currentFile != nil {
		_ = currentFile.Close()
		currentFile = nil
	}
	currentDate = ""
	logDir = ""
	enabled = false
}
