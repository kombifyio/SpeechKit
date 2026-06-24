package speechkit

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeIdleCollector lets a test pin an idle-since timestamp so the
// RecordingController's silence watcher fires deterministically without
// requiring real audio capture.
type fakeIdleCollector struct {
	fakeCollector
	mu        sync.Mutex
	idleSince time.Time
}

func (c *fakeIdleCollector) IdleSince() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.idleSince
}

func (c *fakeIdleCollector) setIdleSince(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.idleSince = t
}

type fakeRecorder struct {
	startErr   error
	stopErr    error
	stopPCM    []byte
	started    bool
	pcmHandler func([]byte)
}

func (r *fakeRecorder) Start() error {
	if r.startErr != nil {
		return r.startErr
	}
	r.started = true
	return nil
}

func (r *fakeRecorder) Stop() ([]byte, error) {
	r.started = false
	return append([]byte(nil), r.stopPCM...), r.stopErr
}

func (r *fakeRecorder) SetPCMHandler(handler func([]byte)) {
	r.pcmHandler = handler
}

type fakeSubmitter struct {
	jobs []TranscriptionJob
	err  error
}

func (s *fakeSubmitter) Submit(job TranscriptionJob) error {
	if s.err != nil {
		return s.err
	}
	s.jobs = append(s.jobs, job.Clone())
	return nil
}

type fakeObserver struct {
	states  []string
	logs    []string
	onState func(status, text string)
}

func (o *fakeObserver) OnState(status, text string) {
	if o.onState != nil {
		o.onState(status, text)
	}
	o.states = append(o.states, status+":"+text)
}

func (o *fakeObserver) OnLog(message, kind string) {
	o.logs = append(o.logs, kind+":"+message)
}

func (o *fakeObserver) hasLog(message string) bool {
	for _, log := range o.logs {
		if strings.Contains(log, message) {
			return true
		}
	}
	return false
}

type fakeCollector struct {
	feedErr       error
	segments      []dictationSegment
	readySegments []dictationSegment
	fedPCM        [][]byte
}

type dictationSegment struct {
	pcm       []byte
	paragraph bool
}

func (c *fakeCollector) FeedPCM(pcm []byte) error {
	c.fedPCM = append(c.fedPCM, append([]byte(nil), pcm...))
	return c.feedErr
}

func (c *fakeCollector) CollectStopSegments(_ []byte) ([]AudioSegment, error) {
	segments := make([]AudioSegment, 0, len(c.segments))
	for _, segment := range c.segments {
		segments = append(segments, AudioSegment{PCM: segment.pcm, Paragraph: segment.paragraph})
	}
	return segments, nil
}

func (c *fakeCollector) DrainReadySegments() []AudioSegment {
	segments := make([]AudioSegment, 0, len(c.readySegments))
	for _, segment := range c.readySegments {
		segments = append(segments, AudioSegment{PCM: segment.pcm, Paragraph: segment.paragraph})
	}
	c.readySegments = nil
	return segments
}

func makePCMForDuration(d time.Duration) []byte {
	if d <= 0 {
		return nil
	}
	const bytesPerSecond = 16000 * 2
	return make([]byte, int(d.Seconds()*bytesPerSecond))
}

