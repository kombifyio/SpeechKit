package geminitranscribe

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// fakeLiveSession replays scripted server messages once the client ended
// the audio stream, like the Live API flushing the trailing transcript.
type fakeLiveSession struct {
	mu        sync.Mutex
	sent      []genai.LiveRealtimeInput
	ended     chan struct{}
	endOnce   sync.Once
	script    []*genai.LiveServerMessage
	scriptPos int
}

func (f *fakeLiveSession) SendRealtimeInput(input genai.LiveRealtimeInput) error {
	f.mu.Lock()
	f.sent = append(f.sent, input)
	f.mu.Unlock()
	if input.AudioStreamEnd {
		f.endOnce.Do(func() { close(f.ended) })
	}
	return nil
}

func (f *fakeLiveSession) Receive() (*genai.LiveServerMessage, error) {
	<-f.ended
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.scriptPos >= len(f.script) {
		return nil, errors.New("websocket: close 1000 (normal)")
	}
	msg := f.script[f.scriptPos]
	f.scriptPos++
	return msg, nil
}

func (f *fakeLiveSession) Close() error { return nil }

func transcriptMessage(text string, finished bool) *genai.LiveServerMessage {
	return &genai.LiveServerMessage{ServerContent: &genai.LiveServerContent{
		InputTranscription: &genai.Transcription{Text: text, Finished: finished},
	}}
}

func TestLiveDictationStreamTurnsFragmentsIntoDraftsAndOneFinal(t *testing.T) {
	session := &fakeLiveSession{
		ended: make(chan struct{}),
		script: []*genai.LiveServerMessage{
			{SetupComplete: &genai.LiveServerSetupComplete{}},
			transcriptMessage("Schick das", false),
			transcriptMessage(" an Kombify", false),
			{ServerContent: &genai.LiveServerContent{TurnComplete: true}},
		},
	}
	var dialedModel string
	var dialed *genai.LiveConnectConfig
	p := New(Options{APIKey: "key"})
	p.liveConnect = func(_ context.Context, model string, cfg *genai.LiveConnectConfig) (liveSession, error) {
		dialedModel, dialed = model, cfg
		return session, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := p.StartDictationStream(ctx, speechkit.DictationStreamOptions{
		Language:       "de-DE",
		Keyterms:       []string{"Kombify", " "},
		InterimResults: true,
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	if dialedModel != LiveModel {
		t.Fatalf("dialed %q, want the live transcription model", dialedModel)
	}
	tc := dialed.InputAudioTranscription
	if tc == nil || !slices.Equal(tc.LanguageCodes, []string{"de-DE"}) || !slices.Equal(tc.CustomVocabulary, []string{"Kombify"}) {
		t.Fatalf("transcription config = %+v, want the session language and vocabulary", tc)
	}

	if err := stream.SendPCM(ctx, []byte{1, 2, 3, 4}); err != nil {
		t.Fatalf("SendPCM: %v", err)
	}
	if err := stream.Finalize(ctx); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	var drafts, finals []string
	for {
		event, err := stream.Receive(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
		if event.IsFinal {
			finals = append(finals, event.Text)
		} else {
			drafts = append(drafts, event.Text)
		}
	}
	if !slices.Equal(finals, []string{"Schick das an Kombify"}) {
		t.Fatalf("finals = %q, want the whole turn once", finals)
	}
	if len(drafts) == 0 || drafts[len(drafts)-1] != "Schick das an Kombify" {
		t.Fatalf("drafts = %q, want them to grow into the turn", drafts)
	}
	if audio := session.sent[0].Audio; audio == nil || audio.MIMEType != "audio/pcm;rate=16000" {
		t.Fatalf("first realtime input = %+v, want 16 kHz PCM audio", session.sent[0])
	}
}
