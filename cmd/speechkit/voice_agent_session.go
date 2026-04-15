package main

import (
	"context"
	"fmt"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

func prepareVoiceAgentSession(state *appState) *voiceagent.Session {
	if state == nil {
		return nil
	}

	geminiProvider := voiceagent.NewGeminiLive()
	return voiceagent.NewSession(geminiProvider, voiceagent.Callbacks{
		OnStateChange: func(vaState voiceagent.State) {
			state.addLog(fmt.Sprintf("Voice Agent: %s", vaState), "info")
			state.updatePrompterState(string(vaState))
		},
		OnAudio: func(audioData []byte) {
			state.writeVoiceAgentAudio(audioData)
		},
		OnText: func(text string) {
			state.showAssistBubble(text)
		},
		OnError: func(err error) {
			state.addLog(fmt.Sprintf("Voice Agent error: %v", err), "error")
		},
		OnInputTranscript: func(text string, done bool) {
			state.sendPrompterMessage("user", text, done)
		},
		OnOutputTranscript: func(text string, done bool) {
			state.sendPrompterMessage("assistant", text, done)
		},
		OnToolCall: func(call voiceagent.ToolCall) {
			state.addLog(fmt.Sprintf("Voice Agent tool requested: %s", call.Name), "warn")
			if state.voiceAgentSession != nil {
				if err := state.voiceAgentSession.SendToolResponse(voiceagent.ToolResponse{
					ID:   call.ID,
					Name: call.Name,
					Response: map[string]any{
						"error": "tool not configured in desktop host",
					},
				}); err != nil {
					state.addLog(fmt.Sprintf("Voice Agent tool response failed: %v", err), "error")
				}
			}
		},
		OnToolCallCancellation: func(ids []string) {
			state.addLog(fmt.Sprintf("Voice Agent tool calls cancelled: %v", ids), "info")
		},
		OnInterrupted: func() {
			state.interruptVoiceAgentStream(context.Background())
			state.sendPrompterMessage("system", "[interrupted]", true)
		},
		OnSessionEnd: func() {
			state.stopVoiceAgentStream()
			state.hidePrompterWindow()
			state.addLog("Voice Agent session ended", "info")
		},
	})
}
