package stt

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestOpenRouterLiveMultilanguageRoundtrip is the live gate the OpenRouter
// manifest entry was missing.
//
// Every other provider's multilanguage behaviour is backed by a vendor
// document, but OpenRouter publishes no dedicated audio-transcription
// reference, so its entry was derived from OpenAI schema compatibility and
// explicitly marked "needs a live roundtrip before it counts as verified".
// Derivation is exactly how a wrong entry survives review, so the only way to
// promote it is to send real audio and look at what comes back.
//
// Skips without OPENROUTER_API_KEY, so it is free to run in the normal suite.
func TestOpenRouterLiveMultilanguageRoundtrip(t *testing.T) {
	apiKey := strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY"))
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY is not set; this gate needs a real provider roundtrip")
	}

	audio, err := os.ReadFile("../../../testdata/e2e/dictation/hello-world.wav")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	model := strings.TrimSpace(os.Getenv("OPENROUTER_STT_MODEL"))
	if model == "" {
		model = "openai/whisper-1"
	}

	for _, tc := range []struct {
		name     string
		opts     TranscribeOpts
		wantText string
	}{
		{
			// The claim under test: omitting the field lets the routed model
			// detect the language, so English audio transcribes as English
			// without anything being pinned.
			name:     "unpinned transcribes English",
			opts:     TranscribeOpts{Model: model},
			wantText: "hello",
		},
		{
			// The counter-test that made this whole class of bug visible on
			// Deepgram: pinning a language the speech is not in must not be
			// how we discover the parameter is ignored.
			name:     "explicit pin is accepted",
			opts:     TranscribeOpts{Model: model, Language: "en"},
			wantText: "hello",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := NewOpenRouterSTTProvider(apiKey, model)
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()

			result, err := provider.Transcribe(ctx, audio, tc.opts)
			if err != nil {
				t.Fatalf("Transcribe: %v", err)
			}
			t.Logf("openrouter model=%s language=%q text=%q", model, result.Language, result.Text)

			if !strings.Contains(strings.ToLower(result.Text), tc.wantText) {
				t.Errorf("transcript = %q, want it to contain %q", result.Text, tc.wantText)
			}
			if strings.TrimSpace(result.Text) == "" {
				t.Error("empty transcript: the request reached the provider but produced no text")
			}
		})
	}
}
