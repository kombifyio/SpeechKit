package allproviders_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/foundry"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

// Hosts branch on the shared live sentinels, not on provider-specific
// strings, so every shipped provider must wrap them.
func TestLiveProvidersWrapSharedSentinels(t *testing.T) {
	providers := map[string]live.LiveProvider{
		"openai":     openai.New(),
		"deepgram":   deepgram.New(),
		"assemblyai": assemblyai.New(),
	}
	for name, p := range providers {
		t.Run(name+"/not-connected", func(t *testing.T) {
			if err := p.SendAudio([]byte{0, 0}); !errors.Is(err, live.ErrNotConnected) {
				t.Fatalf("SendAudio before Connect = %v, want ErrNotConnected", err)
			}
			if _, err := p.Receive(context.Background()); !errors.Is(err, live.ErrNotConnected) {
				t.Fatalf("Receive before Connect = %v, want ErrNotConnected", err)
			}
		})
		t.Run(name+"/missing-api-key", func(t *testing.T) {
			err := p.Connect(context.Background(), live.LiveConfig{})
			if !errors.Is(err, live.ErrMissingAPIKey) {
				t.Fatalf("Connect without APIKey = %v, want ErrMissingAPIKey", err)
			}
		})
	}

	t.Run("foundry/missing-endpoint", func(t *testing.T) {
		err := foundry.New().Connect(context.Background(), live.LiveConfig{APIKey: "k"})
		if !errors.Is(err, live.ErrMissingEndpoint) {
			t.Fatalf("Connect without Endpoint = %v, want ErrMissingEndpoint", err)
		}
	})
}
