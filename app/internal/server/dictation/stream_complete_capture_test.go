//go:build linux

package dictation

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kombifyio/SpeechKit/app/internal/server/wssession"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/customize"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

type liveWindowStream struct{ *fakeDictationStream }

func (*liveWindowStream) CompleteCapture() bool { return false }

type recordingRouter struct {
	*fakeStreamRouter
	input []byte
	opts  stt.TranscribeOpts
}

type stalledDraftStream struct {
	*fakeDictationStream
	entered, canceled, closing, releaseClose, closed chan struct{}
	enterOnce                                        sync.Once
}

func (s *stalledDraftStream) SendPCM(ctx context.Context, _ []byte) error {
	s.enterOnce.Do(func() { close(s.entered) })
	<-ctx.Done()
	close(s.canceled)
	return ctx.Err()
}

func (s *stalledDraftStream) Close() error {
	close(s.closing)
	<-s.releaseClose
	close(s.closed)
	return nil
}

// An optional stalled draft upload and its close handshake must not hold the
// microphone, finalization or the complete authoritative recording hostage.
func TestCompleteCapture_OptionalDraftCannotBlockRecording(t *testing.T) {
	for _, interim := range []bool{true, false} {
		t.Run(map[bool]string{true: "stalled drafts", false: "no drafts requested"}[interim], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			live := &stalledDraftStream{fakeDictationStream: newFakeDictationStream(), entered: make(chan struct{}), canceled: make(chan struct{}), closing: make(chan struct{}), releaseClose: make(chan struct{}), closed: make(chan struct{})}
			defer func() { close(live.releaseClose); <-live.closed }()
			batch := &recordingRouter{}
			capture := newCompleteRecordingStream(ctx, live, batch, speechkit.DictationStreamOptions{InterimResults: interim}, speaker.AudioFormat{}, 1024)
			defer capture.Close()
			pcm := bytes.Repeat([]byte{1, 2}, 32)
			sent := make(chan error, 1)
			go func() { sent <- capture.SendPCM(ctx, pcm) }()
			select {
			case err := <-sent:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("optional draft blocked capture")
			}
			if interim {
				select {
				case <-live.entered:
				case <-ctx.Done():
					t.Fatal("draft sender never started")
				}
			} else {
				select {
				case <-live.closing:
				case <-ctx.Done():
					t.Fatal("unused paid socket stayed open")
				}
			}
			if err := capture.Finalize(ctx); err != nil {
				t.Fatal(err)
			}
			final, err := capture.Receive(ctx)
			if err != nil || !final.IsFinal || !bytes.Equal(batch.input, pcm) {
				t.Fatalf("stalled draft lost complete recording: %+v %v", final, err)
			}
			if interim {
				select {
				case <-live.canceled:
				case <-ctx.Done():
					t.Fatal("stalled draft send was not canceled")
				}
			} else {
				select {
				case <-live.entered:
					t.Fatal("audio was uploaded despite interim_results=false")
				default:
				}
			}
		})
	}
}

func openRecordingWS(t *testing.T, router StreamRouter, configure func(*StreamHandlerOptions), requested ...StreamStartFrame) (context.Context, *websocket.Conn) {
	t.Helper()
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	handler := mustStreamHandler(t, manager, router, configure)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	t.Cleanup(func() { _ = conn.CloseNow() })
	start := StreamStartFrame{Type: StreamMsgStart, InterimResults: boolPtr(false)}
	if len(requested) > 0 {
		start = requested[0]
		start.Type = StreamMsgStart
	}
	writeStreamJSON(t, ctx, conn, start)
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
		t.Fatalf("start: %v", frame)
	}
	return ctx, conn
}

// Empty presses and rejected recording limits need stable, distinguishable
// outcomes rather than a misleading upstream-provider failure.
func TestStreamWS_CaptureOutcomeCodes(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		bytes      int
		finalize   bool
	}{
		{"empty press", StreamErrEmptyTranscript, 0, true},
		{"upload limit", StreamErrPayloadTooLarge, (1 << 20) + 640, false},
		{"duration limit", StreamErrAudioTooLong, 64000, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &recordingRouter{fakeStreamRouter: &fakeStreamRouter{has: true}}
			ctx, conn := openRecordingWS(t, router, func(o *StreamHandlerOptions) {
				o.MaxUploadMB = 1
				o.MaxDecodedAudioSeconds = 600
				if tc.code == StreamErrAudioTooLong {
					o.MaxDecodedAudioSeconds = 1
				}
			})
			for remaining := tc.bytes; remaining > 0; {
				size := min(remaining, 32000)
				if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, size)); err != nil {
					t.Fatal(err)
				}
				remaining -= size
			}
			if tc.finalize {
				writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
			}
			failure := readStreamJSON(t, ctx, conn)
			if failure["type"] != StreamMsgError || failure["code"] != tc.code {
				t.Fatalf("wrong outcome for %s: %v", tc.name, failure)
			}
			if done := readStreamJSON(t, ctx, conn); done["type"] != StreamMsgSegmentDone {
				t.Fatalf("rejected segment did not terminate: %v", done)
			}
		})
	}
}

