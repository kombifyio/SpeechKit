package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type recordingStatusReader interface {
	IsRecording() bool
}

type desktopInputController struct {
	commands          speechkit.CommandBus
	recording         recordingStatusReader
	state             *appState
	hotkeyEvents      <-chan hotkey.Event
	silenceAutoStop   <-chan struct{}
	autoStartInterval time.Duration
	voiceAgentSession *voiceagent.Session
	voiceAgentConfig  *config.VoiceAgentConfig
	cfg               *config.Config
	audioCapturer     audioFrameStreamer
}

type audioFrameStreamer interface {
	SetPCMHandler(fn func([]byte))
	Start() error
}

func (c desktopInputController) Run(ctx context.Context) {
	interval := c.autoStartInterval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	autoStartTicker := time.NewTicker(interval)
	defer autoStartTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.silenceAutoStop:
			c.handleSilenceAutoStop(ctx)
		case <-autoStartTicker.C:
			c.handleAutoStartTick(ctx)
		case evt, ok := <-c.hotkeyEvents:
			if !ok {
				return
			}
			c.handleHotkey(ctx, evt)
		}
	}
}

func (c desktopInputController) handleSilenceAutoStop(ctx context.Context) {
	if c.recording == nil || !c.recording.IsRecording() {
		return
	}
	c.log("Quick Capture: silence detected, auto-stopping", "info")
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStopDictation,
		Metadata: map[string]string{
			"label": "Quick Capture",
		},
	}, "Stop")
}

func (c desktopInputController) handleAutoStartTick(ctx context.Context) {
	if c.recording != nil && c.recording.IsRecording() {
		return
	}
	if c.state == nil || !c.state.consumeQuickCaptureAutoStart() {
		return
	}
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStartDictation,
		Metadata: map[string]string{
			"label": "Quick Capture: auto-recording started (speak now, auto-stops on silence)",
		},
	}, "Start")
}

func (c desktopInputController) handleHotkey(ctx context.Context, evt hotkey.Event) {
	if evt.Binding == "agent" {
		// Voice Agent mode: toggle-based (not PTT).
		if c.cfg != nil && c.cfg.General.AgentMode == "voice_agent" && c.voiceAgentSession != nil {
			if evt.Type == hotkey.EventKeyDown {
				c.toggleVoiceAgent(ctx)
			}
			return
		}

		// Voice Agent configured but session unavailable: warn user.
		if c.cfg != nil && c.cfg.General.AgentMode == "voice_agent" && c.voiceAgentSession == nil {
			if evt.Type == hotkey.EventKeyDown {
				c.log("Voice Agent not available — check API key and config", "error")
			}
			return
		}

		// Assist mode: set mode to agent, then fall through to PTT recording.
		if evt.Type == hotkey.EventKeyDown {
			c.dispatch(ctx, speechkit.Command{
				Type: speechkit.CommandSetActiveMode,
				Metadata: map[string]string{
					"mode": "agent",
				},
			}, "Set mode")
		}
		// Fall through to PTT logic below (don't return).
	}
	if evt.Binding == "dictate" && evt.Type == hotkey.EventKeyDown {
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandSetActiveMode,
			Metadata: map[string]string{
				"mode": "dictate",
			},
		}, "Set mode")
	}
	switch evt.Type {
	case hotkey.EventKeyDown:
		if c.recording != nil && c.recording.IsRecording() {
			c.dispatch(ctx, speechkit.Command{
				Type: speechkit.CommandStopDictation,
				Metadata: map[string]string{
					"label": "Captured",
				},
			}, "Stop")
			return
		}
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandStartDictation,
			Metadata: map[string]string{
				"label": "Recording started",
			},
		}, "Start")
	case hotkey.EventKeyUp:
		if c.recording == nil || !c.recording.IsRecording() {
			return
		}
		if c.state != nil && c.state.quickCaptureModeActive() {
			return
		}
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandStopDictation,
			Metadata: map[string]string{
				"label": "Captured",
			},
		}, "Stop")
	}
}

func (c desktopInputController) dispatch(ctx context.Context, command speechkit.Command, action string) {
	if c.commands == nil {
		return
	}
	if err := c.commands.Dispatch(ctx, command); err != nil {
		c.log(fmt.Sprintf("%s error: %v", action, err), "error")
	}
}

