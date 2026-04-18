package main

import (
	"context"
	"fmt"
	"strings"
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
	switch binding := c.resolveHotkeyBinding(evt.Binding); binding {
	case modeVoiceAgent:
		if c.currentVoiceAgentSession() != nil { //nolint:contextcheck // currentVoiceAgentSession is a stateful getter, not a context-passing call
			c.routeVoiceAgentHotkey(ctx, evt)
			return
		}
		if evt.Type == hotkey.EventKeyDown {
			c.log("Voice Agent not available â€” check API key and config", "error")
		}
		return
	case modeAssist:
		c.logModeRoute(modeAssist, evt.Binding, c.hotkeyBehavior(modeAssist), evt.Type)
		c.routeCaptureHotkey(ctx, modeAssist, evt)
	case modeDictate:
		c.routeCaptureHotkey(ctx, modeDictate, evt)
	default:
		return
	}
}

func (c desktopInputController) handlePushToTalk(ctx context.Context, evt hotkey.Event) {
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

func (c desktopInputController) handleToggleCapture(ctx context.Context, evt hotkey.Event) {
	if evt.Type != hotkey.EventKeyDown {
		return
	}
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
}

func (c desktopInputController) routeCaptureHotkey(ctx context.Context, mode string, evt hotkey.Event) {
	if evt.Type == hotkey.EventKeyDown {
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandSetActiveMode,
			Metadata: map[string]string{
				"mode": mode,
			},
		}, "Set mode")
	}

	switch c.hotkeyBehavior(mode) {
	case config.HotkeyBehaviorToggle:
		c.handleToggleCapture(ctx, evt)
	default:
		c.handlePushToTalk(ctx, evt)
	}
}

func (c desktopInputController) routeVoiceAgentHotkey(ctx context.Context, evt hotkey.Event) {
	behavior := c.hotkeyBehavior(modeVoiceAgent)
	switch behavior {
	case config.HotkeyBehaviorPushToTalk:
		switch evt.Type {
		case hotkey.EventKeyDown:
			c.logVoiceAgentRoute(evt.Binding, "push-to-talk", "info", evt.Type)
			session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
			if session == nil || session.CurrentState() == voiceagent.StateInactive {
				c.toggleVoiceAgent(ctx)
			}
		case hotkey.EventKeyUp:
			session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
			if session == nil || session.CurrentState() == voiceagent.StateInactive {
				return
			}
			c.toggleVoiceAgent(ctx)
		}
	default:
		if evt.Type != hotkey.EventKeyDown {
			return
		}
		c.logVoiceAgentRoute(evt.Binding, "toggle", "info", evt.Type)
		c.toggleVoiceAgent(ctx)
	}
}

func (c desktopInputController) hotkeyBehavior(mode string) string {
	if c.cfg == nil {
		return defaultHotkeyBehavior(mode)
	}

	switch mode {
	case modeAssist:
		return config.NormalizeHotkeyBehavior(
			c.cfg.General.AssistHotkeyBehavior,
			config.NormalizeHotkeyBehavior(c.cfg.General.HotkeyMode, defaultHotkeyBehavior(mode)),
		)
	case modeVoiceAgent:
		return config.NormalizeHotkeyBehavior(
			c.cfg.General.VoiceAgentHotkeyBehavior,
			config.NormalizeHotkeyBehavior(c.cfg.General.HotkeyMode, defaultHotkeyBehavior(mode)),
		)
	default:
		return config.NormalizeHotkeyBehavior(
			c.cfg.General.DictateHotkeyBehavior,
			config.NormalizeHotkeyBehavior(c.cfg.General.HotkeyMode, defaultHotkeyBehavior(mode)),
		)
	}
}

func defaultHotkeyBehavior(mode string) string {
	return config.HotkeyBehaviorPushToTalk
}

func (c desktopInputController) logModeRoute(mode, binding, behavior string, evtType hotkey.EventType) {
	if evtType != hotkey.EventKeyDown {
		return
	}
	if mode == modeAssist {
		if binding == modeAgent {
			c.log(fmt.Sprintf("Agent hotkey routed to Assist %s", hotkeyBehaviorLabel(behavior)), "info")
			return
		}
		c.log(fmt.Sprintf("Assist hotkey routed to Assist %s", hotkeyBehaviorLabel(behavior)), "info")
	}
}

