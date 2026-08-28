package allproviders_test

import (
	"context"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/livecontract"
)

func TestLiveProviderContractHarness(t *testing.T) {
	var provider *scriptedLiveProvider
	livecontract.Run(t, livecontract.Case{
		NewProvider: func() live.LiveProvider {
			provider = &scriptedLiveProvider{
				next: &live.LiveMessage{
					EventType: live.LiveEventOutputText,
					Text:      "pong",
					Done:      true,
				},
			}
			return provider
		},
		Config:        live.LiveConfig{Model: "test-live"},
		Text:          "hello",
		WantName:      "scripted",
		WantEventType: live.LiveEventOutputText,
	})
	if got := strings.Join(provider.calls, ","); got != "connect,audio,end,text,tool,receive,close,close" {
		t.Fatalf("calls = %s", got)
	}
}

func TestLiveProviderContractReceiveCancellation(t *testing.T) {
	livecontract.RunReceiveCancellation(t, livecontract.Case{
		NewProvider: func() live.LiveProvider {
			return &scriptedLiveProvider{next: &live.LiveMessage{Text: "late"}}
		},
		Config: live.LiveConfig{Model: "test-live"},
	})
}

func TestPublicLiveProvidersSatisfyContractInterface(t *testing.T) {
	var _ live.LiveProvider = deepgram.New()
	var _ live.LiveProvider = gemini.New()
	var _ live.LiveProvider = openai.New()
	var _ live.LiveProvider = assemblyai.New()
	var _ live.LiveSessionCapabilities = deepgram.New()
	var _ live.LiveSessionCapabilities = gemini.New()
	var _ live.LiveSessionCapabilities = openai.New()
	var _ live.LiveSessionCapabilities = assemblyai.New()
}

type scriptedLiveProvider struct {
	connected bool
	closed    bool
	next      *live.LiveMessage
	calls     []string
}

func (p *scriptedLiveProvider) Connect(ctx context.Context, cfg live.LiveConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return fmt.Errorf("model required")
	}
	p.connected = true
	p.calls = append(p.calls, "connect")
	return nil
}

func (p *scriptedLiveProvider) SendAudio(chunk []byte) error {
	if !p.connected || p.closed {
		return fmt.Errorf("not connected")
	}
	if len(chunk) == 0 {
		return fmt.Errorf("audio chunk required")
	}
	p.calls = append(p.calls, "audio")
	return nil
}

func (p *scriptedLiveProvider) SendAudioStreamEnd() error {
	if !p.connected || p.closed {
		return fmt.Errorf("not connected")
	}
	p.calls = append(p.calls, "end")
	return nil
}

func (p *scriptedLiveProvider) Receive(ctx context.Context) (*live.LiveMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !p.connected || p.closed {
		return nil, fmt.Errorf("not connected")
	}
	p.calls = append(p.calls, "receive")
	return p.next, nil
}

func (p *scriptedLiveProvider) SendText(text string) error {
	if !p.connected || p.closed {
		return fmt.Errorf("not connected")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("text required")
	}
	p.calls = append(p.calls, "text")
	return nil
}

func (p *scriptedLiveProvider) SendToolResponse(response live.ToolResponse) error {
	if !p.connected || p.closed {
		return fmt.Errorf("not connected")
	}
	if strings.TrimSpace(response.ID) == "" {
		return fmt.Errorf("tool response id required")
	}
	p.calls = append(p.calls, "tool")
	return nil
}

func (p *scriptedLiveProvider) Close() error {
	if !p.connected {
		return fmt.Errorf("not connected")
	}
	p.closed = true
	p.calls = append(p.calls, "close")
	return nil
}

func (p *scriptedLiveProvider) Name() string { return "scripted" }
