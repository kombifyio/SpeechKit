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
	states []string
	logs   []string
}

func (o *fakeObserver) OnState(status, text string) {
	o.states = append(o.states, status+":"+text)
}

func (o *fakeObserver) OnLog(message, kind string) {
	o.logs = append(o.logs, kind+":"+message)
}

type fakeCollector struct {
	feedErr  error
	segments []dictationSegment
}

type dictationSegment struct {
	pcm       []byte
	paragraph bool
}

func (c *fakeCollector) FeedPCM(_ []byte) error {
	return c.feedErr
}

func (c *fakeCollector) CollectStopSegments(_ []byte) ([]AudioSegment, error) {
	segments := make([]AudioSegment, 0, len(c.segments))
	for _, segment := range c.segments {
		segments = append(segments, AudioSegment{PCM: segment.pcm, Paragraph: segment.paragraph})
	}
	return segments, nil
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

	if len(submitter.jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(submitter.jobs))
	}
	if got, want := submitter.jobs[0].Language, "en"; got != want {
		t.Fatalf("job[0].Language = %q, want %q", got, want)
	}
	if got, want := submitter.jobs[1].Prefix, "\n\n"; got != want {
		t.Fatalf("job[1].Prefix = %q, want %q", got, want)
	}
	if got, want := submitter.jobs[1].QuickNoteID, int64(7); got != want {
		t.Fatalf("job[1].QuickNoteID = %d, want %d", got, want)
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
	if got := observer.states; len(got) < 2 || got[1] != "idle:" {
		t.Fatalf("states = %#v", got)
	}
}
