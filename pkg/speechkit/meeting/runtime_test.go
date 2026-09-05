package meeting

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio/capture"
)

type fakePipeline struct {
	channel  string
	events   chan capture.Event
	startErr error
	stopErr  error

	mu      sync.Mutex
	starts  []speechkit.RecordingStartOptions
	stops   int
	closed  bool
	running bool
}

func newFakePipeline(channel string) *fakePipeline {
	return &fakePipeline{channel: channel, events: make(chan capture.Event, 4)}
}

func (p *fakePipeline) Channel() string { return p.channel }

func (p *fakePipeline) Start(opts speechkit.RecordingStartOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.startErr != nil {
		return p.startErr
	}
	p.starts = append(p.starts, opts)
	p.running = true
	return nil
}

func (p *fakePipeline) Stop(speechkit.RecordingStopOptions) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stops++
	p.running = false
	return p.stopErr
}

func (p *fakePipeline) Events() <-chan capture.Event { return p.events }

func (p *fakePipeline) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

func (p *fakePipeline) startOptions() []speechkit.RecordingStartOptions {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]speechkit.RecordingStartOptions(nil), p.starts...)
}

func (p *fakePipeline) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// newTestRuntime wires a runtime over the supplied pipelines and hands back a
// lookup so tests can assert against each channel.
func newTestRuntime(t *testing.T, pipelines ...*fakePipeline) (*Runtime, map[string]*fakePipeline) {
	t.Helper()
	byChannel := map[string]*fakePipeline{}
	for _, pipeline := range pipelines {
		byChannel[pipeline.channel] = pipeline
	}
	runtime := New(Options{
		NewPipeline: func(channel string) (Pipeline, error) {
			pipeline, ok := byChannel[channel]
			if !ok {
				return nil, errors.New("channel not available on this device")
			}
			return pipeline, nil
		},
		DrainTimeout: 500 * time.Millisecond,
		DrainPoll:    5 * time.Millisecond,
	})
	return runtime, byChannel
}

func startTestMeeting(t *testing.T, runtime *Runtime) Snapshot {
	t.Helper()
	snapshot, err := runtime.Start(context.Background(), StartOptions{SessionID: 42, Title: "Planning"})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return snapshot
}

func TestRuntimeRecordsEveryChannelOnOneSharedTimeline(t *testing.T) {
	runtime, pipelines := newTestRuntime(t, newFakePipeline(ChannelMicrophone), newFakePipeline(ChannelSystem))

	snapshot := startTestMeeting(t, runtime)
	if snapshot.State != StateLive {
		t.Fatalf("state = %q, want live", snapshot.State)
	}

	micStart := pipelines[ChannelMicrophone].startOptions()
	systemStart := pipelines[ChannelSystem].startOptions()
	if len(micStart) != 1 || len(systemStart) != 1 {
		t.Fatalf("each channel should have been started once, got %d and %d", len(micStart), len(systemStart))
	}
	if micStart[0].CaptureEpoch != systemStart[0].CaptureEpoch || micStart[0].CaptureEpoch.IsZero() {
		t.Fatalf("channels must share a non-zero epoch: %v vs %v", micStart[0].CaptureEpoch, systemStart[0].CaptureEpoch)
	}
	if micStart[0].CaptureChannel != ChannelMicrophone || systemStart[0].CaptureChannel != ChannelSystem {
		t.Fatalf("channels mislabeled: %q, %q", micStart[0].CaptureChannel, systemStart[0].CaptureChannel)
	}
	if micStart[0].RecordingSessionID != 42 {
		t.Fatalf("RecordingSessionID = %d, want 42", micStart[0].RecordingSessionID)
	}
}

func TestRuntimeResumeKeepsTheOriginalTimeline(t *testing.T) {
	runtime, pipelines := newTestRuntime(t, newFakePipeline(ChannelMicrophone), newFakePipeline(ChannelSystem))
	startTestMeeting(t, runtime)

	if _, err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if _, err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	starts := pipelines[ChannelMicrophone].startOptions()
	if len(starts) != 2 {
		t.Fatalf("microphone starts = %d, want 2 (start and resume)", len(starts))
	}
	if starts[0].CaptureEpoch != starts[1].CaptureEpoch {
		t.Fatal("resuming started a new timeline instead of continuing the meeting")
	}
}

