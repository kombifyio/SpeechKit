package speechkit

import (
	"context"
	"testing"
)

func TestModeScopedServiceContractsArePublicAndDistinct(t *testing.T) {
	var _ DictationService = testDictationService{}
	var _ AssistService = testAssistService{}
	var _ VoiceAgentService = testVoiceAgentService{}

	assistResult, err := testAssistService{}.Process(context.Background(), AssistRequest{
		Text:           "summarize this",
		EditableTarget: false,
	})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if assistResult.Surface != AssistSurfacePanel {
		t.Fatalf("assist surface = %q, want %q", assistResult.Surface, AssistSurfacePanel)
	}
}

type testDictationService struct{}

func (testDictationService) Start(context.Context) error { return nil }
func (testDictationService) Stop(context.Context) (DictationRun, error) {
	return DictationRun{}, nil
}

type testAssistService struct{}

func (testAssistService) Process(context.Context, AssistRequest) (AssistResult, error) {
	return AssistResult{Text: "ok", Surface: AssistSurfacePanel}, nil
}

type testVoiceAgentService struct{}

func (testVoiceAgentService) Start(context.Context) error { return nil }
func (testVoiceAgentService) Stop(context.Context) (VoiceAgentSession, error) {
	return VoiceAgentSession{}, nil
}
func (testVoiceAgentService) SendText(context.Context, string) error { return nil }
func (testVoiceAgentService) CurrentSession(context.Context) (VoiceAgentSession, error) {
	return VoiceAgentSession{}, nil
}
