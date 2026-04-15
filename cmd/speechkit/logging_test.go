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
