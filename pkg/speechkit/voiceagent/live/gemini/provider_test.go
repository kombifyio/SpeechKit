package gemini

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	"google.golang.org/genai"
)

type mockGeminiSession struct {
	realtimeInputs []genai.LiveRealtimeInput
	toolResponses  []genai.LiveToolResponseInput
	receiveMessage *genai.LiveServerMessage
	receiveErr     error
	closeCount     int
}

func (m *mockGeminiSession) SendRealtimeInput(input genai.LiveRealtimeInput) error {
	m.realtimeInputs = append(m.realtimeInputs, input)
	return nil
}

func (m *mockGeminiSession) SendToolResponse(input genai.LiveToolResponseInput) error {
	m.toolResponses = append(m.toolResponses, input)
	return nil
}

func (m *mockGeminiSession) Receive() (*genai.LiveServerMessage, error) {
	if m.receiveErr != nil {
		return nil, m.receiveErr
	}
	return m.receiveMessage, nil
}

func (m *mockGeminiSession) Close() error {
	m.closeCount++
	return nil
}

func TestGeminiLiveSendTextUsesRealtimeInput(t *testing.T) {
	mockSession := &mockGeminiSession{}
	provider := &Provider{session: mockSession}

	if err := provider.SendText("summarize the discussion"); err != nil {
		t.Fatalf("send text: %v", err)
	}

	if len(mockSession.realtimeInputs) != 1 {
		t.Fatalf("expected 1 realtime input, got %d", len(mockSession.realtimeInputs))
	}
	if mockSession.realtimeInputs[0].Text != "summarize the discussion" {
		t.Fatalf("realtime text = %q", mockSession.realtimeInputs[0].Text)
	}
}

func TestGeminiLiveSendAudioAndStreamEndUseRealtimeInput(t *testing.T) {
	mockSession := &mockGeminiSession{}
	provider := &Provider{session: mockSession}

	if err := provider.SendAudio([]byte{1, 2, 3}); err != nil {
		t.Fatalf("send audio: %v", err)
	}
	if err := provider.SendAudioStreamEnd(); err != nil {
		t.Fatalf("send audio stream end: %v", err)
	}

	if len(mockSession.realtimeInputs) != 2 {
		t.Fatalf("expected 2 realtime inputs, got %d", len(mockSession.realtimeInputs))
	}
	audio := mockSession.realtimeInputs[0].Audio
	if audio == nil || audio.MIMEType != "audio/pcm;rate=16000" || string(audio.Data) != string([]byte{1, 2, 3}) {
		t.Fatalf("audio input = %#v", audio)
	}
	if !mockSession.realtimeInputs[1].AudioStreamEnd {
		t.Fatalf("second realtime input = %#v, want stream end", mockSession.realtimeInputs[1])
	}
}

func TestGeminiLiveReceiveMapsServerMessage(t *testing.T) {
	mockSession := &mockGeminiSession{
		receiveMessage: &genai.LiveServerMessage{
			ServerContent: &genai.LiveServerContent{
				ModelTurn: &genai.Content{
					Parts: []*genai.Part{
						{InlineData: &genai.Blob{MIMEType: "audio/pcm", Data: []byte{4, 5}}},
						{Text: "hello "},
						{Text: "world"},
					},
				},
				TurnComplete:        true,
				InputTranscription:  &genai.Transcription{Text: "user text", Finished: true},
				OutputTranscription: &genai.Transcription{Text: "model text", Finished: true},
				Interrupted:         true,
			},
			ToolCall: &genai.LiveServerToolCall{
				FunctionCalls: []*genai.FunctionCall{
					nil,
					{ID: "call-1", Name: "save_summary", Args: map[string]any{"text": "summary"}},
				},
			},
			ToolCallCancellation: &genai.LiveServerToolCallCancellation{IDs: []string{"call-2"}},
			GoAway:               &genai.LiveServerGoAway{},
			SessionResumptionUpdate: &genai.LiveServerSessionResumptionUpdate{
				NewHandle: "resume-1",
			},
		},
	}
	provider := &Provider{session: mockSession, resume: live.NewResumeHandle()}

	msg, err := provider.Receive(context.Background())
	if err != nil {
		t.Fatalf("receive: %v", err)
	}

	if string(msg.Audio) != string([]byte{4, 5}) {
		t.Fatalf("audio = %v", msg.Audio)
	}
	if msg.Text != "hello world" || !msg.Done {
		t.Fatalf("text/done = %q/%v", msg.Text, msg.Done)
	}
	if msg.InputTranscript != "user text" || !msg.InputTranscriptDone {
		t.Fatalf("input transcript = %q done=%v", msg.InputTranscript, msg.InputTranscriptDone)
	}
	if msg.OutputTranscript != "model text" || !msg.OutputTranscriptDone {
		t.Fatalf("output transcript = %q done=%v", msg.OutputTranscript, msg.OutputTranscriptDone)
	}
	if !msg.Interrupted || !msg.GoAway {
		t.Fatalf("interrupted/goaway = %v/%v", msg.Interrupted, msg.GoAway)
	}
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].ID != "call-1" || msg.ToolCalls[0].Name != "save_summary" {
		t.Fatalf("tool calls = %#v", msg.ToolCalls)
	}
	if len(msg.ToolCallCancellationIDs) != 1 || msg.ToolCallCancellationIDs[0] != "call-2" {
		t.Fatalf("tool cancellations = %#v", msg.ToolCallCancellationIDs)
	}
	if !msg.SessionResumable {
		t.Fatalf("SessionResumable = false, want true")
	}
	for _, want := range []live.LiveEventType{
		live.LiveEventSessionEnd,
		live.LiveEventSessionResumable,
		live.LiveEventInterrupted,
		live.LiveEventInputFinal,
		live.LiveEventOutputAudio,
		live.LiveEventOutputText,
		live.LiveEventToolCall,
		live.LiveEventTurnEnd,
	} {
		if !live.EventTypesContain(msg.EventTypes, want) {
			t.Fatalf("EventTypes = %v, missing %s", msg.EventTypes, want)
		}
	}
	if msg.ProviderMetadata["provider_event"] != "live.server_message" {
		t.Fatalf("ProviderMetadata = %#v", msg.ProviderMetadata)
	}
	if got := provider.resume.Get(); got != "resume-1" {
		t.Fatalf("resume handle = %q, want resume-1", got)
	}
}

