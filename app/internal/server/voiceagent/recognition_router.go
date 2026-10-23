//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// DictationStreamStarter is the part of the STT router that opens provider-native live
// recognition. The router picks the provider and endpoint by the same configuration as
// streaming Dictation, so a Voice Agent turn never uses a provider or region Dictation would not.
type DictationStreamStarter interface {
	StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error)
	HasDictationStreaming() bool
}

// voiceEndpointingMs is how long the recogniser waits after the speaker stops before it commits
// the utterance. It equals the silence timer the turn used before (800 ms), so turn-taking does not change:
// at 500 ms the live journey split one spoken question into two turns (run 39820854939, a pause inside the
// sentence), which costs a user more than the 300 ms it saves.
const voiceEndpointingMs = 800

// NewRouterRecognizer streams a registered-agent turn's speech to the router's live recognition.
// It returns nil when the router has no provider that streams, so callers keep the batch path.
func NewRouterRecognizer(router DictationStreamStarter) cascaded.RecognitionStreamer {
	if router == nil || !router.HasDictationStreaming() {
		return nil
	}
	return routerRecognizer{router: router}
}

type routerRecognizer struct{ router DictationStreamStarter }

func (r routerRecognizer) OpenRecognition(ctx context.Context, locale string, onFinal func()) (cascaded.Recognition, error) {
	stream, err := r.router.StartDictationStream(ctx, speechkit.DictationStreamOptions{
		Language:      strings.TrimSpace(locale),
		EndpointingMs: voiceEndpointingMs,
	}, speaker.AudioFormat{Encoding: speaker.AudioEncodingLinear16, SampleRateHz: 16000, Channels: 1})
	if err != nil {
		return nil, err
	}
	rec := &routerRecognition{stream: stream, done: make(chan struct{})}
	go rec.read(ctx, onFinal)
	return rec, nil
}

type routerRecognition struct {
	stream speechkit.DictationStream
	done   chan struct{}
	mu     sync.Mutex
	finals []string
	err    error
	once   sync.Once
}

// read collects the recogniser's committed utterances until it closes or the turn ends.
func (r *routerRecognition) read(ctx context.Context, onFinal func()) {
	defer close(r.done)
	for {
		event, err := r.stream.Receive(ctx)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				r.mu.Lock()
				r.err = err
				r.mu.Unlock()
			}
			return
		}
		text := strings.TrimSpace(event.Text)
		if !event.IsFinal || text == "" {
			continue
		}
		r.mu.Lock()
		r.finals = append(r.finals, text)
		r.mu.Unlock()
		if onFinal != nil {
			onFinal()
		}
	}
}

func (r *routerRecognition) SendPCM(ctx context.Context, pcm []byte) error {
	return r.stream.SendPCM(ctx, pcm)
}

// Finish asks the recogniser to flush and waits until it has closed, so the text holds every
// utterance of the turn.
func (r *routerRecognition) Finish(ctx context.Context) (string, error) {
	if err := r.stream.Finalize(ctx); err != nil {
		return "", err
	}
	select {
	case <-r.done:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return "", r.err
	}
	return strings.Join(r.finals, " "), nil
}

func (r *routerRecognition) Close() error {
	var err error
	r.once.Do(func() { err = r.stream.Close() })
	return err
}
