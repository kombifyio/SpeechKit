package pipeline

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

func TestRecordingControllerStartStopSubmitsSegments(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: []byte(strings.Repeat("a", 6400)), paragraph: false},
			{pcm: []byte(strings.Repeat("b", 6400)), paragraph: true},
		}}
	})
	controller.SetFragmentSegments(true) // asserts the opt-in parallel-segment path

	if err := controller.Start(speechkit.RecordingStartOptions{
		Label:       "Recording started",
		Target:      "target-1",
		Language:    "en",
		QuickNote:   true,
		QuickNoteID: 7,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
	job := submitter.jobs[0]
	if len(job.Segments) != 2 {
		t.Fatalf("job segments = %d, want 2", len(job.Segments))
	}
	if got, want := job.Language, "en"; got != want {
		t.Fatalf("job.Language = %q, want %q", got, want)
	}
	if got, want := job.Segments[1].Prefix, "\n\n"; got != want {
		t.Fatalf("job.Segments[1].Prefix = %q, want %q", got, want)
	}
	if got, want := job.QuickNoteID, int64(7); got != want {
		t.Fatalf("job quick note id = %d, want %d", got, want)
	}
}

func TestRecordingControllerPlacesCaptureOnSharedTimeline(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	now := time.Date(2026, 8, 19, 10, 0, 30, 0, time.UTC)
	controller.now = func() time.Time { return now }

	// A host recording one session from several sources passes the same epoch
	// to every controller; this one joined the session 30 seconds in.
	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		CaptureChannel: speechkit.CaptureChannelSystem,
		CaptureEpoch:   now.Add(-30 * time.Second),
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
	submission := submitter.jobs[0].Submission
	if submission.CaptureChannel != speechkit.CaptureChannelSystem {
		t.Fatalf("CaptureChannel = %q, want %q", submission.CaptureChannel, speechkit.CaptureChannelSystem)
	}
	if submission.CapturedStartMs != 30000 {
		t.Fatalf("CapturedStartMs = %d, want 30000", submission.CapturedStartMs)
	}
	if submission.CapturedEndMs != 30200 {
		t.Fatalf("CapturedEndMs = %d, want 30200 (0.2s of audio)", submission.CapturedEndMs)
	}
}

func TestRecordingControllerWithoutEpochStartsTimelineAtZero(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want 1", len(submitter.jobs))
	}
	if got := submitter.jobs[0].Submission.CapturedStartMs; got != 0 {
		t.Fatalf("CapturedStartMs = %d, want 0 for a session timed from its own start", got)
	}
}

func TestRecordingControllerOpensMicrophoneBeforeProviderStreamDial(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("z", 6400))}
	provider := &orderedDictationStreamProvider{recorder: recorder}
	controller := NewRecordingController(recorder, &fakeSubmitter{}, &fakeObserver{}, nil)
	controller.SetDictationStream(provider, &fakeDictationStreamSink{})

	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		ProviderStream: true,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !provider.sawRecorderStarted {
		t.Fatal("provider stream dialed before the microphone opened")
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerSignalsRecordingAfterRecorderStarts(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observedRecordingAfterStart := false
	observer := &fakeObserver{
		onState: func(status, _ string) {
			if status == "recording" {
				observedRecordingAfterStart = recorder.started
			}
		},
	}
	controller := NewRecordingController(recorder, submitter, observer, nil)

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if !observedRecordingAfterStart {
		t.Fatalf("recording state was signaled before recorder.Start completed")
	}
}

func TestRecordingControllerPrefersPooledPCMHandler(t *testing.T) {
	recorder := &fakePooledRecorder{fakeRecorder: fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeCollector{}

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if recorder.pooledHandler == nil {
		t.Fatal("pooled PCM handler not installed on pooled recorder")
	}
	if recorder.pcmHandler != nil {
		t.Fatal("legacy PCM handler must stay nil when the pooled path is active")
	}

	// A frame delivered through the pooled path reaches the collector
	// and is released exactly once after the handler returns.
	var releases int
	recorder.pooledHandler([]byte{1, 2, 3, 4}, func() { releases++ })
	if releases != 1 {
		t.Fatalf("release calls = %d, want 1", releases)
	}
	if len(collector.fedPCM) != 1 || len(collector.fedPCM[0]) != 4 {
		t.Fatalf("collector fedPCM = %#v, want one 4-byte frame", collector.fedPCM)
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if recorder.pooledHandler != nil {
		t.Fatal("pooled PCM handler not cleared on Stop")
	}
}

func TestRecordingControllerLegacyRecorderStillUsesPCMHandler(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeCollector{}

	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})
	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("legacy PCM handler not installed on plain recorder")
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerStartErrorResetsState(t *testing.T) {
	recorder := &fakeRecorder{startErr: errors.New("boom")}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, &fakeSubmitter{}, observer, nil)

	err := controller.Start(speechkit.RecordingStartOptions{Language: "en"})
	if err == nil {
		t.Fatal("Start() error = nil, want error")
	}
	if controller.IsRecording() {
		t.Fatal("controller.IsRecording() = true, want false")
	}
	if got := observer.states; len(got) != 1 || got[0] != "idle:" {
		t.Fatalf("states = %#v", got)
	}
}
