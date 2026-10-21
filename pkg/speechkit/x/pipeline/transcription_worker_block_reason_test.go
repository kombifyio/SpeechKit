package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

// reasonedBlock is what a desktop output adapter returns when it refuses to
// paste and can say why: it wraps ErrOutputBlocked and names the reason.
type reasonedBlock struct{ reason string }

func (e reasonedBlock) Error() string             { return "speechkit: output blocked: " + e.reason }
func (e reasonedBlock) Unwrap() error             { return speechkit.ErrOutputBlocked }
func (e reasonedBlock) OutputBlockReason() string { return e.reason }

type blockingOutput struct{ err error }

func (o blockingOutput) Deliver(context.Context, speechkit.Transcript, any) error { return o.err }

// Regression: the warn line for a refused paste was the same for a window
// that closed, a window that was not in front and an overlay that had focus,
// so neither the user nor the log could tell which it was.
func TestTranscriptionWorkerWarnNamesTheBlockReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "reasoned block",
			err:  reasonedBlock{reason: "target window did not reach foreground"},
			want: "Text available; output was not confirmed: target window did not reach foreground",
		},
		{
			name: "bare sentinel keeps the established line",
			err:  speechkit.ErrOutputBlocked,
			want: "Text available; output was not confirmed",
		},
		{
			name: "untyped failure never leaks the error text",
			err:  errors.New("send paste chord: transcript-shaped detail"),
			want: "Text available; output was not confirmed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observer := &recordingObserver{}
			runner := NewTranscriptionRunner(stubTranscriber{
				transcript: speechkit.Transcript{Text: "hello", Language: "en", Duration: 100 * time.Millisecond},
			}, nil)
			worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
				Timeout:   time.Second,
				QueueSize: 1,
				Runner:    runner,
				Output:    blockingOutput{err: tt.err},
				Observer:  observer,
			})
			if err != nil {
				t.Fatalf("NewTranscriptionWorker() error = %v", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			worker.Start(ctx)
			if err := worker.Submit(speechkit.TranscriptionJob{
				Submission: speechkit.Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "en"},
				Target:     "editor",
			}); err != nil {
				t.Fatalf("Submit() error = %v", err)
			}
			worker.Close()
			worker.Wait()

			observer.mu.Lock()
			logs := append([]string(nil), observer.logs...)
			observer.mu.Unlock()
			var warned []string
			for _, l := range logs {
				if strings.HasPrefix(l, "warn:") && strings.Contains(l, "output was not confirmed") {
					warned = append(warned, strings.TrimPrefix(l, "warn:"))
				}
			}
			if len(warned) != 1 || warned[0] != tt.want {
				t.Fatalf("warn logs = %q, want exactly [%q]", warned, tt.want)
			}
			for _, l := range logs {
				if strings.Contains(l, "transcript-shaped detail") {
					t.Fatalf("adapter error text leaked into the log: %q", l)
				}
			}
		})
	}
}
