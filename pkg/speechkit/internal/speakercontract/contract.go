package speakercontract

import (
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

func AssertDiarizationResult(t testing.TB, result *speaker.DiarizationResult) {
	t.Helper()
	if result == nil {
		t.Fatal("diarization result is nil")
		return
	}
	if strings.TrimSpace(result.Provider) == "" {
		t.Fatalf("provider is empty: %+v", result)
	}
	if strings.TrimSpace(result.Model) == "" {
		t.Fatalf("model is empty: %+v", result)
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatalf("text is empty: %+v", result)
	}
	if result.Level == "" || result.Level == speaker.IdentificationNone {
		t.Fatalf("level = %q, want diarization or stronger", result.Level)
	}
	if len(result.Words) == 0 {
		t.Fatalf("words are empty: %+v", result)
	}
	if len(result.Segments) == 0 {
		t.Fatalf("segments are empty: %+v", result)
	}
	if len(result.Speakers) == 0 {
		t.Fatalf("speakers are empty: %+v", result)
	}
	for _, word := range result.Words {
		if strings.TrimSpace(word.Text) == "" {
			t.Fatalf("word text is empty: %+v", word)
		}
	}
}

func AssertKnownAttribution(t testing.TB, result *speaker.DiarizationResult, wantName string) {
	t.Helper()
	AssertDiarizationResult(t, result)
	for _, segment := range result.Segments {
		if segment.DisplayName == wantName || segment.PersonID == wantName {
			return
		}
	}
	for _, person := range result.Speakers {
		if person.DisplayName == wantName || person.PersonID == wantName {
			return
		}
	}
	t.Fatalf("known attribution %q not found in %+v", wantName, result)
}

func AssertSpeakerFrame(t testing.TB, frame *speaker.SpeakerFrame) {
	t.Helper()
	if frame == nil {
		t.Fatal("speaker frame is nil")
		return
	}
	if strings.TrimSpace(frame.Provider) == "" {
		t.Fatalf("provider is empty: %+v", frame)
	}
	if strings.TrimSpace(frame.Model) == "" {
		t.Fatalf("model is empty: %+v", frame)
	}
	if frame.Sequence <= 0 {
		t.Fatalf("sequence = %d, want positive", frame.Sequence)
	}
	if strings.TrimSpace(frame.Text) == "" && len(frame.Words) == 0 {
		t.Fatalf("frame has no text or words: %+v", frame)
	}
	for _, word := range frame.Words {
		if strings.TrimSpace(word.Text) == "" {
			t.Fatalf("word text is empty: %+v", word)
		}
	}
}

func AssertUnknownIsUnattributed(t testing.TB, raw any) {
	t.Helper()
	if got := speaker.NormalizeSpeakerLabel(raw); got != "" {
		t.Fatalf("NormalizeSpeakerLabel(%v) = %q, want unattributed", raw, got)
	}
}

func AssertProviderError(t testing.TB, err error, provider string) {
	t.Helper()
	if err == nil {
		t.Fatal("provider error is nil")
		return
	}
	message := err.Error()
	if !strings.Contains(strings.ToLower(message), strings.ToLower(provider)) {
		t.Fatalf("provider error %q does not mention provider %q", message, provider)
	}
	for _, forbidden := range []string{"secret", "api key sk-", "token-", "invalid api key"} {
		if strings.Contains(strings.ToLower(message), forbidden) {
			t.Fatalf("provider error leaks sensitive body detail: %q", message)
		}
	}
}
