package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestRecordingControllerStopWithNoSegmentsResetsIdle(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return &fakeCollector{}
	})
	controller.SetFragmentSegments(true) // the no-speech skip only applies to the opt-in segment path

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("submitted jobs = %d, want 0", got)
	}
	if got := observer.states; len(got) < 2 || got[len(got)-1] != "idle:" {
		t.Fatalf("states = %#v, want final idle", got)
	}
	if !observer.hasLog("No speech segments detected") {
		t.Fatalf("observer logs = %v, want no-segments log", observer.logs)
	}
}

// TestRecordingControllerSkipsShortNoiseFloorCapture pins the junk-capture
// gate: a sub-2s capture whose signal never rises above the noise floor
// (accidental hotkey tap, key chatter) must be dropped before it costs a
// provider roundtrip. The gate is pure energy — a short but audible capture
// must still be submitted.
func TestRecordingControllerSkipsShortNoiseFloorCapture(t *testing.T) {
	silent := make([]byte, 6400) // 0.2s of digital silence
	recorder := &fakeRecorder{stopPCM: silent}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, nil)

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("submitted jobs = %d, want 0 for noise-floor capture", got)
	}
	if !observer.hasLog("No audio above noise floor") {
		t.Fatalf("observer logs = %v, want noise-floor skip log", observer.logs)
	}
	if got := observer.states; len(got) == 0 || got[len(got)-1] != "idle:" {
		t.Fatalf("states = %#v, want final idle", got)
	}

	// Same duration but audible content must be submitted.
	recorder.stopPCM = []byte(strings.Repeat("a", 6400))
	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() (audible) error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() (audible) error = %v", err)
	}
	if got := len(submitter.jobs); got != 1 {
		t.Fatalf("submitted jobs = %d, want 1 for audible capture", got)
	}
	if got := observer.states; len(got) == 0 || got[len(got)-1] != "processing:" {
		t.Fatalf("states = %#v, want final processing after successful submit", got)
	}
}

// TestRecordingControllerTranscribesFullCaptureByDefault is the dropped-words
// regression guard: in the default (non-fragmenting) mode the job must carry the
// FULL captured PCM and NO segments, even when the VAD collector produced
// segments — so the worker transcribes the complete audio and the crude RMS VAD
// can never excise speech before STT.
func TestRecordingControllerTranscribesFullCaptureByDefault(t *testing.T) {
	full := []byte(strings.Repeat("a", 6400) + strings.Repeat("b", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: []byte(strings.Repeat("a", 6400)), paragraph: false},
		}}
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
	job := submitter.jobs[0]
	if len(job.Segments) != 0 {
		t.Fatalf("job.Segments = %d, want 0 (full-capture default must not fragment)", len(job.Segments))
	}
	if string(job.PCM) != string(full) {
		t.Fatalf("job submission PCM = %d bytes, want the full %d-byte capture", len(job.PCM), len(full))
	}
}

func TestRecordingControllerStopTailDelayKeepsCaptureOpen(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	collector := &fakeCollector{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return collector
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	handler := recorder.pcmHandler
	if handler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Stop(speechkit.RecordingStopOptions{
			Label:     "Captured",
			TailDelay: 40 * time.Millisecond,
		})
	}()

	time.Sleep(10 * time.Millisecond)
	if !controller.IsRecording() {
		t.Fatal("controller should still report recording during stop tail delay")
	}
	handler([]byte("tail"))

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop() timed out")
	}

	if len(collector.fedPCM) != 1 {
		t.Fatalf("collector FeedPCM calls = %d, want 1", len(collector.fedPCM))
	}
	if got, want := string(collector.fedPCM[0]), "tail"; got != want {
		t.Fatalf("collector FeedPCM = %q, want %q", got, want)
	}
	if controller.IsRecording() {
		t.Fatal("controller should not report recording after stop completes")
	}
}

func TestRecordingControllerCancelStopsRecorderWithoutSubmitting(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return &fakeCollector{}
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Cancel(speechkit.RecordingCancelOptions{Label: "Discarded for mode switch"}); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	if controller.IsRecording() {
		t.Fatal("controller should not report recording after cancel")
	}
	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("submitted jobs = %d, want 0 for cancel", got)
	}
	if got := recorder.pcmHandler; got != nil {
		t.Fatal("recorder handler should be cleared after cancel")
	}
	if !observer.hasLog("Discarded for mode switch") {
		t.Fatalf("observer logs = %v, want cancel label", observer.logs)
	}
}

func TestRecordingControllerDiscardsStaleCaptureBuffer(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: makePCMForDuration(120 * time.Second)}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, nil)
	startedAt := time.Date(2026, 6, 23, 12, 11, 4, 0, time.UTC)
	now := startedAt
	controller.now = func() time.Time { return now }

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	now = startedAt.Add(6 * time.Second)
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("submitted jobs = %d, want 0 for stale capture buffer", got)
	}
	if !observer.hasLog("stale microphone buffer suspected") {
		t.Fatalf("observer logs = %v, want stale-buffer diagnostic", observer.logs)
	}
	if got := observer.states[len(observer.states)-1]; got != "idle:" {
		t.Fatalf("last state = %q, want idle", got)
	}
}

func TestRecordingControllerHandlesShortAudio(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 100))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, nil)

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 0 {
		t.Fatalf("jobs = %d, want 0", len(submitter.jobs))
	}
	if got := observer.states; len(got) < 2 || got[1] != "idle:" {
		t.Fatalf("states = %#v", got)
	}
}
