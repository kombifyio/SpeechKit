package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type voiceAgentActivationPlan struct {
	session   *voiceagent.Session
	echoGuard *voiceAgentEchoGuard
	start     voiceAgentStartConfig
}

type voiceAgentStartConfig struct {
	APIKey     string
	Model      string
	Voice      string
	Locale     string
	IdleConfig voiceagent.IdleConfig
}

func (c desktopInputController) prepareVoiceAgentActivation(ctx context.Context) (voiceAgentActivationPlan, bool) {
	if msg := c.voiceAgentStartBlockedReason(); msg != "" { //nolint:contextcheck // realtime session readiness is independent from a request lifecycle.
		c.presentPreflightHint(msg)
		return voiceAgentActivationPlan{}, false
	}

	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed.
	if session == nil {
		c.log("Voice Agent session not initialized — check config and API key", "error")
		return voiceAgentActivationPlan{}, false
	}
	if session.CurrentState() != voiceagent.StateInactive {
		return voiceAgentActivationPlan{}, false
	}

	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandSetActiveMode,
		Metadata: map[string]string{
			"mode": modeVoiceAgent,
		},
	}, "Set mode")

	c.log("Voice Agent: activating", "info")
	echoGuard := c.currentVoiceAgentEchoGuard()
	if echoGuard != nil {
		echoGuard.reset()
	}
	if c.state != nil {
		c.state.resetVoiceAgentSessionSummary()
	}
	c.showVoiceAgentConnectingPrompter()

	start, ok := c.voiceAgentStartConfig()
	if !ok {
		return voiceAgentActivationPlan{}, false
	}
	return voiceAgentActivationPlan{
		session:   session,
		echoGuard: echoGuard,
		start:     start,
	}, true
}

func (c desktopInputController) showVoiceAgentConnectingPrompter() {
	if c.state == nil || c.voiceAgentConfig == nil || !c.voiceAgentConfig.ShowPrompter {
		return
	}
	c.state.showPrompterWindowForMode(modeVoiceAgent, true)
	c.state.updatePrompterState(string(voiceagent.StateConnecting))
}

func (c desktopInputController) voiceAgentStartConfig() (voiceAgentStartConfig, bool) {
	apiKey := c.voiceAgentAPIKey()
	if apiKey == "" {
		c.log("Voice Agent: no Google API key configured", "error")
		return voiceAgentStartConfig{}, false
	}
	model, voice, locale := c.voiceAgentModelVoiceLocale()
	return voiceAgentStartConfig{
		APIKey:     apiKey,
		Model:      model,
		Voice:      voice,
		Locale:     locale,
		IdleConfig: c.voiceAgentIdleConfig(),
	}, true
}

func (c desktopInputController) voiceAgentModelVoiceLocale() (string, string, string) {
	model := "gemini-3.1-flash-live-preview"
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
	return model, voice, locale
}

func (c desktopInputController) voiceAgentIdleConfig() voiceagent.IdleConfig {
	idleCfg := voiceagent.DefaultIdleConfig()
	if c.voiceAgentConfig == nil {
		return idleCfg
	}
	if c.voiceAgentConfig.ReminderAfterIdleSec > 0 {
		idleCfg.ReminderAfter = time.Duration(c.voiceAgentConfig.ReminderAfterIdleSec) * time.Second
	}
	if c.voiceAgentConfig.DeactivateAfterIdleSec > 0 {
		idleCfg.DeactivateAfter = time.Duration(c.voiceAgentConfig.DeactivateAfterIdleSec) * time.Second
	}
	return idleCfg
}

func (c desktopInputController) startVoiceAgentSession(ctx context.Context, plan voiceAgentActivationPlan) {
	liveCfg := c.voiceAgentLiveConfig(plan.session, plan.start)
	if c.state != nil {
		c.state.startVoiceAgentStream(ctx)
	}

	if err := plan.session.Start(ctx, liveCfg, plan.start.IdleConfig); err != nil {
		c.log(fmt.Sprintf("Voice Agent: start failed: %v", err), "error")
		if c.state != nil {
			c.state.stopVoiceAgentStream()
		}
		return
	}

	c.log("Voice Agent: streaming audio", "info")
	c.bindVoiceAgentAudio(ctx, plan.session, plan.echoGuard)
}

