package main

import (
	"bytes"
	"errors"
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
