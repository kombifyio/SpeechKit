package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	// maxLogFileSize and maxLogFiles default to the same values the config
	// loader applies ([logging] max_file_size_mb=50, max_files=30). They are
	// vars (not consts) so configureLoggingLimits can adjust them after
	// config is loaded. Aligning the historical defaults with the config
	// defaults eliminates the rotation-mismatch window between
	// initAppLogging (which uses these vars directly) and configureLoggingLimits
	// (which runs later, after config loads).
	maxLogFileSize int64 = 50 * 1024 * 1024
	maxLogFiles    int   = 30

	// logLevelVar is mutated by configureLoggingLevel once config has been
	// parsed. The slog handler in initAppLogging holds a pointer to it, so
	// runtime changes take effect immediately without rebuilding the handler.
	logLevelVar = new(slog.LevelVar)

	// logDisabled flips to true when level="off" so writers can shortcut to
	// io.Discard. Atomic-safe because configureLoggingLevel is called once
	// during startup and slog.LevelVar handles the read side.
	logDisabled bool
)

// configureLoggingLimits is called once during startup with values resolved
// from internal/config.LoggingConfig. Zero values keep the historical
// defaults so existing tests do not need to know about config.
func configureLoggingLimits(maxFileSizeMB int, maxFiles int) {
	if maxFileSizeMB > 0 {
		maxLogFileSize = int64(maxFileSizeMB) * 1024 * 1024
	}
	if maxFiles > 0 {
		maxLogFiles = maxFiles
	}
}

// configureLoggingLevel applies the runtime log level resolved from
// cfg.Logging.Level. SPEECHKIT_LOG_LEVEL overrides the config value so
// support engineers can flip on debug without editing config.toml. The
// recognised levels are "debug", "info", "warn", "error", "off". Unknown
// values fall back to "info" (the desktop client's safe default) and a
// warning is emitted. Returns the level actually applied — for tests and
// the startup log line.
func configureLoggingLevel(configured string) string {
	resolved := strings.TrimSpace(strings.ToLower(configured))
	if env := strings.TrimSpace(strings.ToLower(os.Getenv("SPEECHKIT_LOG_LEVEL"))); env != "" {
		resolved = env
	}
	if resolved == "" {
		resolved = "info"
	}
	switch resolved {
	case "debug":
		logLevelVar.Set(slog.LevelDebug)
		logDisabled = false
	case "info":
		logLevelVar.Set(slog.LevelInfo)
		logDisabled = false
	case "warn", "warning":
		logLevelVar.Set(slog.LevelWarn)
		logDisabled = false
		resolved = "warn"
	case "error":
		logLevelVar.Set(slog.LevelError)
		logDisabled = false
	case "off", "disabled", "silent", "none":
		// LevelError + writer-side discard means even Error/Warn calls are
		// no-ops on the slog side AND nothing reaches stdout/file.
		logLevelVar.Set(slog.LevelError + 1)
		logDisabled = true
		resolved = "off"
	default:
		slog.Warn("unknown log level, falling back to info", "configured", configured)
		logLevelVar.Set(slog.LevelInfo)
		logDisabled = false
		resolved = "info"
	}
	return resolved
}

// loggingDisabled reports whether the logging level has been set to "off".
// Hot-path callers can check this before building expensive log argument
// values (e.g. formatting payload summaries) — useful for slog.Info call
// sites in tight loops where even argument evaluation is measurable.
func loggingDisabled() bool {
	return logDisabled
}

// logLevelSourceLabel reports whether the effective log level came from
// the SPEECHKIT_LOG_LEVEL env var or cfg.Logging.Level. Used purely for
// the diagnostic startup line so the operator can see why their setting
// is or isn't honoured.
func logLevelSourceLabel(configured string) string {
	if env := strings.TrimSpace(os.Getenv("SPEECHKIT_LOG_LEVEL")); env != "" {
		return "env:SPEECHKIT_LOG_LEVEL"
	}
	if strings.TrimSpace(configured) == "" {
		return "default"
	}
	return "config:logging.level"
}

type writerTarget struct {
	name   string
	writer io.Writer
}

type fanoutWriter struct {
	writers []writerTarget
}

