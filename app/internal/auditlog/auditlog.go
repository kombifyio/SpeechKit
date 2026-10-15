package auditlog

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	// hmacKey keys the local integrity chain (HMAC-SHA256). Empty => plain
	// SHA-256 chain (integrity/ordering only).
	hmacKey []byte
	// lastHash is the Hash of the most recently written record, used as the
	// next record's PrevHash. Seeded from the newest audit file on Configure
	// so the chain survives process restarts and day files.
	lastHash string
	// lastSeq is the Seq of the most recently built record. Seeded like
	// lastHash, so sequence numbers continue across restarts.
	lastSeq uint64
	// configuredOnce tells a process start from a reconfiguration of the
	// running process in the audit.started record.
	configuredOnce bool

	// writeErrorHook is told about the first record that could not be
	// written after each Configure; firstWriteErr holds that error until a
	// hook is registered. A compliance trail that silently stops being
	// written is worse than one that says so.
	writeErrorHook     func(error)
	firstWriteErr      error
	writeErrorReported bool
)

// ConfigOptions holds all configuration for the audit-log package.
// Use ConfigureFromOptions instead of the older positional-arg Configure.
type ConfigOptions struct {
	// Enabled gates the entire audit log. When false, AppendEvent is a no-op.
	Enabled bool
	// Dir is the directory under which audit-YYYY-MM-DD.log files are written.
	Dir string
	// RetentionDays is the number of days to keep audit files. Zero → 90 days.
	RetentionDays int
	// EventLogEnabled mirrors records to the product's Windows Event Log
	// source (productid DisplayName). No-op on non-Windows.
	EventLogEnabled bool
	// OTLPEndpoint is the host:port of the OTLP HTTP log receiver. Empty
	// disables the OTLP sink.
	OTLPEndpoint string
	// OTLPCertFile and OTLPKeyFile enable mTLS for the OTLP sink when both
	// are non-empty. OTLPCAFile is optional and adds to the root CA pool.
	OTLPCertFile string
	OTLPKeyFile  string
	OTLPCAFile   string
	// HMACKey keys the local integrity chain. When empty, the package falls
	// back to the AUDIT_INTEGRITY_KEY env var (hex-encoded); if that is also
	// empty the chain uses plain SHA-256. The local chain is an integrity aid:
	// the writer and the key holder are the user, so tamper evidence is the
	// forwarded copy, not this chain.
	HMACKey []byte
	// ForwardRequired makes every record a blocking forward: AppendEvent
	// waits until at least one forward sink (Event Log, OTLP, Forwarders)
	// acknowledges it, bounded per sink (EventLogForwardBound,
	// OTLPForwardBound). When none does, the package enters the degraded
	// state (ForwardStatus, CloudSessionAllowed) until a forward succeeds.
	ForwardRequired bool
	// Forwarders are forward sinks in addition to the Event Log and OTLP
	// sinks, for hosts that ship their own transport.
	Forwarders []Forwarder
}

