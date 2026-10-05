package pipeline

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// stopDrainRecorder models a capture backend that drains queued callbacks
// during Stop, after the hardware has finished recording.
type stopDrainRecorder struct {
	*fakeRecorder
	tail []byte
}

func (r *stopDrainRecorder) Stop() ([]byte, error) {
	if r.pcmHandler != nil {
		r.pcmHandler(r.tail)
	}
	return r.fakeRecorder.Stop()
}

type recordingContextKey struct{}

type recordingHistorySink struct {
	*fakeDictationStreamSink
	audio []byte
	scope any
}

func (s *recordingHistorySink) CompleteDictationStreamRecording(ctx context.Context, _ uint64, recording speechkit.Submission, _ speechkit.DictationStreamSinkOptions) {
	s.audio = append([]byte(nil), recording.PCM...)
	s.scope = ctx.Value(recordingContextKey{})
}

// Protect the audio-conservation invariant: the provider must receive the
// full recording in order, including speech during dial and stop drain.
func TestRecordingControllerStreamPreservesCaptureBoundaries(t *testing.T) {
	onset := bytes.Repeat([]byte{1, 2}, 8000)
	middle := bytes.Repeat([]byte{3, 4}, 16000)
	tail := bytes.Repeat([]byte{5, 6}, 8000)
	full := bytes.Join([][]byte{onset, middle, tail}, nil)
	recorder := &stopDrainRecorder{fakeRecorder: &fakeRecorder{stopPCM: full}, tail: tail}
	stream := newFakeDictationStream(speechkit.DictationStreamEvent{Text: "Keep the complete instruction", IsFinal: true})
	provider := &dialingDictationStreamProvider{recorder: recorder.fakeRecorder, dialing: onset, stream: stream}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	sink := &recordingHistorySink{fakeDictationStreamSink: &fakeDictationStreamSink{}}
	controller.SetDictationStream(provider, sink)
	if err := controller.Start(speechkit.RecordingStartOptions{ProviderStream: true, LiveCommitMode: LiveCommitSession, Context: context.WithValue(context.Background(), recordingContextKey{}, "scoped-history")}); err != nil {
		t.Fatal(err)
	}
	recorder.pcmHandler(middle)
	if err := controller.Stop(speechkit.RecordingStopOptions{}); err != nil {
		t.Fatal(err)
	}
	stream.mu.Lock()
	received := bytes.Join(stream.pcm, nil)
	stream.mu.Unlock()
	if !bytes.Equal(received, full) {
		t.Fatalf("provider audio differs from captured audio: sent %d of %d bytes", len(received), len(full))
	}
	if sink.scope != "scoped-history" {
		t.Fatal("recording completion lost host scope")
	}
	if !bytes.Equal(sink.audio, full) {
		t.Fatal("completed live history did not receive the original recording")
	}
	for _, job := range submitter.jobs {
		if len(job.PCM) > 0 {
			t.Fatal("intact provider audio was also submitted for batch transcription")
		}
	}
}

// A backend can retain full audio even when a delayed dispatcher loses
// callbacks. Hold-to-talk must replay that audio once, not paste its partial
// live transcript before the complete replacement.
func TestRecordingControllerStreamRecoversMissingCallbacksWithoutPartialCommit(t *testing.T) {
	prefix := bytes.Repeat([]byte{1, 2}, 8000)
	tail := bytes.Repeat([]byte{3, 4}, 8000)
	full := bytes.Join([][]byte{prefix, tail}, nil)
	recorder := &fakeRecorder{stopPCM: full}
	stream := newFakeDictationStream(speechkit.DictationStreamEvent{Text: "Partial instruction", IsFinal: true})
	sink := &fakeDictationStreamSink{}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	controller.SetDictationStream(&fakeDictationStreamProvider{stream: stream}, sink)
	if err := controller.Start(speechkit.RecordingStartOptions{ProviderStream: true, LiveCommitMode: LiveCommitSession}); err != nil {
		t.Fatal(err)
	}
	recorder.pcmHandler(prefix)
	if err := controller.Stop(speechkit.RecordingStopOptions{}); err != nil {
		t.Fatal(err)
	}
	var recovered []byte
	for _, job := range submitter.jobs {
		recovered = append(recovered, job.PCM...)
	}
	if !bytes.Equal(recovered, full) {
		t.Fatal("callback loss did not recover the complete recording")
	}
	for _, event := range sink.finalEvents() {
		if event.Text != "" {
			t.Fatal("partial held transcript was committed before full recovery")
		}
	}
}

