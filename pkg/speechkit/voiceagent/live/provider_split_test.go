package live_test

import (
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

// TestProviderSubpackagesSatisfyLiveProvider pins the v0.64 migration window
// for the realtime providers: each new import path must construct a working
// LiveProvider. A subpackage that re-exports the wrong constructor fails here
// rather than in a consumer's build after v0.65 removes the old names.
func TestProviderSubpackagesSatisfyLiveProvider(t *testing.T) {
	providers := map[string]live.LiveProvider{
		"live/gemini.New":     gemini.New(),
		"live/openai.New":     openai.New(),
		"live/deepgram.New":   deepgram.New(),
		"live/assemblyai.New": assemblyai.New(),
	}
	for path, provider := range providers {
		if provider == nil {
			t.Errorf("%s returned nil", path)
		}
	}
}
