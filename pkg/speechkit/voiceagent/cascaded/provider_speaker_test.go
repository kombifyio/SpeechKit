package cascaded

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func TestStreamingSpeakerFramesEmitInputTranscriptMetadata(t *testing.T) {
	sttFake := &fakeSTT{text: "batch final"}
	agent := &fakeAgent{response: "ok"}
	streamer := &fakeSpeakerStreamer{}
	p := NewProvider(Deps{
		STT:             sttFake,
		Agent:           agent,
		SpeakerStreamer: streamer,
		Config:          Config{SilenceTurnMs: 10_000, MinTurnMs: 50},
	})
	if err := p.Connect(context.Background(), SessionConfig{
		Locale: "en",
		Speaker: speaker.Options{
			Diarization:     true,
			PreferStreaming: true,
		},
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	streamer.mu.Lock()
	stream := streamer.stream
	started := streamer.started
	streamer.mu.Unlock()
	if started != 1 || stream == nil {
		t.Fatalf("speaker stream started=%d stream=%v", started, stream)
	}

	chunk := sineChunk(100, 10000)
	if err := p.SendAudio(chunk); err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
	if err := p.SendAudioStreamEnd(); err != nil {
		t.Fatalf("SendAudioStreamEnd: %v", err)
	}
	stream.mu.Lock()
	audioCount := len(stream.audio)
	ended := stream.ended
	stream.mu.Unlock()
	if audioCount != 1 || !ended {
		t.Fatalf("stream audioCount=%d ended=%v", audioCount, ended)
	}

	stream.frames <- &speaker.SpeakerFrame{
		Provider: "fake",
		Text:     "live partial",
		IsFinal:  false,
		Segment: &speaker.SpeakerSegment{
			SpeakerLabel:      "speaker_A",
			PersonID:          "p1",
			DisplayName:       "Alice",
			SpeakerConfidence: 0.77,
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var msg *Message
	for {
		got, err := p.Receive(ctx)
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if got.InputTranscript == "live partial" {
			msg = got
			break
		}
	}
	if msg.InputTranscript != "live partial" || msg.InputSpeakerLabel != "speaker_A" ||
		msg.InputPersonID != "p1" || msg.InputDisplayName != "Alice" || msg.InputSpeakerConfidence != 0.77 ||
		msg.InputTranscriptDone {
		t.Fatalf("message = %+v", msg)
	}
}

func TestSpeakerStreamReconnectsAfterTransientDrop(t *testing.T) {
	origBackoff := speakerReconnectInitialBackoff
	speakerReconnectInitialBackoff = time.Millisecond
	t.Cleanup(func() { speakerReconnectInitialBackoff = origBackoff })

	streamer := &reconnectStreamer{}
	p := NewProvider(Deps{
		STT:             &fakeSTT{text: "x"},
		Agent:           &fakeAgent{response: "ok"},
		SpeakerStreamer: streamer,
		Config:          Config{SilenceTurnMs: 10_000, MinTurnMs: 50},
	})
	if err := p.Connect(context.Background(), SessionConfig{
		Locale:  "en",
		Speaker: speaker.Options{Diarization: true, PreferStreaming: true},
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	s0 := waitForStream(t, streamer, 0)
	s0.frames <- &speaker.SpeakerFrame{Provider: "fake", Text: "before drop",
		Segment: &speaker.SpeakerSegment{SpeakerLabel: "speaker_A"}}
	if got := receiveTranscript(t, p); got != "before drop" {
		t.Fatalf("first transcript = %q, want %q", got, "before drop")
	}

	// Drop the stream with a transient (non-EOF, non-cancel) error. The loop
	// must reconnect instead of dying for the rest of the session.
	s0.errc <- errors.New("websocket: connection reset by peer")
	waitForCount(t, streamer, 2)
	if !s0.isClosed() {
		t.Fatal("dropped stream was not closed before reconnect")
	}

	s1 := waitForStream(t, streamer, 1)
	s1.frames <- &speaker.SpeakerFrame{Provider: "fake", Text: "after reconnect",
		Segment: &speaker.SpeakerSegment{SpeakerLabel: "speaker_B"}}
	if got := receiveTranscript(t, p); got != "after reconnect" {
		t.Fatalf("post-reconnect transcript = %q, want %q", got, "after reconnect")
	}
}
