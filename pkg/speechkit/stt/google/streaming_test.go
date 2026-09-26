package google

import (
	"context"
	"io"
	"testing"

	"cloud.google.com/go/speech/apiv2/speechpb"
)

func respFrom(final bool, transcript string) *speechpb.StreamingRecognizeResponse {
	return &speechpb.StreamingRecognizeResponse{Results: []*speechpb.StreamingRecognitionResult{
		{IsFinal: final, Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: transcript}}},
	}}
}

func TestGoogleStreamingTranscriptFrame(t *testing.T) {
	const provider, model = "google", "latest_long"
	cases := []struct {
		name      string
		resp      *speechpb.StreamingRecognizeResponse
		wantNil   bool
		wantText  string
		wantFinal bool
	}{
		{name: "nil response", resp: nil, wantNil: true},
		{name: "no results", resp: &speechpb.StreamingRecognizeResponse{}, wantNil: true},
		{name: "blank transcript", resp: respFrom(false, "   "), wantNil: true},
		{name: "interim", resp: respFrom(false, "guten tag"), wantText: "guten tag", wantFinal: false},
		{name: "final", resp: respFrom(true, "guten tag"), wantText: "guten tag", wantFinal: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := googleStreamingTranscriptFrame(tc.resp, provider, model, 3, 17)
			if tc.wantNil {
				if f != nil {
					t.Fatalf("want nil, got %+v", f)
				}
				return
			}
			if f == nil {
				t.Fatal("want frame, got nil")
			}
			if f.Text != tc.wantText || f.IsFinal != tc.wantFinal {
				t.Fatalf("text=%q final=%v", f.Text, f.IsFinal)
			}
			if f.Provider != provider || f.Model != model || f.Sequence != 3 || f.LatencyMs != 17 {
				t.Fatalf("metadata mismatch: %+v", f)
			}
			// Transcription-only: v2 streaming cannot diarize, so no speaker data.
			if f.Segment != nil || len(f.Speakers) != 0 || len(f.Words) != 0 {
				t.Fatalf("expected no speaker data, got %+v", f)
			}
		})
	}
}

func TestGoogleStreamingTranscriptFrameConcatenatesResults(t *testing.T) {
	resp := &speechpb.StreamingRecognizeResponse{Results: []*speechpb.StreamingRecognitionResult{
		{IsFinal: false, Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: "erster teil"}}},
		{IsFinal: true, Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: "zweiter teil"}}},
	}}
	f := googleStreamingTranscriptFrame(resp, "google", "latest_long", 1, 0)
	if f == nil {
		t.Fatal("want frame")
	}
	if f.Text != "erster teil zweiter teil" {
		t.Fatalf("text=%q", f.Text)
	}
	if !f.IsFinal {
		t.Fatal("want final when any result is final")
	}
}

type fakeGoogleStream struct {
	responses []*speechpb.StreamingRecognizeResponse
	idx       int
	sent      []*speechpb.StreamingRecognizeRequest
	closed    bool
}

func (f *fakeGoogleStream) Send(req *speechpb.StreamingRecognizeRequest) error {
	f.sent = append(f.sent, req)
	return nil
}

func (f *fakeGoogleStream) Recv() (*speechpb.StreamingRecognizeResponse, error) {
	if f.idx >= len(f.responses) {
		return nil, io.EOF
	}
	r := f.responses[f.idx]
	f.idx++
	return r, nil
}

func (f *fakeGoogleStream) CloseSend() error { f.closed = true; return nil }

func TestGoogleSpeakerStreamReceiveSkipsEmptyThenEOF(t *testing.T) {
	fake := &fakeGoogleStream{responses: []*speechpb.StreamingRecognizeResponse{
		{}, // speech-event only, no transcript -> skipped
		respFrom(false, "hallo welt"),
	}}
	s := &googleSpeakerStream{stream: fake, provider: "google", model: "latest_long"}

	frame, err := s.Receive(context.Background())
	if err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if frame == nil || frame.Text != "hallo welt" {
		t.Fatalf("frame=%+v", frame)
	}
	if frame.Sequence != 2 {
		t.Fatalf("want sequence 2 (one empty skipped), got %d", frame.Sequence)
	}
	if _, err := s.Receive(context.Background()); err != io.EOF {
		t.Fatalf("want io.EOF after drain, got %v", err)
	}
}