func (c desktopInputController) logVoiceAgentRoute(binding, route, level string, evtType hotkey.EventType) {
	if evtType != hotkey.EventKeyDown {
		return
	}
	if binding == modeAgent {
		c.log(fmt.Sprintf("Agent hotkey routed to Voice Agent %s", route), level)
		return
	}
	c.log(fmt.Sprintf("Voice Agent hotkey routed to Voice Agent %s", route), level)
}

func hotkeyBehaviorLabel(behavior string) string {
	if behavior == config.HotkeyBehaviorToggle {
		return "toggle"
	}
	return "capture"
}

func (c desktopInputController) resolveHotkeyBinding(binding string) string {
	trimmed := strings.TrimSpace(binding)
	if trimmed == modeAgent {
		legacyMode := modeAssist
		if c.cfg != nil {
			legacyMode = normalizeAgentMode(c.cfg.General.AgentMode)
		}
		return normalizeRuntimeMode(trimmed, legacyMode)
	}
	return normalizeRuntimeMode(trimmed, "")
}

func (c desktopInputController) voiceAgentAPIKey() string {
	if c.cfg == nil {
		return ""
	}
	return config.ResolveSecret(c.cfg.Providers.Google.APIKeyEnv)
}

func (c desktopInputController) shouldUseVoiceAgentPipelineFallback() bool {
	if c.cfg == nil || !c.cfg.VoiceAgent.PipelineFallback {
		return false
	}
	model := strings.ToLower(strings.TrimSpace(c.cfg.VoiceAgent.Model))
	if model != "" && !strings.Contains(model, "gemini") {
		return true
	}
	return c.voiceAgentAPIKey() == ""
}

func (c desktopInputController) dispatch(ctx context.Context, command speechkit.Command, action string) {
	if c.commands == nil {
		return
	}
	if err := c.commands.Dispatch(ctx, command); err != nil {
		c.log(fmt.Sprintf("%s error: %v", action, err), "error")
	}
}

func (c desktopInputController) currentVoiceAgentSession() *voiceagent.Session {
	if c.state != nil {
		c.state.mu.Lock()
		session := c.state.voiceAgentSession
		c.state.mu.Unlock()
		if session != nil {
			return session
		}

		session = prepareVoiceAgentSession(c.state, c.cfg)
		if session != nil {
			c.state.mu.Lock()
			if c.state.voiceAgentSession == nil {
				c.state.voiceAgentSession = session
			} else {
				session = c.state.voiceAgentSession
			}
			c.state.mu.Unlock()
			return session
		}
	}
	return c.voiceAgentSession
}

func (c desktopInputController) toggleVoiceAgent(ctx context.Context) {
	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
	if session == nil {
		c.log("Voice Agent session not initialized â€” check config and API key", "error")
		return
	}

	if session.CurrentState() != voiceagent.StateInactive {
		c.deactivateVoiceAgent(ctx, true)
		return
	}

	c.activateVoiceAgent(ctx)
}

