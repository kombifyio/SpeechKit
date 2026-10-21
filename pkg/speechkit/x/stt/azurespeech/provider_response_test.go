package azurespeech

import (
	"context"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func TestTranscribe_MapsResponse(t *testing.T) {
	server, _ := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Hallo Welt. Guten Tag." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Language != "de" {
		t.Errorf("Language = %q, want the phrase locale", res.Language)
	}
	if res.Provider != "foundry" {
		t.Errorf("Provider = %q", res.Provider)
	}
	if res.Model != DefaultModel {
		t.Errorf("Model = %q", res.Model)
	}
	if math.Abs(res.Confidence-0.8) > 1e-9 {
		t.Errorf("Confidence = %v, want the mean 0.8", res.Confidence)
	}
	if res.Words != nil {
		t.Error("Words must stay nil: MAI reports no per-word confidence")
	}
	if res.Speakers != nil {
		t.Error("Speakers must stay nil when diarization was not requested")
	}
	if res.Duration <= 0 {
		t.Error("Duration must record the wall time")
	}
}

func TestTranscribe_MapsDiarization(t *testing.T) {
	server, _ := fakeService(t, http.StatusOK, successBody)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Speaker: speaker.Options{Diarization: true}})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	d := res.Speakers
	if d == nil {
		t.Fatal("Speakers must be populated for a diarized response")
	}
	if d.Provider != "foundry" || d.Model != DefaultModel || d.Level != speaker.IdentificationDiarization {
		t.Errorf("diarization header = %+v", *d)
	}
	if d.Text != res.Text || d.Language != res.Language {
		t.Errorf("diarization text/language must mirror the result: %+v", *d)
	}
	if len(d.Segments) != 2 {
		t.Fatalf("segments = %d, want one per phrase", len(d.Segments))
	}
	first, second := d.Segments[0], d.Segments[1]
	if first.StartMs != 0 || first.EndMs != 1000 || first.Text != "Hallo Welt." {
		t.Errorf("first segment = %+v", first)
	}
	if second.StartMs != 1200 || second.EndMs != 2400 || second.Text != "Guten Tag." {
		t.Errorf("second segment = %+v", second)
	}
	if first.SpeakerLabel == "" || second.SpeakerLabel == "" || first.SpeakerLabel == second.SpeakerLabel {
		t.Errorf("segments must carry distinct speaker labels: %q vs %q", first.SpeakerLabel, second.SpeakerLabel)
	}
	if len(d.Speakers) != 2 || d.Speakers[0].Label != first.SpeakerLabel || d.Speakers[1].Label != second.SpeakerLabel {
		t.Errorf("speakers = %+v, want the two segment labels", d.Speakers)
	}
	if len(first.Words) != 2 || first.Words[1].StartMs != 400 || first.Words[1].EndMs != 1000 || first.Words[1].SpeakerLabel != first.SpeakerLabel {
		t.Errorf("first segment words = %+v", first.Words)
	}
	if len(d.Words) != 2 {
		t.Errorf("flattened words = %d, want 2", len(d.Words))
	}
}

func TestTranscribe_DiarizationWithoutSpeakersStaysNil(t *testing.T) {
	body := `{"combinedPhrases":[{"channel":0,"text":"Nur ich."}],"phrases":[{"channel":0,"offsetMilliseconds":0,"durationMilliseconds":500,"text":"Nur ich.","locale":"de"}]}`
	server, _ := fakeService(t, http.StatusOK, body)
	p := newTestProvider(server, Options{APIKey: "k", Diarization: true})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Speakers != nil {
		t.Errorf("Speakers = %+v, want nil when no phrase carries a speaker", res.Speakers)
	}
	if res.Confidence != 0 {
		t.Errorf("Confidence = %v, want 0 when the service reported none", res.Confidence)
	}
}

func TestTranscribe_TextFallsBackToPhrases(t *testing.T) {
	body := `{"phrases":[{"text":"Erster Teil."},{"text":"Zweiter Teil."}]}`
	server, _ := fakeService(t, http.StatusOK, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	res, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Language: "de"})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if res.Text != "Erster Teil. Zweiter Teil." {
		t.Errorf("Text = %q", res.Text)
	}
	if res.Language != "de" {
		t.Errorf("Language = %q, want the requested language when phrases carry none", res.Language)
	}
}

func TestTranscribe_BadRequestSurfacesServiceMessage(t *testing.T) {
	body := `{"code":"InvalidRequest","message":"Requested MAI transcription model 'MAI-Transcribe-9' is not supported."}`
	server, _ := fakeService(t, http.StatusBadRequest, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{Model: "MAI-Transcribe-9"})
	if err == nil {
		t.Fatal("expected an error for HTTP 400")
	}
	if !strings.Contains(err.Error(), "MAI-Transcribe-9") {
		t.Errorf("the service's explanation must reach the caller: %v", err)
	}
}

func TestTranscribe_AuthErrorDoesNotLeakBody(t *testing.T) {
	body := `{"error":{"code":"401","message":"Access denied due to invalid subscription key sk-secret-body"}}`
	server, _ := fakeService(t, http.StatusUnauthorized, body)
	p := newTestProvider(server, Options{APIKey: "k"})
	_, err := p.Transcribe(context.Background(), []byte("pcm"), stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
	if strings.Contains(err.Error(), "sk-secret-body") {
		t.Errorf("provider response body leaked in error: %v", err)
	}
}