type slowRecordingRouter struct {
	*recordingRouter
	entered, release chan struct{}
}

func (r *slowRecordingRouter) Route(ctx context.Context, pcm []byte, duration float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	close(r.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.release:
		return r.recordingRouter.Route(ctx, pcm, duration, opts)
	}
}

// Active finalization uses the recognition deadline; the microphone idle
// watchdog resumes only after the finished segment.
func TestStreamWS_FinalizationOutlivesIdleAndRearmsIt(t *testing.T) {
	router := &slowRecordingRouter{recordingRouter: &recordingRouter{fakeStreamRouter: &fakeStreamRouter{has: true}}, entered: make(chan struct{}), release: make(chan struct{})}
	ctx, conn := openRecordingWS(t, router, func(o *StreamHandlerOptions) { o.IdleTimeout = 600 * time.Millisecond })
	if err := conn.Write(ctx, websocket.MessageBinary, make([]byte, 640)); err != nil {
		t.Fatal(err)
	}
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
	select {
	case <-router.entered:
	case <-ctx.Done():
		t.Fatal("batch did not start")
	}
	time.Sleep(1800 * time.Millisecond)
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgPing})
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgPong {
		t.Fatalf("active recognition expired as idle: %v", frame)
	}
	close(router.release)
	if frame := readStreamJSON(t, ctx, conn); frame["done"] != true {
		t.Fatalf("complete recording failed: %v", frame)
	}
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgSegmentDone {
		t.Fatalf("segment did not finish: %v", frame)
	}
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgSessionEnd || frame["reason"] != StreamEndReasonIdle {
		t.Fatalf("idle cleanup did not resume: %v", frame)
	}
}

func (r *recordingRouter) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	stream, err := r.fakeStreamRouter.StartDictationStream(ctx, opts, format)
	if err != nil {
		return nil, err
	}
	return &liveWindowStream{stream.(*fakeDictationStream)}, nil
}

func (r *recordingRouter) Route(_ context.Context, pcm []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	r.input, r.opts = append([]byte(nil), pcm...), opts
	return &stt.Result{Text: "Die Workpaths bitte updaten", Language: "multi", Provider: "batch"}, nil
}

// Regression: live endpointing dropped words that the same full recording
// retained. A server keyboard must commit only the whole-recording result,
// including when its native stream supports only moving-window finals.
func TestStreamWS_CommitsCompleteRecordingInsteadOfLiveFinals(t *testing.T) {
	t.Setenv(wssession.EnvAllowEmptyOrigin, "1")
	manager := mustStreamManager(t)
	stream := newFakeDictationStream()
	router := &recordingRouter{fakeStreamRouter: &fakeStreamRouter{has: true, prepared: []*fakeDictationStream{stream}}}
	handler := mustStreamHandler(t, manager, router, nil)
	mux := http.NewServeMux()
	handler.Mount(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	session, ticket, err := manager.Create(wssession.Identity{UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn := dialStreamWS(t, ctx, server.URL, session.ID, ticket)
	defer conn.CloseNow() //nolint:errcheck
	writeStreamJSON(t, ctx, conn, StreamStartFrame{Type: StreamMsgStart, Language: "multi", PromptHint: "Keep product names", VocabularyHints: customize.RecognitionHints{Keyterms: []string{"Workpaths"}}, RecognitionOptions: provideropts.Values{provideropts.OptionNumerals: true}})
	if frame := readStreamJSON(t, ctx, conn); frame["type"] != StreamMsgReady {
		t.Fatalf("start: %v", frame)
	}
	first, second := bytes.Repeat([]byte{1, 2}, 1600), bytes.Repeat([]byte{3, 4}, 1600)
	if err := conn.Write(ctx, websocket.MessageBinary, first); err != nil {
		t.Fatal(err)
	}
	stream.emit(speechkit.DictationStreamEvent{Text: "Die bitte", IsFinal: true})
	draft := readStreamJSON(t, ctx, conn)
	if draft["type"] != StreamMsgTranscript || draft["done"] != false {
		t.Fatalf("live final must remain a draft: %v", draft)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, second); err != nil {
		t.Fatal(err)
	}
	writeStreamJSON(t, ctx, conn, map[string]string{"type": StreamMsgFinalize})
	final := readStreamJSON(t, ctx, conn)
	if final["done"] != true || final["text"] != "Die Workpaths bitte updaten" {
		t.Fatalf("whole recording was not committed: %v", final)
	}
	if done := readStreamJSON(t, ctx, conn); done["type"] != StreamMsgSegmentDone {
		t.Fatalf("segment did not finish: %v", done)
	}
	if !bytes.Equal(router.input, append(first, second...)) {
		t.Fatal("batch recognition did not receive all audio in order")
	}
	if router.opts.Language != "multi" || router.opts.Prompt != "Keep product names" || len(router.opts.VocabularyHints.Keyterms) == 0 || router.opts.Options[provideropts.OptionNumerals] != true {
		t.Fatalf("recognition customization was lost: %+v", router.opts)
	}
}