// ConfigureFromOptions configures the audit-log package from a ConfigOptions
// value. Must be called once during startup before any AppendEvent call.
// When Enabled = false, all subsequent AppendEvent calls are no-ops.
// The file sink is always written; Event Log and OTLP are forward sinks.
// Failure to open a forward sink is logged at slog.Warn; with
// ForwardRequired it also shows as the degraded state on the first record.
//
// When enabled, the first record written is an audit.started record that
// names the last sequence number found in Dir and the start reason.
func ConfigureFromOptions(opts ConfigOptions) error {
	stopForwarding()
	mu.Lock()
	enabled = opts.Enabled
	logDir = opts.Dir
	if opts.RetentionDays <= 0 {
		retentionDays = 90
	} else {
		retentionDays = opts.RetentionDays
	}
	hmacKey = opts.HMACKey
	if len(hmacKey) == 0 {
		if hexKey := strings.TrimSpace(os.Getenv("AUDIT_INTEGRITY_KEY")); hexKey != "" {
			if decoded, err := hex.DecodeString(hexKey); err == nil {
				hmacKey = decoded
			} else {
				slog.Warn("auditlog: AUDIT_INTEGRITY_KEY is not valid hex; falling back to keyless integrity chain")
			}
		}
	}
	// Start fresh: close any prior file and re-seed the chain head and the
	// sequence from the newest record on disk.
	if currentFile != nil {
		_ = currentFile.Close()
		currentFile = nil
	}
	currentDate = ""
	lastHash = ""
	lastSeq = 0
	firstWriteErr = nil
	writeErrorReported = false
	reason := ""
	if enabled && logDir != "" {
		reason = AuditStartFirst
		if prev, ok := newestRecordInDir(logDir); ok {
			lastHash = prev.Hash
			lastSeq = prev.Seq
			reason = AuditStartProcess
		}
		if configuredOnce {
			reason = AuditStartReconfigured
		}
		configuredOnce = true
	}
	startSeq := lastSeq
	mu.Unlock()

	CloseEventLog()
	ShutdownOTLP()
	if opts.EventLogEnabled {
		if err := OpenEventLog(); err != nil {
			slog.Warn("auditlog: Event Log sink open failed", "err", err)
		}
	}
	if opts.OTLPEndpoint != "" {
		if err := ConfigureOTLP(opts.OTLPEndpoint, opts.OTLPCertFile, opts.OTLPKeyFile, opts.OTLPCAFile); err != nil {
			slog.Warn("auditlog: OTLP sink config failed", "err", err)
		}
	}
	sinks := builtinSinks()
	sinks = append(sinks, opts.Forwarders...)
	configureForwarding(opts.ForwardRequired, opts.Enabled, sinks)

	if reason != "" {
		names := make([]any, 0, len(sinks))
		for _, s := range sinks {
			names = append(names, s.Name())
		}
		if err := AppendEvent(context.Background(), Record{
			Event: EventAuditStarted,
			Resource: map[string]any{
				"last_seq":         startSeq,
				"reason":           reason,
				"forward_required": opts.ForwardRequired,
				"sinks":            names,
			},
		}); err != nil {
			slog.Warn("auditlog: audit.started could not be written", "err", err)
		}
	}
	return nil
}

// Configure is a backward-compatible wrapper around ConfigureFromOptions.
// Prefer ConfigureFromOptions for new call sites.
// When eventLogEnabled = true, each AppendEvent call also mirrors the record
// to the product's Windows Event Log source (no-op on non-Windows).
// The source must be registered first via InstallEventLogSource (admin-required).
func Configure(enabled_ bool, dir string, retentionDays_ int, eventLogEnabled bool) {
	_ = ConfigureFromOptions(ConfigOptions{
		Enabled:         enabled_,
		Dir:             dir,
		RetentionDays:   retentionDays_,
		EventLogEnabled: eventLogEnabled,
	})
}

// AppendEvent serialises one Record, appends it to today's audit log and
// forwards it. Returns an error if the local write fails; callers MUST
// log-and-continue rather than abort the user action.
//
// With ForwardRequired the call waits (bounded) until a forward sink
// acknowledges the record; a missing acknowledgement does not fail the call,
// it puts the package in the degraded state that CloudSessionAllowed reports.
func AppendEvent(ctx context.Context, ev Record) error {
	rec, built, err := writeRecord(ctx, ev)
	if built {
		deliver(ctx, rec)
	}
	return err
}

// writeRecord builds the next record (sequence number, chain fields) and
// appends it to the local file. built reports that the record exists, so it
// can be forwarded even when the local write failed.
func writeRecord(ctx context.Context, ev Record) (rec Record, built bool, err error) {
	mu.Lock()
	rec, built, err = appendEventLocked(ctx, ev)
	var hook func(error)
	if err != nil && firstWriteErr == nil {
		firstWriteErr = err
		if writeErrorHook != nil && !writeErrorReported {
			writeErrorReported = true
			hook = writeErrorHook
		}
	}
	mu.Unlock()
	if hook != nil {
		hook(err)
	}
	return rec, built, err
}

// OnWriteError registers fn to be told, once per configuration, that an
// audit record could not be written (an unwritable directory, a full disk).
// Every caller of AppendEvent logs and carries on, so without this an
// enabled audit trail could stop being written with no one noticing. A
// failure that happened before fn was registered is reported right away.
// fn runs without the package lock held.
func OnWriteError(fn func(error)) {
	mu.Lock()
	writeErrorHook = fn
	var pending error
	if fn != nil && firstWriteErr != nil && !writeErrorReported {
		writeErrorReported = true
		pending = firstWriteErr
	}
	mu.Unlock()
	if pending != nil {
		fn(pending)
	}
}

