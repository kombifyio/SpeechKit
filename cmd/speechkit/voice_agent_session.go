package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

// emitVoiceAgentSessionEnd emits the voiceagent.session.end audit event with
// the given terminatedBy reason. No-op when sessionID is empty (i.e. the
// session never started or its ID was already cleared by an earlier path).
func emitVoiceAgentSessionEnd(ctx context.Context, sessionID string, startTime time.Time, terminatedBy string) {
	if sessionID == "" {
		return
	}
	_ = auditlog.AppendEvent(ctx, auditlog.Record{
		Event: auditlog.EventVoiceAgentSessionEnd,
		Resource: map[string]any{
			"session_id":       sessionID,
			"duration_seconds": time.Since(startTime).Seconds(),
			"terminated_by":    terminatedBy,
		},
	})
}

func prepareVoiceAgentSession(state *appState, cfg *config.Config) *voiceagent.Session {
	if state == nil {
		return nil
	}

	provider := selectVoiceAgentProvider(state, cfg)
	if provider == nil {
		// Fall back to the in-process Gemini Live provider — preserves
		// pre-0.26 behaviour when the server delegate is unavailable.
		provider = voiceagent.NewGeminiLive()
	}
	return voiceagent.NewSession(provider, buildVoiceAgentCallbacks(state, cfg))
}

// selectVoiceAgentProvider returns the server-delegated LiveProvider when
// the user opted Voice Agent into server-side execution; nil otherwise.
// nil tells the caller to keep the in-process provider.
func selectVoiceAgentProvider(state *appState, cfg *config.Config) voiceagent.LiveProvider {
	if state == nil {
		return nil
	}
	state.mu.Lock()
	delegates := state.serverDelegates
	state.mu.Unlock()
	if !delegates.hasVoiceAgent() {
		return nil
	}
	return delegates.newVoiceAgentProvider(cfg)
}

// handleVoiceAgentStateInactive detects an idle-driven session termination and
// emits the idle session.end audit event. Called from the OnStateChange
// callback when the voice agent state transitions to Inactive.
//
// Guard: (sessionID != "" && terminatedBy == "") ensures the user-path and
// error-path don't double-emit — those paths clear voiceAgentSessionID or set
// voiceAgentTerminatedBy before triggering state transitions.
func handleVoiceAgentStateInactive(ctx context.Context, state *appState) {
	state.mu.Lock()
	sessionID := state.voiceAgentSessionID
	startTime := state.voiceAgentSessionStart
	terminatedBy := state.voiceAgentTerminatedBy
	if sessionID == "" || terminatedBy != "" {
		state.mu.Unlock()
		return
	}
	state.voiceAgentSessionID = ""
	state.voiceAgentSessionStart = time.Time{}
	state.mu.Unlock()
	emitVoiceAgentSessionEnd(ctx, sessionID, startTime, "idle")
}

func buildVoiceAgentCallbacks(state *appState, cfg *config.Config) voiceagent.Callbacks {
	return voiceagent.Callbacks{
		OnStateChange: func(vaState voiceagent.State) {
			state.addLog(fmt.Sprintf("Voice Agent: %s", vaState), "info")
			state.updateOverlayForVoiceAgentState(string(vaState))
			state.updatePrompterState(string(vaState))
			if vaState != voiceagent.StateListening {
				state.updatePrompterActivity("user", 0)
			}
			if vaState != voiceagent.StateSpeaking {
				state.updatePrompterActivity("assistant", 0)
			}
			if vaState == voiceagent.StateInactive {
				handleVoiceAgentStateInactive(context.Background(), state) //nolint:contextcheck // state-change callback has no caller context
			}
		},
		OnAudio: func(audioData []byte) {
			state.markVoiceAgentAssistantAudio()
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
			state.recordVoiceAgentDialogTurn("assistant", text, true)
		},
		OnError: func(err error) {
			state.addLog(fmt.Sprintf("Voice Agent error: %v", err), "error")
			state.sendPrompterMessage("assistant", friendlyConversationError(modeVoiceAgent, err), true)
			state.updatePrompterState("error")
			state.updatePrompterActivity("user", 0)
			state.updatePrompterActivity("assistant", 0)
			// Pre-set the termination reason so OnSessionEnd (called by cleanupOnError
			// immediately after OnError returns) emits the correct terminated_by value.
			state.mu.Lock()
			state.voiceAgentTerminatedBy = "error"
			state.mu.Unlock()
		},
		OnInputTranscript: func(text string, done bool) {
			state.sendPrompterMessage("user", text, done)
			state.recordVoiceAgentDialogTurn("user", text, done)
		},
		OnOutputTranscript: func(text string, done bool) {
			state.sendPrompterMessage("assistant", text, done)
			state.recordVoiceAgentDialogTurn("assistant", text, done)
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
			state.stopVoiceAgentAudioSender()
			state.stopVoiceAgentStream()
			state.finishVoiceAgentSessionSummary(context.Background(), cfg) //nolint:contextcheck // event callback has no caller context
			state.updatePrompterActivity("user", 0)
			state.updatePrompterActivity("assistant", 0)
			state.addLog("Voice Agent session ended", "info")

			// OnSessionEnd is called exclusively from cleanupOnError (error / GoAway /
			// reconnect-failure paths). The reason was pre-set by OnError; fall back to
			// "error" if no explicit reason was recorded (e.g. GoAway without a preceding
			// error callback).
			state.mu.Lock()
			sessionID := state.voiceAgentSessionID
			sessionStart := state.voiceAgentSessionStart
			terminatedBy := state.voiceAgentTerminatedBy
			if terminatedBy == "" {
				terminatedBy = "error"
			}
			state.voiceAgentSessionID = ""
			state.voiceAgentSessionStart = time.Time{}
			state.voiceAgentTerminatedBy = ""
			state.mu.Unlock()

			emitVoiceAgentSessionEnd(context.Background(), sessionID, sessionStart, terminatedBy) //nolint:contextcheck // callback has no caller context
		},
	}
}
