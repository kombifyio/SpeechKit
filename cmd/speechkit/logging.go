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

type writerTarget struct {
	name   string
	writer io.Writer
}

type fanoutWriter struct {
	writers []writerTarget
}

func (w fanoutWriter) Write(p []byte) (int, error) {
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
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	handler := slog.NewJSONHandler(multiWriter, opts)
	slog.SetDefault(slog.New(handler))

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