func (w fanoutWriter) Write(p []byte) (int, error) {
	if loggingDisabled() {
		return len(p), nil
	}

	var (
		successfulWrites int
		firstErr         error
	)

	for _, target := range w.writers {
		if target.writer == nil {
			continue
		}
		n, err := target.writer.Write(p)
		if err == nil && n == len(p) {
			successfulWrites++
			continue
		}
		if err == nil {
			err = io.ErrShortWrite
		}
		if firstErr == nil {
			firstErr = err
		}
	}

	if successfulWrites > 0 {
		return len(p), nil
	}
	if firstErr != nil {
		return 0, firstErr
	}
	return len(p), nil
}

func initAppLogging() (string, func()) {
	exePath, err := os.Executable()
	if err != nil {
		slog.Warn("resolve executable path for logging", "err", err)
		return "", func() {}
	}

	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		slog.Warn("create log directory", "err", err)
		return "", func() {}
	}

	logPath := filepath.Join(logDir, "speechkit.log")
	rotateLogFile(logPath, logDir)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600) // #nosec G304 -- logPath is scoped under the executable's logs directory.
	if err != nil {
		slog.Warn("open log file", "err", err)
		return logPath, func() {}
	}

	multiWriter := fanoutWriter{
		writers: []writerTarget{
			{name: "stdout", writer: os.Stdout},
			{name: "logfile", writer: logFile},
		},
	}
	// Default-off semantics: before config has been parsed we still know
	// SPEECHKIT_LOG_LEVEL from the environment. Apply it now so the brief
	// window between initAppLogging and configureLoggingLevel (~10 startup
	// lines) honours the user's intent: env=off keeps the file empty, env=
	// info logs everything, no env → silent until config loads (and config
	// default is "off", so still silent).
	earlyLevel := strings.TrimSpace(strings.ToLower(os.Getenv("SPEECHKIT_LOG_LEVEL")))
	switch earlyLevel {
	case "debug":
		logLevelVar.Set(slog.LevelDebug)
	case "info":
		logLevelVar.Set(slog.LevelInfo)
	case "warn", "warning":
		logLevelVar.Set(slog.LevelWarn)
	case "error":
		logLevelVar.Set(slog.LevelError)
	case "off", "disabled", "silent", "none":
		logLevelVar.Set(slog.LevelError + 1)
		logDisabled = true
	default:
		// No env override — match the privacy-first config default. The
		// later configureLoggingLevel call will lift this if config.toml
		// sets a higher verbosity.
		logLevelVar.Set(slog.LevelError + 1)
		logDisabled = true
	}
	opts := &slog.HandlerOptions{Level: logLevelVar}
	handler := slog.NewJSONHandler(multiWriter, opts)
	slog.SetDefault(slog.New(handler))

	// This first line is suppressed under default-off; it's intentionally
	// kept (a) so debug-on operators still get a startup marker, (b) so
	// support engineers can confirm the log file location when they flip
	// SPEECHKIT_LOG_LEVEL on.
	slog.Info("logging initialized", "path", logPath)

	return logPath, func() {
		_ = logFile.Close()
	}
}

// rotateLogFile renames the current log if it exceeds maxLogFileSize,
// then prunes old rotated logs to keep at most maxLogFiles.
func rotateLogFile(logPath, logDir string) {
	info, err := os.Stat(logPath)
	if err != nil || info.Size() < maxLogFileSize {
		return
	}

	rotated := fmt.Sprintf("speechkit-%s.log", time.Now().Format("20060102-150405"))
	if err := os.Rename(logPath, filepath.Join(logDir, rotated)); err != nil {
		slog.Warn("rotate log file", "err", err)
		return
	}

	pruneOldLogs(logDir)
}

func pruneOldLogs(logDir string) {
	entries, err := os.ReadDir(logDir)
	if err != nil {
		return
	}

	var rotated []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "speechkit-") && strings.HasSuffix(name, ".log") {
			rotated = append(rotated, name)
		}
	}

	if len(rotated) <= maxLogFiles {
		return
	}

	sort.Strings(rotated)
	for _, name := range rotated[:len(rotated)-maxLogFiles] {
		_ = os.Remove(filepath.Join(logDir, name))
	}
}
