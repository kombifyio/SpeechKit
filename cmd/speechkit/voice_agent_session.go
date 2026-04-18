package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

func prepareVoiceAgentSession(state *appState, cfg *config.Config) *voiceagent.Session {
	if state == nil {
		return nil
	}

	geminiProvider := voiceagent.NewGeminiLive()
	return voiceagent.NewSession(geminiProvider, buildVoiceAgentCallbacks(state, cfg))
}

func buildVoiceAgentCallbacks(state *appState, cfg *config.Config) voiceagent.Callbacks {
	return voiceagent.Callbacks{
		OnStateChange: func(vaState voiceagent.State) {
			state.addLog(fmt.Sprintf("Voice Agent: %s", vaState), "info")
			state.updatePrompterState(string(vaState))
			if vaState != voiceagent.StateListening {
				state.updatePrompterActivity("user", 0)
			}
			if vaState != voiceagent.StateSpeaking {
				state.updatePrompterActivity("assistant", 0)
			}
		},
		OnAudio: func(audioData []byte) {
			state.writeVoiceAgentAudio(audioData)
			state.updatePrompterActivity("assistant", audio.PCMLevel(audioData))
		},
		OnText: func(text string) {
			if strings.TrimSpace(text) == "" {
				return
			}
			if cfg != nil && cfg.VoiceAgent.EnableOutputTranscript {
				return
			}
			state.sendPrompterMessage("assistant", text, false)
		},
		OnError: func(err error) {
			state.addLog(fmt.Sprintf("Voice Agent error: %v", err), "error")
			state.sendPrompterMessage("assistant", friendlyConversationError(cfg, modeVoiceAgent, err), true)
			state.updatePrompterState("error")
			state.updatePrompterActivity("user", 0)
			state.updatePrompterActivity("assistant", 0)
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
			state.interruptVoiceAgentStream(context.Background()) //nolint:contextcheck // event callback has no caller context; Background() is correct
			state.updatePrompterActivity("assistant", 0)
			state.sendPrompterMessage("system", "[interrupted]", true)
		},
		OnSessionEnd: func() {
			state.stopVoiceAgentStream()
			state.updatePrompterActivity("user", 0)
			state.updatePrompterActivity("assistant", 0)
			state.addLog("Voice Agent session ended", "info")
		},
	}
}
