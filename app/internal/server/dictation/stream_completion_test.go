//go:build linux

package dictation

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

type simultaneousFailureStream struct {
	sending, receiving, release chan struct{}
}

func (*simultaneousFailureStream) CompleteCapture() bool { return true }
func (s *simultaneousFailureStream) SendPCM(ctx context.Context, _ []byte) error {
	close(s.sending)
	select {
	case <-s.release:
		return errors.New("upstream secret=private-token")
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (*simultaneousFailureStream) Finalize(context.Context) error { return nil }
func (s *simultaneousFailureStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	close(s.receiving)
	select {
	case <-s.release:
		return speechkit.DictationStreamEvent{}, errors.New("upstream secret=private-token")
	case <-ctx.Done():
		return speechkit.DictationStreamEvent{}, ctx.Err()
	}
}
func (*simultaneousFailureStream) Close() error { return nil }

type simultaneousFailureRouter struct{ stream *simultaneousFailureStream }

func (*simultaneousFailureRouter) HasDictationStreaming() bool { return true }
func (r *simultaneousFailureRouter) StartDictationStream(context.Context, speechkit.DictationStreamOptions, speaker.AudioFormat) (speechkit.DictationStream, error) {
	return r.stream, nil
}

// A provider upload failure can surface in SendPCM and Receive concurrently.
// The client must get one safe terminal outcome, with no later segment output.
func TestStreamWS_ConcurrentProviderFailuresHaveOneSafeCompletion(t *testing.T) {
	stream := &simultaneousFailureStream{sending: make(chan struct{}), receiving: make(chan struct{}), release: make(chan struct{})}
	ctx, conn := openRecordingWS(t, &simultaneousFailureRouter{stream}, nil)
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	for _, entered := range []chan struct{}{stream.sending, stream.receiving} {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("provider pumps did not overlap")
		}
	}
	close(stream.release)
	failure := readStreamJSON(t, ctx, conn)
	if failure["type"] != StreamMsgError || failure["code"] != StreamErrProviderStreamFailed || strings.Contains(failure["message"].(string), "private-token") {
		t.Fatalf("unsafe terminal result: %v", failure)
	}
	if done := readStreamJSON(t, ctx, conn); done["type"] != StreamMsgSegmentDone {
		t.Fatalf("missing terminal completion: %v", done)
	}
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgPing})
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgPong {
		t.Fatalf("post-terminal segment output: %v", frame)
	}
}

type batchOnlyProvider struct {
	input []byte
	opts  stt.TranscribeOpts
}

func (*batchOnlyProvider) Name() string                 { return "assemblyai" }
func (*batchOnlyProvider) Health(context.Context) error { return nil }
func (*batchOnlyProvider) StartDictationStream(context.Context, speechkit.DictationStreamOptions, speaker.AudioFormat) (speechkit.DictationStream, error) {
	return nil, errors.New("live handshake unavailable")
}
func (p *batchOnlyProvider) Transcribe(_ context.Context, pcm []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	p.input = append([]byte(nil), pcm...)
	p.opts = opts
	return &stt.Result{Text: "Whole recording retained", Provider: p.Name()}, nil
}

// Known profile capabilities choose same-provider full-recording batch before
// an unnecessary live handshake; a live outage cannot break finals-only input.
func TestStreamWS_FinalsOnlyUsesChosenBatchProviderWithoutLiveHandshake(t *testing.T) {
	provider := &batchOnlyProvider{}
	router := &stt.Router{Strategy: stt.StrategyCloudOnly}
	router.AddCloud(provider)
	ctx, conn := openRecordingWS(t, router, nil)
	pcm := bytes.Repeat([]byte{1, 2}, 320)
	if err := conn.Write(ctx, websocket.MessageBinary, pcm); err != nil {
		t.Fatal(err)
	}
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
	final := readStreamJSON(t, ctx, conn)
	if final["done"] != true || final["text"] != "Whole recording retained" || final["provider"] != "assemblyai" || !bytes.Equal(provider.input, pcm) {
		t.Fatalf("chosen batch provider failed despite valid recording: %v", final)
	}
	if done := readStreamJSON(t, ctx, conn); done["type"] != StreamMsgSegmentDone {
		t.Fatalf("missing complete recording result: %v", done)
	}
}
