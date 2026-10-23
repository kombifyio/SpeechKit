//go:build linux

package voiceagent

import (
	"context"
	"errors"
)

const anonymousGrantHeader = "X-SpeechKit-Anonymous-Voice-Grant"

// Application-managed anonymous reasoning needs the admitting application's
// integration. Standalone hosts can use the configured cascaded provider.
type AnonymousProviderFactory struct {
	STT      CascadedSTT
	TTS      CascadedTTS
	Endpoint string
}

func (*AnonymousProviderFactory) NewProvider() LiveProviderAdapter {
	return unavailableAnonymousProvider{}
}

type unavailableAnonymousProvider struct{}

var errAnonymousIntegrationUnavailable = errors.New("voiceagent: anonymous reasoning integration unavailable")

func (unavailableAnonymousProvider) Connect(context.Context, LiveConfigFrame) error {
	return errAnonymousIntegrationUnavailable
}

func (unavailableAnonymousProvider) SendAudio([]byte) error {
	return errAnonymousIntegrationUnavailable
}

func (unavailableAnonymousProvider) SendAudioStreamEnd() error {
	return errAnonymousIntegrationUnavailable
}

func (unavailableAnonymousProvider) SendText(string) error {
	return errAnonymousIntegrationUnavailable
}

func (unavailableAnonymousProvider) Receive(context.Context) (*LiveMessage, error) {
	return nil, errAnonymousIntegrationUnavailable
}

func (unavailableAnonymousProvider) Close() error { return nil }
func (unavailableAnonymousProvider) Name() string { return "anonymous-guide" }