func (c desktopInputController) activateVoiceAgent(ctx context.Context) {
	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
	if session == nil {
		c.log("Voice Agent session not initialized â€” check config and API key", "error")
		return
	}
	if session.CurrentState() != voiceagent.StateInactive {
		return
	}

	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandSetActiveMode,
		Metadata: map[string]string{
			"mode": modeVoiceAgent,
		},
	}, "Set mode")

	c.log("Voice Agent: activating", "info")

	// Show prompter window if configured.
	if c.state != nil && c.voiceAgentConfig != nil && c.voiceAgentConfig.ShowPrompter {
		c.state.showPrompterWindowForMode(modeVoiceAgent, true)
	}

	// Resolve API key.
	apiKey := c.voiceAgentAPIKey()
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
		frameworkPrompt := ""
		refinementPrompt := ""
		policies := voiceagent.LivePolicies{}
		if c.cfg != nil {
			vocabularyHint = buildVoiceAgentVocabularyHint(parseVocabularyDictionary(c.cfg.Vocabulary.Dictionary))
		}
		if c.voiceAgentConfig != nil {
			frameworkPrompt = strings.TrimSpace(c.voiceAgentConfig.FrameworkPrompt)
			if frameworkPrompt == "" {
				frameworkPrompt = strings.TrimSpace(c.voiceAgentConfig.Instruction)
			}
			refinementPrompt = strings.TrimSpace(c.voiceAgentConfig.RefinementPrompt)
			policies = voiceagent.LivePolicies{
				EnableInputAudioTranscription:  c.voiceAgentConfig.EnableInputTranscript,
				EnableOutputAudioTranscription: c.voiceAgentConfig.EnableOutputTranscript,
				EnableAffectiveDialog:          c.voiceAgentConfig.EnableAffectiveDialog,
				Thinking: voiceagent.ThinkingPolicy{
					Enabled:         c.voiceAgentConfig.ThinkingEnabled,
					IncludeThoughts: c.voiceAgentConfig.IncludeThoughts,
					ThinkingBudget:  int32(c.voiceAgentConfig.ThinkingBudget), //nolint:gosec // Windows API integer conversion, value fits
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
					PrefixPaddingMs:   int32(c.voiceAgentConfig.VADPrefixPaddingMs),   //nolint:gosec // Windows API integer conversion, value fits
					SilenceDurationMs: int32(c.voiceAgentConfig.VADSilenceDurationMs), //nolint:gosec // Windows API integer conversion, value fits
					ActivityHandling:  voiceagent.ActivityHandling(c.voiceAgentConfig.ActivityHandling),
					TurnCoverage:      voiceagent.TurnCoverage(c.voiceAgentConfig.TurnCoverage),
				},
			}
		}

		// Start streaming audio output before connecting.
		if c.state != nil {
			c.state.startVoiceAgentStream(ctx)
		}

		if err := session.Start(ctx, voiceagent.LiveConfig{
			Model:            model,
			APIKey:           apiKey,
			Voice:            voice,
			Locale:           locale,
			FrameworkPrompt:  frameworkPrompt,
			RefinementPrompt: refinementPrompt,
			Instruction:      frameworkPrompt,
			VocabularyHint:   vocabularyHint,
			Policies:         policies,
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
				if session.CurrentState() == voiceagent.StateInactive {
					return
				}
				_ = session.SendAudio(frame)
			})

			// Start the mic capture so frames flow to the handler.
			if err := c.audioCapturer.Start(); err != nil {
				c.log(fmt.Sprintf("Voice Agent: mic capture start failed: %v", err), "error")
			}
		}
	}()
}

func (c desktopInputController) deactivateVoiceAgent(_ context.Context, keepPrompterVisible bool) {
	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
	if session == nil || session.CurrentState() == voiceagent.StateInactive {
		if c.state != nil && !keepPrompterVisible {
			c.state.hidePrompterWindow()
		}
		return
	}

	c.log("Voice Agent: deactivating", "info")
	if c.audioCapturer != nil {
		c.audioCapturer.SetPCMHandler(nil)
	}
	session.Stop()
	if c.state != nil {
		c.state.updatePrompterActivity("user", 0)
		c.state.updatePrompterActivity("assistant", 0)
		c.state.updatePrompterState("inactive")
		c.state.stopVoiceAgentStream()
		if !keepPrompterVisible {
			c.state.hidePrompterWindow()
		}
	}
}

func (c desktopInputController) closeVoiceAgentPrompter(ctx context.Context) {
	if c.state == nil {
		return
	}

	switch c.voiceAgentCloseBehavior() {
	case config.VoiceAgentCloseBehaviorNewChat:
		c.deactivateVoiceAgent(ctx, true)
		c.state.clearPrompterMessages()
		c.state.updatePrompterState("inactive")
		c.state.hidePrompterWindow()
	default:
		c.state.minimisePrompterWindow()
	}
}

func (c desktopInputController) voiceAgentCloseBehavior() string {
	if c.voiceAgentConfig != nil {
		return config.NormalizeVoiceAgentCloseBehavior(
			c.voiceAgentConfig.CloseBehavior,
			config.VoiceAgentCloseBehaviorContinue,
		)
	}
	if c.cfg != nil {
		return config.NormalizeVoiceAgentCloseBehavior(
			c.cfg.VoiceAgent.CloseBehavior,
			config.VoiceAgentCloseBehaviorContinue,
		)
	}
	return config.VoiceAgentCloseBehaviorContinue
}

func (c desktopInputController) log(message, kind string) {
	if c.state == nil || message == "" {
		return
	}
	c.state.addLog(message, kind)
}
