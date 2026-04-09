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

const (
	maxLogFileSize = 5 * 1024 * 1024 // 5 MB
	maxLogFiles    = 3
)

func initAppLogging() (string, func()) {
	exePath, err := os.Executable()
	if err != nil {
		slog.Warn("resolve executable path for logging", "err", err)
		return "", func() {}
	}

	logDir := filepath.Join(filepath.Dir(exePath), "logs")
	if err := os.MkdirAll(logDir, 0700); err != nil {
		slog.Warn("create log directory", "err", err)
		return "", func() {}
	}

	logPath := filepath.Join(logDir, "speechkit.log")
	rotateLogFile(logPath, logDir)

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		slog.Warn("open log file", "err", err)
		return logPath, func() {}
	}

	multiWriter := io.MultiWriter(os.Stdout, logFile)
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
		os.Remove(filepath.Join(logDir, name))
	}
}
