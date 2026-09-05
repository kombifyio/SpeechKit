package pipeline_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/pipeline"
)

type finalizationTranscriber struct{}

func (finalizationTranscriber) Transcribe(context.Context, []byte, float64, string) (speechkit.Transcript, error) {
	return speechkit.Transcript{Text: "retained fixture", Language: "en"}, nil
}

type finalizationOutput struct{ attempted atomic.Bool }

func (o *finalizationOutput) Deliver(context.Context, speechkit.Transcript, any) error {
	if o.attempted.Swap(true) {
		return errors.New("duplicate output")
	}
	return speechkit.ErrOutputBlocked
}

type finalizationHistory struct {
	speechkit.Persistence
	started chan context.Context
	release chan struct{}
}

func (h finalizationHistory) SaveTranscription(ctx context.Context, _, _, _, _ string, _, _ int64, _ []byte) error {
	h.started <- ctx
	<-h.release
	return errors.New("history unavailable")
}

type finalizationUpdate struct {
	text   string
	target any
	state  speechkit.TranscriptionFinalization
}

type finalizationObserver struct {
	updates chan finalizationUpdate
	states  chan string
}

func (o finalizationObserver) OnState(state, _ string)                        { o.states <- state }
func (finalizationObserver) OnLog(string, string)                             {}
func (finalizationObserver) OnTranscriptCommitted(speechkit.Transcript, bool) {}
func (o finalizationObserver) OnTranscriptionFinalized(t speechkit.Transcript, f speechkit.TranscriptionFinalization, target any) {
	o.updates <- finalizationUpdate{text: t.Text, state: f, target: target}
}

// Regression: the worker announced completion before output and exposed neither
// output failure nor history failure. Slow history must not delay recovery.
func TestWorkerFinalizationSeparatesOutputFromAsyncHistory(t *testing.T) {
	history := finalizationHistory{started: make(chan context.Context, 2), release: make(chan struct{})}
	observer := finalizationObserver{updates: make(chan finalizationUpdate, 8), states: make(chan string, 8)}
	output := &finalizationOutput{}
	worker, err := pipeline.NewTranscriptionWorker(pipeline.TranscriptionWorkerConfig{
		Runner: pipeline.NewTranscriptionRunner(finalizationTranscriber{}, history),
		Output: output, Observer: observer,
	})
	if err != nil {
		t.Fatal(err)
	}
	type scopeKey struct{}
	ctx := context.WithValue(context.Background(), scopeKey{}, "retention-scope")
	worker.Start(ctx)
	released := false
	t.Cleanup(func() {
		if !released {
			close(history.release)
		}
		worker.Close()
		worker.Wait()
	})
	job := speechkit.TranscriptionJob{
		Submission: speechkit.Submission{WAV: []byte("synthetic"), SessionID: 1, SegmentID: 1, SegmentFinal: true},
		Target:     "original target",
	}
	if err := worker.Submit(job); err != nil {
		t.Fatal(err)
	}
	read := func() finalizationUpdate {
		t.Helper()
		select {
		case update := <-observer.updates:
			return update
		case <-time.After(2 * time.Second):
			t.Fatal("missing completion notification")
			return finalizationUpdate{}
		}
	}
	requested := read()
	blocked := read()
	if requested.state.Output != speechkit.OutputRequested || blocked.state.Output != speechkit.OutputBlocked ||
		blocked.state.Persistence != speechkit.PersistencePending ||
		blocked.text != "retained fixture" || blocked.target != job.Target || requested.state.ID != blocked.state.ID {
		t.Fatal("blocked output was not recoverable before history finished")
	}
	select {
	case storeCtx := <-history.started:
		if storeCtx.Value(scopeKey{}) != "retention-scope" {
			t.Fatal("async history lost the host retention/scope context")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("output failure skipped history")
	}
	// Re-submitting the same committed recognition must not re-STT/re-paste.
	if err := worker.Submit(job); err != nil {
		t.Fatal(err)
	}
	close(history.release)
	released = true
	failed := read()
	if failed.state.ID != blocked.state.ID || failed.state.Output != speechkit.OutputBlocked ||
		failed.state.Persistence != speechkit.PersistenceFailed {
		t.Fatal("history failure overwrote the output result")
	}
	worker.Close()
	worker.Wait()
	close(observer.states)
	for state := range observer.states {
		if state == "done" {
			t.Fatal("blocked output emitted a legacy success notification")
		}
	}
	select {
	case <-observer.updates:
		t.Fatal("duplicate recognition generated a second completion")
	default:
	}
}
