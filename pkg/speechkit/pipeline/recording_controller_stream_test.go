package pipeline

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestRecordingControllerProviderStreamCommitsFinalWithoutBatchDuplicate(t *testing.T) {
	full := []byte(strings.Repeat("z", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	provider := &fakeDictationStreamProvider{
		stream: newFakeDictationStream(speechkit.DictationStreamEvent{
			Text:      "native final",
			IsFinal:   true,
			Provider:  "fake-stream",
			Model:     "stream-model",
			SegmentID: 1,
		}),
	}
	sink := &fakeDictationStreamSink{}
	collector := &fakeCollector{readySegments: []dictationSegment{{pcm: []byte(strings.Repeat("a", 6400))}}}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetDictationStream(provider, sink)

	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		Target:         "editor",
		StreamSegments: true,
		ProviderStream: true,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))
	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("batch jobs before Stop = %d, want 0 while native stream is active", got)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("batch jobs after native final = %d, want 0", got)
	}
	finals := sink.finalEvents()
	if len(finals) != 1 {
		t.Fatalf("final stream events = %d, want 1", len(finals))
	}
	if got, want := finals[0].SessionID, uint64(1); got != want {
		t.Fatalf("stream final session id = %d, want %d", got, want)
	}
	if got := provider.stream.pcmFrames(); got == 0 {
		t.Fatal("provider stream received no PCM frames")
	}
	if len(provider.opts) != 1 || !provider.opts[0].InterimResults {
		t.Fatalf("provider opts = %#v, want interim stream opts", provider.opts)
	}
}

func TestRecordingControllerProviderStreamDialTimeoutFallsBackWithoutBlockingCapture(t *testing.T) {
	prev := providerStreamDialTimeout
	providerStreamDialTimeout = 30 * time.Millisecond
	t.Cleanup(func() { providerStreamDialTimeout = prev })

	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("z", 6400))}
	submitter := &fakeSubmitter{}
	provider := &blockingDictationStreamProvider{block: 5 * time.Second}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	controller.SetDictationStream(provider, &fakeDictationStreamSink{})

	started := time.Now()
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		ProviderStream: true,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 300*time.Millisecond {
		t.Fatalf("Start blocked %s waiting for provider stream; hotkey loop would miss KeyUp", elapsed)
	}
	if !recorder.started {
		t.Fatal("microphone must open even when the provider stream handshake hangs")
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerProviderStreamStartFailureFallsBackToSegmentBatch(t *testing.T) {
	readyPCM := []byte(strings.Repeat("a", 6400))
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("z", 6400))}
	submitter := &fakeSubmitter{}
	provider := &fakeDictationStreamProvider{err: errors.New("dial failed")}
	sink := &fakeDictationStreamSink{}
	collector := &fakeCollector{readySegments: []dictationSegment{{pcm: readyPCM}}}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return collector
	})
	controller.SetDictationStream(provider, sink)

	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		StreamSegments: true,
		ProviderStream: true,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))

	if len(submitter.jobs) != 1 {
		t.Fatalf("segment-batch fallback jobs = %d, want 1", len(submitter.jobs))
	}
	if got, want := string(submitter.jobs[0].PCM), string(readyPCM); got != want {
		t.Fatalf("fallback job PCM = %q, want ready segment", got)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}
