package deepgram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

// Dictation commits this stream's final, so it must be the REST transcript of
// everything that was sent, requested multilingual unless a language was
// chosen: the live socket lost words on the same audio.
func TestCompleteCaptureStreamReturnsTheTranscriptOfTheWholeCapture(t *testing.T) {
	type upload struct {
		query url.Values
		body  []byte
	}
	uploads := make(chan upload, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		uploads <- upload{query: r.URL.Query(), body: body}
		_ = json.NewEncoder(w).Encode(deepgramResponse{Results: deepgramResults{Channels: []deepgramChannel{{
			Alternatives: []deepgramAlternative{{Transcript: "Die Workpaths bitte updaten", Confidence: 0.9}},
		}}}})
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// The recording controller cancels the context it starts a stream with as
	// soon as Start returns; the upload has to outlive it.
	startCtx, startDone := context.WithCancel(ctx)
	stream, err := newTestDeepgramProvider(server.URL).StartDictationStream(startCtx, speechkit.DictationStreamOptions{CompleteCapture: true}, speaker.AudioFormat{})
	startDone()
	if err != nil {
		t.Fatalf("StartDictationStream: %v", err)
	}
	defer stream.Close() //nolint:errcheck // test cleanup
	if !speechkit.IsCompleteCaptureStream(stream) {
		t.Fatal("the stream does not report a complete-capture final")
	}

	first, second := bytes.Repeat([]byte{1, 2}, 1600), bytes.Repeat([]byte{3, 4}, 1600)
	for _, frame := range [][]byte{first, second} {
		if err := stream.SendPCM(ctx, frame); err != nil {
			t.Fatalf("SendPCM: %v", err)
		}
	}
	if err := stream.Finalize(ctx); err != nil {
		t.Fatalf("Finalize: %v", err)
	}
	final, err := stream.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if !final.IsFinal || final.Text != "Die Workpaths bitte updaten" {
		t.Fatalf("final = %+v, want the REST transcript as the final", final)
	}
	got := <-uploads
	if !bytes.Equal(got.body, append(append([]byte(nil), first...), second...)) {
		t.Fatalf("Deepgram received %d bytes, want the whole capture in order", len(got.body))
	}
	if language := got.query.Get("language"); language != "multi" {
		t.Fatalf("language = %q, want the multilingual mode when none was chosen", language)
	}
	if _, err := stream.Receive(ctx); !errors.Is(err, io.EOF) {
		t.Fatalf("Receive after the final = %v, want io.EOF", err)
	}
}
