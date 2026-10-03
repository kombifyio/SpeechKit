package pipeline

import (
	"bytes"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type recordingSubmitFunc func(speechkit.TranscriptionJob) error

func (f recordingSubmitFunc) Submit(job speechkit.TranscriptionJob) error { return f(job) }

func TestRecordingControllerRestartRetainsPendingAudio(t *testing.T) {
	oldPCM := bytes.Repeat([]byte{'a'}, 6400)
	newPCM := bytes.Repeat([]byte{'b'}, 6400)
	oldEntered, oldRelease, oldDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	accepted := make(chan []byte, 1)
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(oldRelease) }) })
	var rejected atomic.Bool
	submitter := recordingSubmitFunc(func(job speechkit.TranscriptionJob) error {
		if bytes.Equal(job.PCM, oldPCM) {
			close(oldEntered)
			<-oldRelease
			return nil
		}
		if rejected.CompareAndSwap(false, true) {
			return ErrWorkerQueueFull
		}
		accepted <- append([]byte(nil), job.PCM...)
		return nil
	})
	recorder := &fakeRecorder{}
	collector := &fakeCollector{readySegments: []dictationSegment{{pcm: oldPCM}}}
	controller := NewRecordingController(recorder, submitter, nil, func() speechkit.SegmentCollector { return collector })
	start := func() {
		t.Helper()
		if err := controller.Start(speechkit.RecordingStartOptions{StreamSegments: true}); err != nil {
			t.Fatal(err)
		}
	}
	start()
	oldHandler := recorder.pcmHandler
	go func() { defer close(oldDone); oldHandler([]byte("frame")) }()
	select {
	case <-oldEntered:
	case <-time.After(time.Second):
		t.Fatal("old recording did not reach the submitter")
	}
	if err := controller.Cancel(speechkit.RecordingCancelOptions{}); err != nil {
		t.Fatal(err)
	}
	collector = &fakeCollector{readySegments: []dictationSegment{{pcm: newPCM}}}
	start()
	recorder.pcmHandler([]byte("frame")) // The new segment remains pending while the worker is full.
	release.Do(func() { close(oldRelease) })
	select {
	case <-oldDone:
	case <-time.After(time.Second):
		t.Fatal("old recording did not finish submitting")
	}
	recorder.pcmHandler([]byte("frame"))
	select {
	case pcm := <-accepted:
		if !bytes.Equal(pcm, newPCM) {
			t.Fatal("retry submitted audio from another recording")
		}
	case <-time.After(time.Second):
		t.Fatal("restarting discarded the new recording's pending audio")
	}
}

func TestRecordingControllerStreamsReadySegmentsBeforeStop(t *testing.T) {
	full := []byte(strings.Repeat("z", 6400))
	readyPCM := []byte(strings.Repeat("a", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	collector := &fakeCollector{readySegments: []dictationSegment{
		{pcm: readyPCM, paragraph: true},
	}}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return collector
	})

	if err := controller.Start(speechkit.RecordingStartOptions{
		Language:       "de",
		QuickNote:      true,
		QuickNoteID:    99,
		Target:         "editor",
		StreamSegments: true,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs before Stop = %d, want 1 streamed segment", len(submitter.jobs))
	}
	job := submitter.jobs[0]
	if got, want := string(job.PCM), string(readyPCM); got != want {
		t.Fatalf("streamed job PCM = %q, want ready segment", got)
	}
	if len(job.Segments) != 0 {
		t.Fatalf("streamed job nested segments = %d, want 0", len(job.Segments))
	}
	if got, want := job.Prefix, "\n\n"; got != want {
		t.Fatalf("streamed job prefix = %q, want paragraph prefix", got)
	}
	if got, want := job.Language, "de"; got != want {
		t.Fatalf("streamed job language = %q, want %q", got, want)
	}
	if got, want := job.QuickNoteID, int64(99); got != want {
		t.Fatalf("streamed job quick note id = %d, want %d", got, want)
	}
	if job.SessionID == 0 {
		t.Fatal("streamed job session id = 0, want non-zero")
	}
	if got, want := job.SegmentID, uint64(1); got != want {
		t.Fatalf("streamed job segment id = %d, want %d", got, want)
	}
	if !job.SegmentFinal {
		t.Fatal("streamed job SegmentFinal = false, want true")
	}
	if got, want := job.Target, any("editor"); got != want {
		t.Fatalf("streamed job target = %v, want %v", got, want)
	}

	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs after Stop = %d, want no full-capture duplicate", len(submitter.jobs))
	}
}

func TestRecordingControllerDoesNotStreamReadySegmentsWithoutStartOption(t *testing.T) {
	full := []byte(strings.Repeat("z", 6400))
	readyPCM := []byte(strings.Repeat("a", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	collector := &fakeCollector{readySegments: []dictationSegment{
		{pcm: readyPCM},
	}}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return collector
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("jobs before Stop = %d, want no streamed segment", got)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if got := len(submitter.jobs); got != 1 {
		t.Fatalf("jobs after Stop = %d, want full-capture job", got)
	}
}

func TestRecordingControllerRetainsReadySegmentWhenQueueFull(t *testing.T) {
	readyPCM := []byte(strings.Repeat("a", 6400))
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("z", 6400))}
	submitter := &fakeSubmitter{err: ErrWorkerQueueFull}
	observer := &fakeObserver{}
	collector := &fakeCollector{readySegments: []dictationSegment{
		{pcm: readyPCM},
	}}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
		return collector
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de", StreamSegments: true}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))
	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("jobs after full queue = %d, want 0", got)
	}
	if !observer.hasLog("segment retained for retry") {
		t.Fatalf("observer logs = %v, want retained segment warning", observer.logs)
	}

	submitter.err = nil
	recorder.pcmHandler([]byte("retry"))
	if got := len(submitter.jobs); got != 1 {
		t.Fatalf("jobs after retry = %d, want retained segment submitted", got)
	}
	if got, want := string(submitter.jobs[0].PCM), string(readyPCM); got != want {
		t.Fatalf("submitted PCM = %q, want retained ready segment", got)
	}

	if err := controller.Cancel(speechkit.RecordingCancelOptions{Label: "discard"}); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
}

func TestRecordingControllerStreamSegmentsFlushesStopTail(t *testing.T) {
	full := []byte(strings.Repeat("x", 12800))
	tailPCM := []byte(strings.Repeat("b", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() speechkit.SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: tailPCM, paragraph: false},
		}}
	})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "en", StreamSegments: true}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs = %d, want final tail job", len(submitter.jobs))
	}
	job := submitter.jobs[0]
	if got, want := string(job.PCM), string(tailPCM); got != want {
		t.Fatalf("tail job PCM = %q, want stop tail", got)
	}
	if len(job.Segments) != 0 {
		t.Fatalf("tail job nested segments = %d, want 0", len(job.Segments))
	}
	if got, want := job.DurationSecs, speechkit.PCMDurationSecs(tailPCM); got != want {
		t.Fatalf("tail duration = %v, want %v", got, want)
	}
	if job.SessionID == 0 || job.SegmentID != 1 || !job.SegmentFinal {
		t.Fatalf("tail segment metadata = session:%d segment:%d final:%v, want non-zero/1/final", job.SessionID, job.SegmentID, job.SegmentFinal)
	}
}
