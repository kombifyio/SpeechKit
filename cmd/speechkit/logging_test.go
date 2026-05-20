package main

import (
	"bytes"
	"errors"
	"log/slog"
	"testing"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write(_ []byte) (int, error) {
	return 0, w.err
}

func TestFanoutWriterContinuesWritingAfterStdoutError(t *testing.T) {
	var logFile bytes.Buffer
	writer := fanoutWriter{
		writers: []writerTarget{
			{name: "stdout", writer: failingWriter{err: errors.New("invalid handle")}},
			{name: "logfile", writer: &logFile},
		},
	}

	payload := []byte("logging initialized")
	n, err := writer.Write(payload)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write count = %d, want %d", n, len(payload))
	}
	if got := logFile.String(); got != string(payload) {
		t.Fatalf("log file output = %q, want %q", got, string(payload))
	}
}

func TestConfigureLoggingLimitsAppliesNonZero(t *testing.T) {
	origSize := maxLogFileSize
	origFiles := maxLogFiles
	t.Cleanup(func() {
		maxLogFileSize = origSize
		maxLogFiles = origFiles
	})

	configureLoggingLimits(100, 60) // explicitly different from any default

	if maxLogFileSize != 100*1024*1024 {
		t.Errorf("maxLogFileSize: want 100MB (%d), got %d", 100*1024*1024, maxLogFileSize)
	}
	if maxLogFiles != 60 {
		t.Errorf("maxLogFiles: want 60, got %d", maxLogFiles)
	}
}

func TestConfigureLoggingLevelHonorsConfig(t *testing.T) {
	t.Setenv("SPEECHKIT_LOG_LEVEL", "")
	cases := []struct {
		in        string
		wantLevel slog.Level
		wantOff   bool
		wantOut   string
	}{
		{"debug", slog.LevelDebug, false, "debug"},
		{"info", slog.LevelInfo, false, "info"},
		{"warn", slog.LevelWarn, false, "warn"},
		{"warning", slog.LevelWarn, false, "warn"},
		{"error", slog.LevelError, false, "error"},
		{"off", slog.LevelError + 1, true, "off"},
		{"disabled", slog.LevelError + 1, true, "off"},
		{"", slog.LevelInfo, false, "info"},
		{"BOGUS", slog.LevelInfo, false, "info"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := configureLoggingLevel(c.in)
			if got != c.wantOut {
				t.Fatalf("returned label = %q, want %q", got, c.wantOut)
			}
			if logLevelVar.Level() != c.wantLevel {
				t.Fatalf("logLevelVar = %v, want %v", logLevelVar.Level(), c.wantLevel)
			}
			if loggingDisabled() != c.wantOff {
				t.Fatalf("loggingDisabled = %v, want %v", loggingDisabled(), c.wantOff)
			}
		})
	}
}

func TestConfigureLoggingLevelEnvOverridesConfig(t *testing.T) {
	t.Setenv("SPEECHKIT_LOG_LEVEL", "debug")
	got := configureLoggingLevel("info")
	if got != "debug" {
		t.Fatalf("env-overridden level = %q, want %q", got, "debug")
	}
	if logLevelVar.Level() != slog.LevelDebug {
		t.Fatalf("logLevelVar = %v, want debug", logLevelVar.Level())
	}
}

func TestFanoutWriterShortCircuitsWhenLogDisabled(t *testing.T) {
	t.Setenv("SPEECHKIT_LOG_LEVEL", "")
	configureLoggingLevel("off")
	t.Cleanup(func() { configureLoggingLevel("info") })

	var sink bytes.Buffer
	writer := fanoutWriter{writers: []writerTarget{{name: "sink", writer: &sink}}}
	payload := []byte("should never reach sink")
	n, err := writer.Write(payload)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(payload) {
		t.Fatalf("Write count = %d, want %d", n, len(payload))
	}
	if sink.Len() != 0 {
		t.Fatalf("sink received %d bytes despite logging off; got: %q", sink.Len(), sink.String())
	}
}

func TestConfigureLoggingLimitsKeepsDefaultsOnZero(t *testing.T) {
	origSize := maxLogFileSize
	origFiles := maxLogFiles
	t.Cleanup(func() {
		maxLogFileSize = origSize
		maxLogFiles = origFiles
	})

	configureLoggingLimits(0, 0)

	if maxLogFileSize != origSize {
		t.Errorf("maxLogFileSize changed on zero input: want %d, got %d", origSize, maxLogFileSize)
	}
	if maxLogFiles != origFiles {
		t.Errorf("maxLogFiles changed on zero input: want %d, got %d", origFiles, maxLogFiles)
	}
}