func (c desktopInputController) voiceAgentLiveConfig(session *voiceagent.Session, start voiceAgentStartConfig) voiceagent.LiveConfig {
	frameworkPrompt, refinementPrompt := c.voiceAgentPrompts()
	voice, frameworkPrompt, workflow := applyVoiceAgentProfileSelection(
		voiceAgentProfileID(c.voiceAgentConfig),
		voiceAgentSequenceID(c.voiceAgentConfig),
		start.Voice,
		frameworkPrompt,
	)
	if session.ProviderName() == "server-voiceagent" {
		workflow = nil
	}

	return voiceagent.LiveConfig{
		Model:            start.Model,
		APIKey:           start.APIKey,
		Voice:            voice,
		Locale:           start.Locale,
		FrameworkPrompt:  frameworkPrompt,
		RefinementPrompt: refinementPrompt,
		Instruction:      frameworkPrompt,
		VocabularyHint:   c.voiceAgentVocabularyHint(),
		Policies:         c.voiceAgentLivePolicies(),
		Workflow:         workflow,
	}
}

func (c desktopInputController) voiceAgentVocabularyHint() string {
	if c.cfg == nil {
		return ""
	}
	return transcription.BuildVoiceAgentVocabularyHint(transcription.ParseVocabularyDictionary(c.cfg.Vocabulary.Dictionary))
}

func (c desktopInputController) voiceAgentPrompts() (string, string) {
	if c.voiceAgentConfig == nil {
		return "", ""
	}
	frameworkPrompt := strings.TrimSpace(c.voiceAgentConfig.FrameworkPrompt)
	if frameworkPrompt == "" {
		frameworkPrompt = strings.TrimSpace(c.voiceAgentConfig.Instruction)
	}
	return frameworkPrompt, strings.TrimSpace(c.voiceAgentConfig.RefinementPrompt)
}

func (c desktopInputController) voiceAgentLivePolicies() voiceagent.LivePolicies {
	if c.voiceAgentConfig == nil {
		return voiceagent.LivePolicies{}
	}
	return voiceagent.LivePolicies{
		EnableInputAudioTranscription:  c.voiceAgentConfig.EnableInputTranscript,
		EnableOutputAudioTranscription: c.voiceAgentConfig.EnableOutputTranscript,
		EnableAffectiveDialog:          c.voiceAgentConfig.EnableAffectiveDialog,
		Thinking: voiceagent.ThinkingPolicy{
			Enabled:         c.voiceAgentConfig.ThinkingEnabled,
			IncludeThoughts: c.voiceAgentConfig.IncludeThoughts,
			ThinkingBudget:  int32(c.voiceAgentConfig.ThinkingBudget), // #nosec G115 -- config normalization bounds Gemini thinking budgets before use.
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
			PrefixPaddingMs:   int32(c.voiceAgentConfig.VADPrefixPaddingMs),   // #nosec G115 -- VAD millisecond settings are bounded by config normalization.
			SilenceDurationMs: int32(c.voiceAgentConfig.VADSilenceDurationMs), // #nosec G115 -- VAD millisecond settings are bounded by config normalization.
			ActivityHandling:  voiceagent.ActivityHandling(c.voiceAgentConfig.ActivityHandling),
			TurnCoverage:      voiceagent.TurnCoverage(c.voiceAgentConfig.TurnCoverage),
		},
	}
}

func (c desktopInputController) bindVoiceAgentAudio(ctx context.Context, session *voiceagent.Session, echoGuard *voiceAgentEchoGuard) {
	if c.audioCapturer == nil {
		return
	}

	audioSender := newVoiceAgentAudioSender(session, defaultVoiceAgentAudioQueueSize)
	sendErrorLogged := false
	audioSender.onSendError = c.voiceAgentAudioSendErrorHandler(&sendErrorLogged)
	audioSender.Start(ctx)
	if c.state != nil {
		c.state.setVoiceAgentAudioSender(audioSender)
	}
	c.audioCapturer.SetPCMHandler(func(frame []byte) {
		if !voiceAgentMicFrameAllowed(session.CurrentState(), echoGuard) {
			return
		}
		_ = audioSender.Enqueue(frame)
	})

	if err := c.audioCapturer.Start(); err != nil {
		audioSender.Stop()
		c.audioCapturer.SetPCMHandler(nil)
		if c.state != nil {
			c.state.stopVoiceAgentAudioSender()
		}
		c.log(fmt.Sprintf("Voice Agent: mic capture start failed: %v", err), "error")
	}
}

func (c desktopInputController) voiceAgentAudioSendErrorHandler(sendErrorLogged *bool) func(error) {
	return func(err error) {
		if err == nil || *sendErrorLogged {
			return
		}
		*sendErrorLogged = true
		c.log(fmt.Sprintf("Voice Agent: audio send failed: %v", err), "warn")
		if c.state != nil {
			c.state.sendPrompterMessage("system", "Voice Agent audio stream needs attention. Restart the Voice Agent if the next turn is not picked up.", true)
		}
	}
}