func appendEventLocked(ctx context.Context, ev Record) (Record, bool, error) {
	_ = ctx // reserved for trace propagation
	if !enabled {
		return Record{}, false, nil
	}
	if logDir == "" {
		return Record{}, false, fmt.Errorf("auditlog: Configure not called")
	}

	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	ev.SchemaVersion = SchemaVersion
	if ev.Outcome == "" {
		ev.Outcome = OutcomeSuccess
	}

	// Normalize so the write-time hash input matches what a verifier
	// reconstructs from disk, number the record, link it to the previous
	// record via PrevHash, then stamp this record's own Hash.
	normalized, err := normalizeRecord(ev)
	if err != nil {
		return Record{}, false, fmt.Errorf("normalize: %w", err)
	}
	ev = normalized
	ev.Seq = lastSeq + 1
	ev.PrevHash = lastHash
	ev.Hash = ""
	hash, err := RecordHash(hmacKey, ev)
	if err != nil {
		return Record{}, false, err
	}
	ev.Hash = hash
	// The sequence number is spent once the record exists: a record whose
	// local write fails is still forwarded, and the local gap it leaves is
	// what the verifier reports.
	lastSeq = ev.Seq

	line, err := json.Marshal(ev)
	if err != nil {
		return ev, true, fmt.Errorf("marshal: %w", err)
	}
	dateKey := ev.Timestamp.UTC().Format("2006-01-02")
	if err := ensureCurrentFileLocked(dateKey); err != nil {
		return ev, true, err
	}
	if _, err := currentFile.Write(append(line, '\n')); err != nil {
		return ev, true, fmt.Errorf("write: %w", err)
	}
	lastHash = ev.Hash
	return ev, true, nil
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

// auditFiles lists the audit-YYYY-MM-DD.log files in dir, oldest first.
func auditFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "audit-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// newestRecordInDir returns the last record of the newest non-empty audit
// file in dir. It is how the chain head and the sequence survive a restart,
// also when the restart happens on a later day than the last write.
func newestRecordInDir(dir string) (Record, bool) {
	names := auditFiles(dir)
	for i := len(names) - 1; i >= 0; i-- {
		if rec, ok := lastRecordInFile(filepath.Join(dir, names[i])); ok {
			return rec, true
		}
	}
	return Record{}, false
}

// lastRecordInFile returns the last record in an audit file. It reads only
// the file tail to avoid loading a full day of records.
func lastRecordInFile(path string) (Record, bool) {
	f, err := os.Open(path) // #nosec G304 -- path composed under logDir
	if err != nil {
		return Record{}, false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return Record{}, false
	}
	const tailWindow = 64 * 1024
	start := int64(0)
	if info.Size() > tailWindow {
		start = info.Size() - tailWindow
	}
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return Record{}, false
	}
	lines := strings.Split(strings.TrimRight(string(buf), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		var rec Record
		if json.Unmarshal([]byte(line), &rec) != nil {
			return Record{}, false // tail window cut mid-line; cannot trust it
		}
		return rec, true
	}
	return Record{}, false
}

// pruneOldAuditFilesLocked removes audit-YYYY-MM-DD.log files older than
// retentionDays. Errors are silent — pruning failure must never block writes.
func pruneOldAuditFilesLocked() {
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays).Format("2006-01-02")
	for _, name := range auditFiles(logDir) {
		date := strings.TrimSuffix(strings.TrimPrefix(name, "audit-"), ".log")
		if date < cutoff {
			_ = os.Remove(filepath.Join(logDir, name))
		}
	}
}

// ResetForTests releases the current file handle and clears all package
// state, as a process exit would. INTENDED FOR TEST USE ONLY. Call from
// app/internal/auditlogtest.Reset (not directly from outside this module).
// A direct production call silently destroys the audit stream — a
// compliance bug.
func ResetForTests() {
	stopForwarding()
	CloseEventLog() // must run outside mu to avoid lock-order inversion with eventLogMu
	ShutdownOTLP()  // must run outside mu to avoid lock-order inversion with otlpMu
	resetForwarding()
	mu.Lock()
	defer mu.Unlock()
	if currentFile != nil {
		_ = currentFile.Close()
		currentFile = nil
	}
	currentDate = ""
	logDir = ""
	enabled = false
	lastHash = ""
	lastSeq = 0
	configuredOnce = false
	writeErrorHook = nil
	firstWriteErr = nil
	writeErrorReported = false
}