func TestRuntimeKeepsRecordingWhenOneChannelDies(t *testing.T) {
	runtime, pipelines := newTestRuntime(t, newFakePipeline(ChannelMicrophone), newFakePipeline(ChannelSystem))
	startTestMeeting(t, runtime)

	pipelines[ChannelSystem].events <- capture.Event{Type: capture.EventError, Message: "output device removed"}

	deadline := time.Now().Add(2 * time.Second)
	for {
		snapshot := runtime.Snapshot()
		if channelState(snapshot, ChannelSystem) == ChannelStateFailed {
			if snapshot.State != StateLive {
				t.Fatalf("meeting state = %q, want the meeting to stay live on the surviving channel", snapshot.State)
			}
			if got := channelState(snapshot, ChannelMicrophone); got != ChannelStateRecording {
				t.Fatalf("microphone state = %q, want it to keep recording", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("system channel never reported the device error: %+v", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRuntimeStartsWithoutAnUnavailableChannel(t *testing.T) {
	// Only the microphone opens: a machine with no render device still records
	// the local speaker rather than refusing the meeting.
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))

	snapshot := startTestMeeting(t, runtime)

	if snapshot.State != StateLive {
		t.Fatalf("state = %q, want live", snapshot.State)
	}
	if got := channelState(snapshot, ChannelSystem); got != ChannelStateFailed {
		t.Fatalf("system channel state = %q, want it marked unavailable", got)
	}
}

func TestRuntimeRefusesToStartWhenNoChannelOpens(t *testing.T) {
	runtime, _ := newTestRuntime(t)

	if _, err := runtime.Start(context.Background(), StartOptions{SessionID: 7}); !errors.Is(err, ErrNoChannels) {
		t.Fatalf("Start() error = %v, want ErrNoChannels", err)
	}
	if runtime.ActiveSessionID() != 0 {
		t.Fatal("a meeting that never opened a channel must not stay active")
	}
}

func TestRuntimeRefusesASecondConcurrentMeeting(t *testing.T) {
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))
	startTestMeeting(t, runtime)

	if _, err := runtime.Start(context.Background(), StartOptions{SessionID: 43}); !errors.Is(err, ErrMeetingActive) {
		t.Fatalf("Start() error = %v, want ErrMeetingActive", err)
	}
}

func TestRuntimeStopWaitsForInFlightTranscription(t *testing.T) {
	runtime, pipelines := newTestRuntime(t, newFakePipeline(ChannelMicrophone))
	startTestMeeting(t, runtime)

	runtime.NoteSegmentSubmitted(42)
	committed := make(chan struct{})
	go func() {
		time.Sleep(50 * time.Millisecond)
		runtime.NoteSegmentCommitted(42)
		close(committed)
	}()

	snapshot, err := runtime.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	select {
	case <-committed:
	default:
		t.Fatal("Stop() finished before the in-flight segment was committed")
	}
	if snapshot.State != StateEnded {
		t.Fatalf("state = %q, want ended", snapshot.State)
	}
	if !pipelines[ChannelMicrophone].isClosed() {
		t.Fatal("Stop() left the capture device open")
	}
	if runtime.ActiveSessionID() != 0 {
		t.Fatal("a finished meeting must release the runtime")
	}
}

func TestRuntimeStopGivesUpOnTranscriptionThatNeverLands(t *testing.T) {
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))
	startTestMeeting(t, runtime)
	runtime.NoteSegmentSubmitted(42)

	var stopped Snapshot
	done := make(chan struct{})
	go func() {
		defer close(done)
		var err error
		stopped, err = runtime.Stop(context.Background())
		if err != nil {
			t.Errorf("Stop() error = %v", err)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() hung on a segment that never committed")
	}
	if !stopped.Degraded || stopped.PendingSegments != 1 {
		t.Fatalf("unresolved accepted segment was not surfaced as degraded capture: %+v", stopped)
	}
}

func TestRuntimeCommandsRequireAMatchingState(t *testing.T) {
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))

	if _, err := runtime.Pause(); !errors.Is(err, ErrNoMeeting) {
		t.Fatalf("Pause() without a meeting = %v, want ErrNoMeeting", err)
	}

	startTestMeeting(t, runtime)
	if _, err := runtime.Resume(); !errors.Is(err, ErrNoMeeting) {
		t.Fatalf("Resume() while live = %v, want a state complaint", err)
	}
}