func TestRecordingControllerStartStopSubmitsSegments(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: []byte(strings.Repeat("a", 6400)), paragraph: false},
			{pcm: []byte(strings.Repeat("b", 6400)), paragraph: true},
		}}
	})
	controller.SetFragmentSegments(true) // asserts the opt-in parallel-segment path

	if err := controller.Start(RecordingStartOptions{
		Label:       "Recording started",
		Target:      "target-1",
		Language:    "en",
		QuickNote:   true,
		QuickNoteID: 7,
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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

func TestRecordingControllerStopWithNoSegmentsResetsIdle(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return &fakeCollector{}
	})
	controller.SetFragmentSegments(true) // the no-speech skip only applies to the opt-in segment path

	if err := controller.Start(RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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
	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: []byte(strings.Repeat("a", 6400)), paragraph: false},
		}}
	})

	if err := controller.Start(RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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

func TestRecordingControllerStreamsReadySegmentsBeforeStop(t *testing.T) {
	full := []byte(strings.Repeat("z", 6400))
	readyPCM := []byte(strings.Repeat("a", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	collector := &fakeCollector{readySegments: []dictationSegment{
		{pcm: readyPCM, paragraph: true},
	}}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() SegmentCollector {
		return collector
	})

	if err := controller.Start(RecordingStartOptions{
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
	if got, want := job.Target, any("editor"); got != want {
		t.Fatalf("streamed job target = %v, want %v", got, want)
	}

	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() SegmentCollector {
		return collector
	})

	if err := controller.Start(RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if recorder.pcmHandler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}
	recorder.pcmHandler([]byte("frame"))

	if got := len(submitter.jobs); got != 0 {
		t.Fatalf("jobs before Stop = %d, want no streamed segment", got)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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
	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return collector
	})

	if err := controller.Start(RecordingStartOptions{Language: "de", StreamSegments: true}); err != nil {
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

	if err := controller.Cancel(RecordingCancelOptions{Label: "discard"}); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
}

func TestRecordingControllerStreamSegmentsFlushesStopTail(t *testing.T) {
	full := []byte(strings.Repeat("x", 12800))
	tailPCM := []byte(strings.Repeat("b", 6400))
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() SegmentCollector {
		return &fakeCollector{segments: []dictationSegment{
			{pcm: tailPCM, paragraph: false},
		}}
	})

	if err := controller.Start(RecordingStartOptions{Language: "en", StreamSegments: true}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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
	if got, want := job.DurationSecs, PCMDurationSecs(tailPCM); got != want {
		t.Fatalf("tail duration = %v, want %v", got, want)
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

	if err := controller.Start(RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if !observedRecordingAfterStart {
		t.Fatalf("recording state was signaled before recorder.Start completed")
	}
}

func TestRecordingControllerStopTailDelayKeepsCaptureOpen(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	collector := &fakeCollector{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, func() SegmentCollector {
		return collector
	})

	if err := controller.Start(RecordingStartOptions{Language: "de"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	handler := recorder.pcmHandler
	if handler == nil {
		t.Fatal("recorder PCM handler was not installed")
	}

	done := make(chan error, 1)
	go func() {
		done <- controller.Stop(RecordingStopOptions{
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
	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return &fakeCollector{}
	})

	if err := controller.Start(RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Cancel(RecordingCancelOptions{Label: "Discarded for mode switch"}); err != nil {
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

	if err := controller.Start(RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	now = startedAt.Add(6 * time.Second)
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
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

	if err := controller.Start(RecordingStartOptions{Language: "en"}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 0 {
		t.Fatalf("jobs = %d, want 0", len(submitter.jobs))
	}
	if got := observer.states; len(got) < 2 || got[1] != "idle:" {
		t.Fatalf("states = %#v", got)
	}
}

func TestRecordingControllerIdleWatcherFiresOnSilence(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	// Pin idleSince well into the past so the watcher's first tick
	// already sees the timeout exceeded.
	collector.setIdleSince(time.Now().Add(-5 * time.Second))

	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 100 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fired.Load() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("idle callback fired %d times, want 1", fired.Load())
	}

	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerIdleWatcherSkipsWhileSpeechActive(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	// Zero IdleSince === "currently speaking" — watcher must not fire.
	collector.setIdleSince(time.Time{})

	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 50 * time.Millisecond,
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Give the watcher a generous window to incorrectly fire.
	time.Sleep(200 * time.Millisecond)

	if fired.Load() != 0 {
		t.Fatalf("idle callback fired while speech active (fired=%d)", fired.Load())
	}

	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestRecordingControllerStopClearsIdleWatcher(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("a", 6400))}
	submitter := &fakeSubmitter{}
	observer := &fakeObserver{}
	collector := &fakeIdleCollector{}
	collector.setIdleSince(time.Now().Add(-5 * time.Second))

	controller := NewRecordingController(recorder, submitter, observer, func() SegmentCollector {
		return collector
	})
	controller.SetIdleWatchInterval(5 * time.Millisecond)

	var fired atomic.Int32
	if err := controller.Start(RecordingStartOptions{
		Language:    "en",
		IdleTimeout: 5 * time.Second, // long enough that Stop wins the race
		OnIdleTimeoutCallback: func() {
			fired.Add(1)
		},
	}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Stop immediately — the watcher should not get a chance to fire.
	if err := controller.Stop(RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Wait a few watcher ticks past the stop to make sure a stale fire
	// did not leak through.
	time.Sleep(100 * time.Millisecond)
	if fired.Load() != 0 {
		t.Fatalf("idle callback fired after Stop (fired=%d)", fired.Load())
	}
}

func TestRecordingControllerStartErrorResetsState(t *testing.T) {
	recorder := &fakeRecorder{startErr: errors.New("boom")}
	observer := &fakeObserver{}
	controller := NewRecordingController(recorder, &fakeSubmitter{}, observer, nil)

	err := controller.Start(RecordingStartOptions{Language: "en"})
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
