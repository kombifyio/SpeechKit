package voicelive_test

// Live handshake against a real Foundry resource. Off by default; run with
// SPEECHKIT_RUN_LIVE_FOUNDRY_TEST=1, SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT and
// AZURE_AI_API_KEY (nothing tenant-specific in the tree). Optional
// SPEECHKIT_VOICELIVE_API_VERSION overrides the api-version under test.

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/voicelive"
)

func TestLiveVoiceLiveHandshake(t *testing.T) {
	if os.Getenv("SPEECHKIT_RUN_LIVE_FOUNDRY_TEST") != "1" {
		t.Skip("set SPEECHKIT_RUN_LIVE_FOUNDRY_TEST=1 to run the live Voice Live handshake")
	}
	endpoint := strings.TrimSpace(os.Getenv("SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT"))
	key := strings.TrimSpace(os.Getenv("AZURE_AI_API_KEY"))
	if endpoint == "" || key == "" {
		t.Skip("SPEECHKIT_FOUNDRY_PROJECT_ENDPOINT and AZURE_AI_API_KEY are required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		t.Fatalf("endpoint %q: %v", endpoint, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	p := voicelive.New()
	p.APIVersion = strings.TrimSpace(os.Getenv("SPEECHKIT_VOICELIVE_API_VERSION"))
	cfg := live.LiveConfig{
		Endpoint: "wss://" + parsed.Host + "/voice-live/realtime",
		APIKey:   key,
		Model:    voicelive.DefaultModel,
		Voice:    voicelive.DefaultVoice,
		Locale:   "de-DE",
	}
	if err := p.Connect(ctx, cfg); err != nil {
		t.Fatalf("connect (api-version %q): %v", p.APIVersion, err)
	}
	t.Logf("session established with model %s, voice %s, api-version %q", cfg.Model, cfg.Voice, p.APIVersion)
	if err := p.Close(); err != nil {
		t.Logf("close: %v", err)
	}
}