func channelState(snapshot Snapshot, channel string) ChannelState {
	for _, entry := range snapshot.Channels {
		if entry.Channel == channel {
			return entry.State
		}
	}
	return ""
}

// A meeting can be stopped from the dashboard, from the note window's Finish
// button, or from anywhere else the host wires up. Recording that it ended —
// and writing it up — has to happen whichever was used: in the first live test
// the note window stopped the runtime directly, so the meeting stayed
// "recording" in the library and was never written up.
func TestRuntimeReportsTheEndingWhicheverWayItWasStopped(t *testing.T) {
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))
	var ended []int64
	runtime.SetEndedHook(func(sessionID int64) { ended = append(ended, sessionID) })

	startTestMeeting(t, runtime)
	if len(ended) != 0 {
		t.Fatal("a running meeting was reported as ended")
	}

	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(ended) != 1 || ended[0] != 42 {
		t.Fatalf("ended hook fired %v, want exactly session 42 once", ended)
	}
}

func TestRuntimeDoesNotReportAnEndingForAMeetingThatNeverStarted(t *testing.T) {
	runtime, _ := newTestRuntime(t)
	runtime.SetEndedHook(func(int64) { t.Fatal("reported an ending for a meeting that never opened a channel") })

	if _, err := runtime.Start(context.Background(), StartOptions{SessionID: 7}); !errors.Is(err, ErrNoChannels) {
		t.Fatalf("Start() error = %v, want ErrNoChannels", err)
	}
}

// Stop can race in from two surfaces at once (dashboard and note window).
// Exactly one may run the finalization; the loser must be told the meeting is
// already ending instead of stopping the pipelines a second time and firing
// the ended hook twice.
func TestRuntimeConcurrentStopsFinalizeExactlyOnce(t *testing.T) {
	runtime, _ := newTestRuntime(t, newFakePipeline(ChannelMicrophone))
	var mu sync.Mutex
	var ended []int64
	runtime.SetEndedHook(func(sessionID int64) {
		mu.Lock()
		ended = append(ended, sessionID)
		mu.Unlock()
	})
	startTestMeeting(t, runtime)

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := runtime.Stop(context.Background())
			results <- err
		}()
	}
	succeeded, rejected := 0, 0
	for range 2 {
		switch err := <-results; {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrNoMeeting):
			rejected++
		default:
			t.Fatalf("Stop() error = %v, want nil or ErrNoMeeting", err)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("stops: %d succeeded, %d rejected; want exactly one of each", succeeded, rejected)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ended) != 1 || ended[0] != 42 {
		t.Fatalf("ended hook fired %v, want exactly session 42 once", ended)
	}
}

// ElapsedMs is the clock a screen capture is stamped with. It must run on the
// same time base as transcript segments — wall clock since the capture epoch —
// and it must keep running through a pause, because the segment clock does too.
func TestRuntimeElapsedMsMatchesTheTranscriptTimeline(t *testing.T) {
	pipeline := newFakePipeline(ChannelMicrophone)
	clock := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	runtime := New(Options{
		NewPipeline: func(channel string) (Pipeline, error) {
			if channel != ChannelMicrophone {
				return nil, errors.New("channel not available on this device")
			}
			return pipeline, nil
		},
		Now:          func() time.Time { return clock },
		DrainTimeout: 500 * time.Millisecond,
		DrainPoll:    5 * time.Millisecond,
	})

	if _, _, ok := runtime.ElapsedMs(); ok {
		t.Fatal("ElapsedMs reported a timeline before any meeting started")
	}

	startTestMeeting(t, runtime)
	clock = clock.Add(90 * time.Second)
	sessionID, elapsed, ok := runtime.ElapsedMs()
	if !ok || sessionID != 42 {
		t.Fatalf("ElapsedMs() = (%d, %d, %v), want session 42", sessionID, elapsed, ok)
	}
	if elapsed != 90_000 {
		t.Fatalf("elapsed = %d, want 90000", elapsed)
	}

	// A pause must not stop this clock: segments after resume are stamped with
	// wall-clock offsets from the original epoch.
	if _, err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	clock = clock.Add(30 * time.Second)
	if _, elapsed, _ = runtime.ElapsedMs(); elapsed != 120_000 {
		t.Fatalf("elapsed during pause = %d, want 120000", elapsed)
	}

	if _, err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, _, ok := runtime.ElapsedMs(); ok {
		t.Fatal("ElapsedMs reported a timeline after the meeting ended")
	}
}
