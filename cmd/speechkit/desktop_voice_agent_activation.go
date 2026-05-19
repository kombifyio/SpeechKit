package main

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kombifyio/SpeechKit/cmd/speechkit/internal/transcription"
	"github.com/kombifyio/SpeechKit/internal/auditlog"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type voiceAgentActivationPlan struct {
	session   *voiceagent.Session
	echoGuard *voiceAgentEchoGuard
	start     voiceAgentStartConfig
	// preSession is set while the activation goroutine is still inside
	// session.Start (typically while Gemini Live establishes its WebSocket).
	// The microphone handler consults this flag so frames captured during the
	// handshake are forwarded to the audio sender's channel even though the
	// session state has not yet transitioned out of StateInactive.
	preSession *atomic.Bool
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
	preSession := &atomic.Bool{}
	preSession.Store(true)
	return voiceAgentActivationPlan{
		session:    session,
		echoGuard:  echoGuard,
		start:      start,
		preSession: preSession,
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

func (c desktopInputController) startVoiceAgentSession(ctx context.Context, plan voiceAgentActivationPlan, audioSender *voiceAgentAudioSender) {
	liveCfg := c.voiceAgentLiveConfig(plan.session, plan.start)
	if c.state != nil {
		c.state.startVoiceAgentStream(ctx)
	}

	sessionStart := time.Now()
	err := plan.session.Start(ctx, liveCfg, plan.start.IdleConfig)
	// The activation cancel hook is consumed regardless of outcome; from here
	// on Stop() drives the teardown path.
	if c.state != nil {
		c.state.clearVoiceAgentActivationCancel()
	}
	if err != nil {
		c.log(fmt.Sprintf("Voice Agent: start failed: %v", err), "error")
		c.tearDownVoiceAgentAudioCapture(plan, audioSender)
		if c.state != nil {
			c.state.stopVoiceAgentStream()
		}
		return
	}

	providerName := plan.session.ProviderName()
	if providerName == "" {
		providerName = "unknown" // TODO Phase 1: surface provider name from session config
	}
	sessionID := fmt.Sprintf("va-%d", sessionStart.UnixNano())
	_ = auditlog.AppendEvent(ctx, auditlog.Record{
		Event: auditlog.EventVoiceAgentSessionStart,
		Resource: map[string]any{
			"session_id": sessionID,
			"provider":   providerName,
			"transport":  "in-process",
		},
	})

	if c.state != nil {
		c.state.mu.Lock()
		c.state.voiceAgentSessionID = sessionID
		c.state.voiceAgentSessionStart = sessionStart
		// Defensive: clear stale terminated_by from any prior session so the
		// idle-path guard (terminated_by == "") in OnStateChange works on the
		// very first state transition of this fresh session.
		c.state.voiceAgentTerminatedBy = ""
		c.state.mu.Unlock()
	}

	c.activateVoiceAgentAudioSender(ctx, plan, audioSender)
	c.log("Voice Agent: streaming audio", "info")
}

// activateVoiceAgentAudioSender starts the audio sender goroutine that drains
// the in-flight frame channel into the live session. Frames captured while
// session.Start was establishing the WebSocket are already queued in the
// channel; the goroutine forwards them in order, then keeps streaming the
// real-time mic input. Clearing the preSession flag flips the microphone
// handler back to the normal voiceAgentMicFrameAllowed gate so the echo guard
// and Speaking-state mute take effect once the AI starts to respond.
func (c desktopInputController) activateVoiceAgentAudioSender(ctx context.Context, plan voiceAgentActivationPlan, audioSender *voiceAgentAudioSender) {
	if audioSender == nil {
		return
	}
	sendErrorLogged := false
	audioSender.onSendError = c.voiceAgentAudioSendErrorHandler(&sendErrorLogged)
	if plan.preSession != nil {
		plan.preSession.Store(false)
	}
	audioSender.Start(ctx)
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

	region := ""
	if c.cfg != nil {
		region = c.cfg.Providers.Google.Region
	}
	return voiceagent.LiveConfig{
		Model:            start.Model,
		APIKey:           start.APIKey,
		Voice:            voice,
		Locale:           start.Locale,
		Region:           region,
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

// armVoiceAgentAudioCapture wires the microphone to a freshly-created audio
// sender BEFORE the realtime session is connected. The sender's drain
// goroutine is deliberately NOT started yet — frames captured during the
// WebSocket handshake queue up in the sender's bounded channel and are
// replayed in order once activateVoiceAgentAudioSender runs. This is what
// keeps hold-to-talk feeling instantaneous: the user's earliest utterance
// reaches the model even though Connect took a few hundred milliseconds.
//
// In test contexts that pass a nil audioCapturer this still returns a working
// sender so the activation goroutine can proceed; tests drive frames into the
// sender directly.
func (c desktopInputController) armVoiceAgentAudioCapture(plan voiceAgentActivationPlan) (*voiceAgentAudioSender, bool) {
	audioSender := newVoiceAgentAudioSender(plan.session, defaultVoiceAgentAudioQueueSize)
	if c.state != nil {
		c.state.setVoiceAgentAudioSender(audioSender)
	}
	if c.audioCapturer == nil {
		return audioSender, true
	}

	c.audioCapturer.SetPCMHandler(func(frame []byte) {
		if plan.preSession != nil && plan.preSession.Load() {
			// Pre-session window: session state is still StateInactive or has
			// just transitioned to StateConnecting. Buffer the frame in the
			// sender's channel; voiceAgentMicFrameAllowed would otherwise drop
			// it because it treats StateInactive as muted.
			_ = audioSender.Enqueue(frame)
			return
		}
		if !voiceAgentMicFrameAllowed(plan.session.CurrentState(), plan.echoGuard) {
			return
		}
		_ = audioSender.Enqueue(frame)
	})

	if err := c.audioCapturer.Start(); err != nil {
		c.log(fmt.Sprintf("Voice Agent: mic capture start failed: %v", err), "error")
		c.audioCapturer.SetPCMHandler(nil)
		if c.state != nil {
			c.state.stopVoiceAgentAudioSender()
		}
		audioSender.Stop()
		return nil, false
	}
	return audioSender, true
}

// tearDownVoiceAgentAudioCapture unwinds an armVoiceAgentAudioCapture when the
// activation aborts (session.Start failed, or the user released the hotkey
// while the WebSocket was still connecting). It clears the mic handler so the
// capturer stops feeding the now-dead sender, drops the pending sender, and
// flips preSession back to false so any straggler frame from the capturer
// callback thread takes the muted path instead of enqueueing.
func (c desktopInputController) tearDownVoiceAgentAudioCapture(plan voiceAgentActivationPlan, sender *voiceAgentAudioSender) {
	if plan.preSession != nil {
		plan.preSession.Store(false)
	}
	if c.audioCapturer != nil {
		c.audioCapturer.SetPCMHandler(nil)
	}
	if c.state != nil {
		c.state.stopVoiceAgentAudioSender()
	}
	if sender != nil {
		sender.Stop()
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
