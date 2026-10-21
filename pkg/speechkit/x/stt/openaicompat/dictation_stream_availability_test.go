package openaicompat

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// The adapter also serves Groq, Ollama and self-hosted servers, which cannot
// stream: a router holding only those must not advertise live dictation, or
// clients would open a stream that can never start instead of batching.
func TestRouterAdvertisesLiveDictationOnlyForOpenAI(t *testing.T) {
	for _, tc := range []struct {
		provider *Provider
		want     bool
	}{
		{provider: NewGroq(Options{APIKey: "key"}), want: false},
		{provider: NewOpenAI(Options{APIKey: "key"}), want: true},
	} {
		router := &stt.Router{Strategy: stt.StrategyCloudOnly}
		router.AddCloud(tc.provider)
		if got := router.HasDictationStreaming(); got != tc.want {
			t.Fatalf("%s: HasDictationStreaming = %v, want %v", tc.provider.Name(), got, tc.want)
		}
	}
}