func TestGeminiLiveCloseClearsSessionAndResumeHandle(t *testing.T) {
	mockSession := &mockGeminiSession{}
	provider := New()
	provider.session = mockSession
	provider.client = &genai.Client{}
	provider.resume.Set("resume-1")

	if provider.Name() != "gemini-live" {
		t.Fatalf("provider name = %q", provider.Name())
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if mockSession.closeCount != 1 {
		t.Fatalf("session close count = %d, want 1", mockSession.closeCount)
	}
	if provider.session != nil || provider.client != nil {
		t.Fatalf("provider should clear session/client on close")
	}
	if got := provider.resume.Get(); got != "" {
		t.Fatalf("resume handle after close = %q, want empty", got)
	}
}

func TestGeminiLiveSendToolResponseUsesSchedulingAndContinuation(t *testing.T) {
	mockSession := &mockGeminiSession{}
	provider := &Provider{session: mockSession}
	willContinue := true

	if err := provider.SendToolResponse(live.ToolResponse{
		ID:           "call-1",
		Name:         "save_summary",
		Response:     map[string]any{"output": "stored"},
		Scheduling:   live.ToolResponseSchedulingInterrupt,
		WillContinue: &willContinue,
	}); err != nil {
		t.Fatalf("send tool response: %v", err)
	}

	if len(mockSession.toolResponses) != 1 {
		t.Fatalf("expected 1 tool response, got %d", len(mockSession.toolResponses))
	}
	if len(mockSession.toolResponses[0].FunctionResponses) != 1 {
		t.Fatalf("function responses = %#v", mockSession.toolResponses[0].FunctionResponses)
	}

	response := mockSession.toolResponses[0].FunctionResponses[0]
	if response.Scheduling != genai.FunctionResponseSchedulingInterrupt {
		t.Fatalf("scheduling = %q, want %q", response.Scheduling, genai.FunctionResponseSchedulingInterrupt)
	}
	if response.WillContinue == nil || !*response.WillContinue {
		t.Fatalf("willContinue = %#v, want true", response.WillContinue)
	}
}

func TestShouldTryFallback(t *testing.T) {
	tests := []struct {
		name     string
		primary  string
		fallback string
		want     bool
	}{
		{
			name:     "fallback is empty",
			primary:  "gemini-3.1-flash-live-preview",
			fallback: "",
			want:     false,
		},
		{
			name:     "fallback is whitespace",
			primary:  "gemini-3.1-flash-live-preview",
			fallback: "   ",
			want:     false,
		},
		{
			name:     "fallback equals primary",
			primary:  "gemini-3.1-flash-live-preview",
			fallback: "gemini-3.1-flash-live-preview",
			want:     false,
		},
		{
			name:     "fallback equals primary with surrounding whitespace",
			primary:  "gemini-3.1-flash-live-preview",
			fallback: "  gemini-3.1-flash-live-preview  ",
			want:     false,
		},
		{
			name:     "GA fallback distinct from preview primary",
			primary:  "gemini-3.1-flash-live-preview",
			fallback: "gemini-2.5-flash-native-audio-preview-12-2025",
			want:     true,
		},
		{
			name:     "primary blank but fallback set",
			primary:  "",
			fallback: "gemini-2.5-flash-native-audio-preview-12-2025",
			want:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := live.ShouldTryFallback(tt.primary, tt.fallback); got != tt.want {
				t.Fatalf("live.ShouldTryFallback(%q, %q) = %v; want %v",
					tt.primary, tt.fallback, got, tt.want)
			}
		})
	}
}
