//go:build hass_filetest

package main

import (
	"context"
	"os"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
)

// These minimal host projections let the pure-Go Home Assistant authority
// tests and voice_agent_local.go type-check as an explicit file-list test on
// Windows hosts without a C compiler. Production definitions remain behind the
// windows+cgo build tag in the other example files.
type Config struct {
	HomeAssistant struct {
		BaseURL  string
		TokenEnv string
		Language string
	}
	VoiceAgent struct {
		Provider          string
		Model             string
		Voice             string
		Locale            string
		SystemPrompt      string
		IdleReminderSec   int
		IdleDeactivateSec int
		WaitTimeoutSec    int
		ThinkEndpointURL  string
		ThinkProvider     string
		ThinkModel        string
		ThinkAPIKeyEnv    string
	}
}

func (c *Config) haToken() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(os.Getenv(strings.TrimSpace(c.HomeAssistant.TokenEnv)))
}

type AudioIO struct{ fanout audioFanout }

func (*AudioIO) Ding()                                           {}
func (*AudioIO) CaptureAccepted()                                {}
func (*AudioIO) PlayCue(string) error                            { return nil }
func (*AudioIO) PlayPCM([]byte, int, int) error                  { return nil }
func (a *AudioIO) SubscribeMonitored(buf int) *audioSubscription { return a.fanout.subscribe(buf) }
func (a *AudioIO) Unsubscribe(frames chan []byte)                { a.fanout.unsubscribe(frames) }

type BoxLink struct{}

func (*BoxLink) SetStage(companion.Stage) {}

func streamUtterance(context.Context, *Config, *AudioIO, func([]byte) error) (int, bool, error) {
	return 0, false, nil
}

func streamUtteranceChecked(context.Context, *Config, *AudioIO, func([]byte) error) (utteranceCaptureResult, error) {
	return utteranceCaptureResult{}, nil
}

func streamUtteranceCheckedFromSubscription(context.Context, *Config, *AudioIO, *audioSubscription, func([]byte) error) (utteranceCaptureResult, error) {
	return utteranceCaptureResult{}, nil
}

func streamAuthorityUtteranceCheckedFromSubscription(context.Context, *Config, *AudioIO, *audioSubscription, func([]byte) error) (utteranceCaptureResult, error) {
	return utteranceCaptureResult{}, nil
}

func normalizeResponseGain(pcm []byte) []byte { return pcm }

func wavFromPCM16(pcm []byte, _ int) []byte { return append([]byte(nil), pcm...) }

func resolveCompanionSecret(string) string { return "" }

func boxSessionSerial() string { return "FILETEST" }