func (c desktopInputController) toggleVoiceAgent(ctx context.Context) {
	if c.voiceAgentSession == nil {
		c.log("Voice Agent session not initialized — check config and API key", "error")
		return
	}

	if c.voiceAgentSession.CurrentState() != voiceagent.StateInactive {
		c.log("Voice Agent: deactivating", "info")
		// Remove mic handler before stopping to avoid sending to a closing session.
		if c.audioCapturer != nil {
			c.audioCapturer.SetPCMHandler(nil)
		}
		c.voiceAgentSession.Stop()
		if c.state != nil {
			c.state.updatePrompterState("inactive")
			c.state.stopVoiceAgentStream()
		}
		return
	}

	c.log("Voice Agent: activating", "info")

	// Show prompter window if configured.
	if c.state != nil && c.voiceAgentConfig != nil && c.voiceAgentConfig.ShowPrompter {
		c.state.clearPrompterMessages()
		c.state.showPrompterWindow()
	}

	// Resolve API key.
	apiKey := ""
	if c.cfg != nil {
		apiKey = config.ResolveSecret(c.cfg.Providers.Google.APIKeyEnv)
	}
	if apiKey == "" {
		c.log("Voice Agent: no Google API key configured", "error")
		return
	}

	model := "gemini-2.5-flash-native-audio-preview-12-2025"
	voice := "Kore"
	locale := "en"
	if c.voiceAgentConfig != nil {
		if c.voiceAgentConfig.Model != "" {
			model = c.voiceAgentConfig.Model
		}
		if c.voiceAgentConfig.Voice != "" {
			voice = c.voiceAgentConfig.Voice
		}
	}
	if c.cfg != nil && c.cfg.General.Language != "" {
		locale = c.cfg.General.Language
	}

	idleCfg := voiceagent.DefaultIdleConfig()
	if c.voiceAgentConfig != nil {
		if c.voiceAgentConfig.ReminderAfterIdleSec > 0 {
			idleCfg.ReminderAfter = time.Duration(c.voiceAgentConfig.ReminderAfterIdleSec) * time.Second
		}
		if c.voiceAgentConfig.DeactivateAfterIdleSec > 0 {
			idleCfg.DeactivateAfter = time.Duration(c.voiceAgentConfig.DeactivateAfterIdleSec) * time.Second
		}
	}

	go func() {
		vocabularyHint := ""
		instruction := ""
		policies := voiceagent.LivePolicies{}
		if c.cfg != nil {
			vocabularyHint = buildVoiceAgentVocabularyHint(parseVocabularyDictionary(c.cfg.Vocabulary.Dictionary))
		}
		if c.voiceAgentConfig != nil {
			instruction = c.voiceAgentConfig.Instruction
			policies = voiceagent.LivePolicies{
				EnableInputAudioTranscription:  c.voiceAgentConfig.EnableInputTranscript,
				EnableOutputAudioTranscription: c.voiceAgentConfig.EnableOutputTranscript,
				EnableAffectiveDialog:          c.voiceAgentConfig.EnableAffectiveDialog,
				Thinking: voiceagent.ThinkingPolicy{
					Enabled:         c.voiceAgentConfig.ThinkingEnabled,
					IncludeThoughts: c.voiceAgentConfig.IncludeThoughts,
					ThinkingBudget:  int32(c.voiceAgentConfig.ThinkingBudget),
					ThinkingLevel:   voiceagent.ThinkingLevel(c.voiceAgentConfig.ThinkingLevel),
				},
				ContextCompression: voiceagent.ContextCompressionPolicy{
					Enabled:       c.voiceAgentConfig.ContextCompressionEnabled,
					TriggerTokens: c.voiceAgentConfig.ContextCompressionTriggerTokens,
					TargetTokens:  c.voiceAgentConfig.ContextCompressionTargetTokens,
				},
				ActivityDetection: voiceagent.ActivityDetectionPolicy{
					Automatic:         c.voiceAgentConfig.AutomaticActivityDetection,
					StartSensitivity:  voiceagent.StartSensitivity(c.voiceAgentConfig.VADStartSensitivity),
					EndSensitivity:    voiceagent.EndSensitivity(c.voiceAgentConfig.VADEndSensitivity),
					PrefixPaddingMs:   int32(c.voiceAgentConfig.VADPrefixPaddingMs),
					SilenceDurationMs: int32(c.voiceAgentConfig.VADSilenceDurationMs),
					ActivityHandling:  voiceagent.ActivityHandling(c.voiceAgentConfig.ActivityHandling),
					TurnCoverage:      voiceagent.TurnCoverage(c.voiceAgentConfig.TurnCoverage),
				},
			}
		}

		// Start streaming audio output before connecting.
		if c.state != nil {
			c.state.startVoiceAgentStream(ctx)
		}

		if err := c.voiceAgentSession.Start(ctx, voiceagent.LiveConfig{
			Model:          model,
			APIKey:         apiKey,
			Voice:          voice,
			Locale:         locale,
			Instruction:    instruction,
			VocabularyHint: vocabularyHint,
			Policies:       policies,
		}, idleCfg); err != nil {
			c.log(fmt.Sprintf("Voice Agent: start failed: %v", err), "error")
			if c.state != nil {
				c.state.stopVoiceAgentStream()
			}
			return
		}

		// Stream mic audio to the Voice Agent session via the shared audio capturer.
		c.log("Voice Agent: streaming audio", "info")
		if c.audioCapturer != nil {
			c.audioCapturer.SetPCMHandler(func(frame []byte) {
				if c.voiceAgentSession.CurrentState() == voiceagent.StateInactive {
					return
				}
				_ = c.voiceAgentSession.SendAudio(frame)
			})

			// Start the mic capture so frames flow to the handler.
			if err := c.audioCapturer.Start(); err != nil {
				c.log(fmt.Sprintf("Voice Agent: mic capture start failed: %v", err), "error")
			}
		}
	}()
}

func (c desktopInputController) log(message, kind string) {
	if c.state == nil || message == "" {
		return
	}
	c.state.addLog(message, kind)
}
