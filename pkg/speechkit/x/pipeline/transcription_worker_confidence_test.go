package pipeline

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestLowConfidenceWords(t *testing.T) {
	words := []speechkit.WordConfidence{
		{Text: "Ich", Confidence: 0.99},
		{Text: "Ultracord", Confidence: 0.42},
		{Text: "mit", Confidence: 0.95},
		{Text: "Stacket", Confidence: 0.55},
		{Text: "stacket", Confidence: 0.51}, // case-insensitive duplicate of "Stacket"
	}

	terms, minConf := speechkit.LowConfidenceWords(words, 0.6)
	if got := strings.Join(terms, ","); got != "Ultracord,Stacket" {
		t.Fatalf("terms = %q, want \"Ultracord,Stacket\" (distinct, below-threshold only)", got)
	}
	if minConf < 0.41 || minConf > 0.43 {
		t.Fatalf("minConfidence = %v, want ~0.42 (floor across all words)", minConf)
	}

	if terms, gotMin := speechkit.LowConfidenceWords(words, 0); terms != nil || gotMin != 0 {
		t.Fatalf("threshold<=0 must disable detection, got (%#v, %v)", terms, gotMin)
	}
	if terms, _ := speechkit.LowConfidenceWords(nil, 0.6); terms != nil {
		t.Fatalf("nil words must yield nil terms, got %#v", terms)
	}
}

// TestTranscriptionWorkerLogsLowConfidenceWords is the regression guard for the
// "words vanish silently" complaint: flag low confidence without recording
// sensitive recognized terms in the log.
func TestTranscriptionWorkerLogsLowConfidenceWords(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: speechkit.Transcript{
			Text:     "Ich nutze Ultracord",
			Provider: "deepgram",
			Duration: 100 * time.Millisecond,
			Words: []speechkit.WordConfidence{
				{Text: "Ich", Confidence: 0.99},
				{Text: "nutze", Confidence: 0.98},
				{Text: "Ultracord", Confidence: 0.41},
			},
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:                time.Second,
		QueueSize:              1,
		Runner:                 runner,
		Output:                 output,
		Observer:               observer,
		LowConfidenceThreshold: 0.6,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	var found string
	observer.mu.Lock()
	for _, l := range observer.logs {
		if strings.HasPrefix(l, "warn:") {
			found = l
		}
	}
	observer.mu.Unlock()

	if found == "" {
		t.Fatalf("expected a low-confidence warn log, got logs = %#v", observer.logs)
	}
	if strings.Contains(found, "Ultracord") || strings.Contains(found, "nutze") {
		t.Fatal("confidence warning leaked recognized words")
	}
}

// A provider can answer successfully with a zero-length transcript — Deepgram
// does exactly that when the pinned language does not match the speech: HTTP
// 200, no error, no words. The worker used to run the whole commit path on that
// empty string, so the user's speech disappeared with no log line, no visible
// state and no history entry. Empty-final is now a named outcome; this test
// pins all three halves of it, plus the safety property that nothing is
// delivered.
func TestTranscriptionWorkerNamesAnEmptyFinalTranscript(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: speechkit.Transcript{
			Text:     "   ",
			Provider: "deepgram",
			Model:    "nova-3",
			Language: "de",
			Duration: 100 * time.Millisecond,
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:   time.Second,
		QueueSize: 1,
		Runner:    runner,
		Output:    output,
		Observer:  observer,
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)

	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}

	worker.Close()
	worker.Wait()

	observer.mu.Lock()
	logs := append([]string(nil), observer.logs...)
	states := append([]string(nil), observer.states...)
	committed := append([]string(nil), observer.committed...)
	observer.mu.Unlock()

	var warned string
	for _, l := range logs {
		if strings.HasPrefix(l, "warn:") && strings.Contains(l, speechkit.EmptyFinalTranscriptMessage) {
			warned = l
		}
	}
	if warned == "" {
		t.Fatalf("an empty final transcript must warn, got logs = %#v", logs)
	}
	// The log has to carry enough to diagnose the pinned-language case, which
	// is the whole reason this outcome exists.
	for _, want := range []string{"deepgram", "nova-3", "de"} {
		if !strings.Contains(warned, want) {
			t.Errorf("empty-final warn log must name %q for diagnosis, got %q", want, warned)
		}
	}

	var sawVisibleState bool
	for _, s := range states {
		if strings.Contains(s, speechkit.EmptyFinalTranscriptMessage) {
			sawVisibleState = true
		}
	}
	if !sawVisibleState {
		t.Fatalf("an empty final transcript must set a visible state, got states = %#v", states)
	}

	if delivered := output.snapshot(); len(delivered) != 0 {
		t.Fatalf("an empty transcript must never be delivered, got %#v", delivered)
	}
	if len(committed) != 0 {
		t.Fatalf("an empty transcript must not be announced as committed, got %#v", committed)
	}
	if observer.finalization.Recognition != speechkit.RecognitionEmpty ||
		observer.finalization.Output != speechkit.OutputNotRequested {
		t.Fatal("empty recognition was reported as output")
	}
}

func TestTranscriptionWorkerSkipsLowConfidenceWhenDisabled(t *testing.T) {
	observer := &recordingObserver{}
	output := &recordingOutput{}
	runner := NewTranscriptionRunner(stubTranscriber{
		transcript: speechkit.Transcript{
			Text:     "Ultracord",
			Provider: "deepgram",
			Duration: 50 * time.Millisecond,
			Words:    []speechkit.WordConfidence{{Text: "Ultracord", Confidence: 0.10}},
		},
	}, nil)

	worker, err := NewTranscriptionWorker(TranscriptionWorkerConfig{
		Timeout:                time.Second,
		QueueSize:              1,
		Runner:                 runner,
		Output:                 output,
		Observer:               observer,
		LowConfidenceThreshold: 0, // disabled
	})
	if err != nil {
		t.Fatalf("NewTranscriptionWorker() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	worker.Start(ctx)
	if err := worker.Submit(speechkit.TranscriptionJob{
		Submission: speechkit.Submission{WAV: []byte("wav"), DurationSecs: 0.2, Language: "de"},
		Target:     "editor",
	}); err != nil {
		t.Fatalf("Submit() error = %v", err)
	}
	worker.Close()
	worker.Wait()

	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, l := range observer.logs {
		if strings.HasPrefix(l, "warn:") {
			t.Fatalf("threshold disabled, must not emit low-confidence log, got %q", l)
		}
	}
}
