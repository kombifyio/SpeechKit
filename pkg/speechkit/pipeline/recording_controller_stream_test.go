package pipeline

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
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
	observer := &fakeObserver{}
	collector := &fakeCollector{readySegments: []dictationSegment{{pcm: []byte(strings.Repeat("a", 6400))}}}
	controller := NewRecordingController(recorder, submitter, observer, func() speechkit.SegmentCollector {
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
	// The drained finals already carry the session's terminal state; a
	// trailing "processing" has no job behind it and strands the overlay.
	if got := observer.states; got[len(got)-1] == "processing:" {
		t.Fatalf("states = %#v, want no processing state after the stream drained", got)
	}
}

// A stream that dies mid-dictation (Deepgram's UtteranceEnd once failed to
// decode) must not take the rest of the dictation with it: Stop transcribes
// the capture after the last committed word, mapped past the audio captured
// while the handshake was still dialing.
func TestRecordingControllerProviderStreamFailureTranscribesUncommittedTail(t *testing.T) {
	const bytesPerMs = 32 // 16 kHz mono PCM16
	dialing := strings.Repeat("d", 200*bytesPerMs)
	committed := strings.Repeat("c", 500*bytesPerMs)
	rest := strings.Repeat("r", 1000*bytesPerMs)
	recorder := &fakeRecorder{stopPCM: []byte(dialing + committed + rest)}
	submitter := &fakeSubmitter{}
	stream := &failingDictationStream{final: speechkit.DictationStreamEvent{
		Text:    "Erster Satz.",
		IsFinal: true,
		Words:   []speechkit.WordConfidence{{Text: "Satz.", StartMs: 100, EndMs: 500}},
	}}
	provider := &dialingDictationStreamProvider{recorder: recorder, dialing: []byte(dialing), stream: stream}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	controller.SetDictationStream(provider, &fakeDictationStreamSink{})

	if err := controller.Start(speechkit.RecordingStartOptions{Language: "de", ProviderStream: true}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	recorder.pcmHandler([]byte(committed))
	recorder.pcmHandler([]byte(rest))
	if err := controller.Stop(speechkit.RecordingStopOptions{Label: "Captured"}); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	if len(submitter.jobs) != 1 {
		t.Fatalf("jobs after stream failure = %d, want one job for the uncommitted audio", len(submitter.jobs))
	}
	if got := string(submitter.jobs[0].PCM); got != rest {
		t.Fatalf("tail job = %d bytes, want exactly the %d uncommitted bytes", len(got), len(rest))
	}
}

// dialingDictationStreamProvider delivers capture frames while its handshake
// is still in flight, like a real websocket dial.
type dialingDictationStreamProvider struct {
	recorder *fakeRecorder
	dialing  []byte
	stream   speechkit.DictationStream
}

func (p *dialingDictationStreamProvider) StartDictationStream(context.Context, speechkit.DictationStreamOptions, speaker.AudioFormat) (speechkit.DictationStream, error) {
	p.recorder.pcmHandler(p.dialing)
	return p.stream, nil
}

// failingDictationStream commits one final, then fails like an undecodable
// provider message.
type failingDictationStream struct {
	final    speechkit.DictationStreamEvent
	received atomic.Int32
}

func (s *failingDictationStream) SendPCM(context.Context, []byte) error { return nil }
func (s *failingDictationStream) Finalize(context.Context) error        { return nil }
func (s *failingDictationStream) Close() error                          { return nil }

func (s *failingDictationStream) Receive(context.Context) (speechkit.DictationStreamEvent, error) {
	if s.received.Add(1) == 1 {
		return s.final, nil
	}
	return speechkit.DictationStreamEvent{}, errors.New("stream parse failure")
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

// Hold-to-talk holds its finals until release; cancelling the capture (a mode
// switch) must drop them instead of inserting text the user abandoned.
func TestRecordingControllerCancelDropsHeldSessionFinals(t *testing.T) {
	recorder := &fakeRecorder{stopPCM: []byte(strings.Repeat("z", 6400))}
	provider := &fakeDictationStreamProvider{
		stream: newFakeDictationStream(speechkit.DictationStreamEvent{Text: "abandoned", IsFinal: true, SegmentID: 1}),
	}
	sink := &fakeDictationStreamSink{}
	controller := NewRecordingController(recorder, &fakeSubmitter{}, &fakeObserver{}, nil)
	controller.SetDictationStream(provider, sink)

	if err := controller.Start(speechkit.RecordingStartOptions{Target: "editor", ProviderStream: true, LiveCommitMode: LiveCommitSession}); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	recorder.pcmHandler([]byte("frame"))
	if err := controller.Cancel(speechkit.RecordingCancelOptions{}); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if finals := sink.finalEvents(); len(finals) != 0 {
		t.Fatalf("finals after cancel = %#v, want none", finals)
	}
}