type stalledSendStream struct{ *fakeDictationStream }

func (s *stalledSendStream) SendPCM(ctx context.Context, _ []byte) error {
	<-ctx.Done()
	return ctx.Err()
}

type fixedStreamProvider struct{ stream speechkit.DictationStream }

func (p fixedStreamProvider) StartDictationStream(context.Context, speechkit.DictationStreamOptions, speaker.AudioFormat) (speechkit.DictationStream, error) {
	return p.stream, nil
}

// A stalled network sender must not turn queue pressure into word holes.
func TestRecordingControllerStreamPressureRecoversRecording(t *testing.T) {
	frame := bytes.Repeat([]byte{1, 2}, 512)
	full := bytes.Repeat(frame, 200)
	recorder := &fakeRecorder{stopPCM: full}
	submitter := &fakeSubmitter{}
	stream := &stalledSendStream{newFakeDictationStream(speechkit.DictationStreamEvent{})}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	controller.SetDictationStream(fixedStreamProvider{stream}, &fakeDictationStreamSink{})
	if err := controller.Start(speechkit.RecordingStartOptions{ProviderStream: true}); err != nil {
		t.Fatal(err)
	}
	for offset := 0; offset < len(full); offset += len(frame) {
		recorder.pcmHandler(full[offset : offset+len(frame)])
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{}); err != nil {
		t.Fatal(err)
	}
	var recovered []byte
	for _, job := range submitter.jobs {
		recovered = append(recovered, job.PCM...)
	}
	if !bytes.Equal(recovered, full) {
		t.Fatal("network pressure lost captured audio")
	}
}

// Cancellation while Finalize runs must recover speech after an earlier final.
type cancellingFinalizeStream struct {
	*fakeDictationStream
	cancel   context.CancelFunc
	received bool
	handled  chan struct{}
}

func (s *cancellingFinalizeStream) Finalize(context.Context) error { s.cancel(); return nil }
func (s *cancellingFinalizeStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	if !s.received {
		s.received = true
		return speechkit.DictationStreamEvent{Text: "Keep", IsFinal: true, Words: []speechkit.WordConfidence{{EndMs: 100}}}, nil
	}
	close(s.handled)
	<-ctx.Done()
	return speechkit.DictationStreamEvent{}, ctx.Err()
}
func TestRecordingControllerStreamCancellationDuringFinalizeRecoversTail(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	full := bytes.Repeat([]byte{1, 2}, 8000)
	recorder := &fakeRecorder{stopPCM: full}
	stream := &cancellingFinalizeStream{fakeDictationStream: newFakeDictationStream(speechkit.DictationStreamEvent{}), cancel: cancel, handled: make(chan struct{})}
	submitter := &fakeSubmitter{}
	controller := NewRecordingController(recorder, submitter, &fakeObserver{}, nil)
	controller.SetDictationStream(fixedStreamProvider{stream}, &fakeDictationStreamSink{})
	if err := controller.Start(speechkit.RecordingStartOptions{ProviderStream: true, Context: ctx}); err != nil {
		t.Fatal(err)
	}
	recorder.pcmHandler(full)
	select {
	case <-stream.handled:
	case <-time.After(time.Second):
		t.Fatal("first final was not handled")
	}
	if err := controller.Stop(speechkit.RecordingStopOptions{}); err != nil {
		t.Fatal(err)
	}
	var recovered []byte
	for _, job := range submitter.jobs {
		recovered = append(recovered, job.PCM...)
	}
	if !bytes.Equal(recovered, full[100*pcmBytesPerMs:]) {
		t.Fatal("cancellation lost trailing speech")
	}
}
