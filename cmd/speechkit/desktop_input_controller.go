package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
	"github.com/kombifyio/SpeechKit/internal/voicebehavior"
	"github.com/kombifyio/SpeechKit/internal/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type recordingStatusReader interface {
	IsRecording() bool
}

type desktopInputController struct {
	commands            speechkit.CommandBus
	recording           recordingStatusReader
	state               *appState
	hotkeyEvents        <-chan hotkey.Event
	silenceAutoStop     <-chan struct{}
	autoStartInterval   time.Duration
	voiceAgentSession   *voiceagent.Session
	voiceAgentConfig    *config.VoiceAgentConfig
	cfg                 *config.Config
	installState        *config.InstallState
	sttRouter           *router.Router
	audioCapturer       audioFrameStreamer
	voiceAgentEchoGuard *voiceAgentEchoGuard
	// showDashboard, when non-nil, lets the controller open the
	// dashboard window from inside a hotkey/preflight path — used when
	// onboarding is incomplete so the user sees the wizard instead of
	// silently failing.
	showDashboard func(source string)
}

type audioFrameStreamer interface {
	SetPCMHandler(fn func([]byte))
	Start() error
}

const (
	voiceAgentHoldToTalkReleasePollInterval = 50 * time.Millisecond
	// voiceAgentHoldToTalkReleaseMaxWait caps how long the post-release
	// grace period can run even when the user configures a longer value.
	// Beyond ~30 s a "hold released" session is almost certainly stuck on a
	// model that never produced a Done message; force-deactivate so the user
	// can start a new turn without restarting the app.
	voiceAgentHoldToTalkReleaseMaxWait      = 30 * time.Second
	voiceAgentHoldToTalkReleaseGraceDefault = 10 * time.Second
)

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
	label := c.quickNoteRecordingLabel()
	c.log(fmt.Sprintf("%s: silence detected, auto-stopping", label), "info")
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStopDictation,
		Metadata: map[string]string{
			"label": label,
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
	if c.gateOnOnboardingPending() {
		return
	}
	if !c.preflightCaptureStart(ctx, modeDictate) {
		return
	}
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStartDictation,
		Metadata: map[string]string{
			"label": fmt.Sprintf("%s: auto-recording started (speak now, auto-stops on silence)", c.quickNoteRecordingLabel()),
		},
	}, "Start")
}

func (c desktopInputController) quickNoteRecordingLabel() string {
	if c.state == nil {
		return "Quick Capture"
	}
	noteContext := c.state.currentQuickNoteContext()
	if noteContext.enabled && !noteContext.captureMode {
		return "Quick Note"
	}
	return "Quick Capture"
}

func (c desktopInputController) handleHotkey(ctx context.Context, evt hotkey.Event) {
	// Pre-Wails-Run guard: the keyboard hook + the per-mode hotkey
	// managers start polling immediately, so a key that's down when the
	// app is launched can fire EventKeyDown before app.Run() has set up
	// the Wails main thread. Every downstream UI path (overlay positioning,
	// dashboard show, assist bubble) then dereferences a nil Screen /
	// nil dispatcher and panics. Drop hotkey events until the app is
	// fully started; see beads kombify-SpeechKit-0s6.
	if c.state != nil && !c.state.isAppStarted() {
		return
	}
	if evt.Type == hotkey.EventKeyDown && c.gateOnOnboardingPending() {
		return
	}
	switch binding := c.resolveHotkeyBinding(evt.Binding); binding {
	case modeVoiceAgent:
		if !c.modeCurrentlyEnabled(modeVoiceAgent) {
			c.log("Voice Agent hotkey ignored because the mode is disabled.", "info")
			return
		}
		c.routeVoiceAgentHotkey(ctx, evt)
	case modeAssist:
		if !c.modeCurrentlyEnabled(modeAssist) {
			c.log("Assist hotkey ignored because the mode is disabled.", "info")
			return
		}
		c.logModeRoute(modeAssist, evt.Binding, c.hotkeyBehavior(modeAssist), evt.Type)
		c.routeCaptureHotkey(ctx, modeAssist, evt)
	case modeDictate:
		if !c.modeCurrentlyEnabled(modeDictate) {
			c.log("Dictation hotkey ignored because the mode is disabled.", "info")
			return
		}
		c.routeCaptureHotkey(ctx, modeDictate, evt)
	default:
		return
	}
}

// gateOnOnboardingPending returns true when SpeechKit is in a fresh
// local install with the onboarding wizard not yet completed. The
// caller must abort mode activation when this returns true. The user
// is shown a one-line hint and the dashboard is opened so they can
// finish the wizard.
//
// We deliberately gate ALL three modes (Dictate, Assist, Voice Agent)
// uniformly even when a cloud provider would otherwise satisfy the
// per-mode prerequisites — until onboarding is acknowledged the user
// has no way to know SpeechKit is listening on their global hotkeys.
//
// The showDashboard call is gated on state.isAppStarted() because a
// hotkey arriving before app.Run() — phantom keyboard state from the
// launching context, an auto-start ticker firing during init — would
// otherwise reach application.InvokeSync against an uninitialised
// dispatcher and panic (beads kombify-SpeechKit-0s6). Pre-Run the
// preflight hint still runs (it touches no main-thread UI), and the
// first-run dashboard popup is owned by scheduleFirstRunOnboarding
// which polls state.dashboardReadyForAutoOpen() — so the user still
// gets the wizard, just routed through the safe path.
func (c desktopInputController) gateOnOnboardingPending() bool {
	if c.installState == nil {
		return false
	}
	if c.installState.Mode != config.InstallModeLocal {
		return false
	}
	if c.installState.SetupDone {
		return false
	}
	c.presentPreflightHint("SpeechKit setup is not finished. Complete the onboarding before activating modes.")
	if c.showDashboard != nil && c.state.isAppStarted() {
		c.showDashboard("setup-required")
	}
	return true
}

func (c desktopInputController) handleHoldToTalk(ctx context.Context, evt hotkey.Event) {
	switch evt.Type {
	case hotkey.EventKeyDown:
		if c.recording != nil && c.recording.IsRecording() {
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
		if (c.recording == nil || !c.recording.IsRecording()) && !c.preflightCaptureStart(ctx, mode) {
			return
		}
		c.primeOverlayForCapture(mode)
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
		c.handleHoldToTalk(ctx, evt)
	}
}

func (c desktopInputController) primeOverlayForCapture(mode string) {
	if c.state == nil {
		return
	}
	c.state.primeOverlayForCapture(mode)
}

func (c desktopInputController) routeVoiceAgentHotkey(ctx context.Context, evt hotkey.Event) {
	// Wake-word-origin events are routed independently of the configured
	// Hold-to-Talk / Toggle behaviour. There is no KeyUp counterpart for a
	// synthesized wake-word press, so a Hold-to-Talk Voice-Agent setting
	// would otherwise leave the session running until the 15 min idle
	// timeout. Wake-word events always go through the Session abstraction
	// (no Pipeline-Fallback detour) and rely on the attached
	// AutoEndPolicy for termination.
	if evt.Source == hotkey.EventSourceWakeword {
		if evt.Type != hotkey.EventKeyDown {
			// Wake-word dispatcher does not emit KeyUp by default. Defensive
			// guard for callers that opt into SyntheticRelease.
			return
		}
		c.logVoiceAgentRoute(evt.Binding, "wake-word", "info", evt.Type)
		if msg := c.voiceAgentStartBlockedReason(); msg != "" { //nolint:contextcheck // realtime readiness is independent of caller ctx.
			// No voice-agent provider configured/activated — drop the policy
			// the wake-word handler parked in the slot, show a preflight
			// hint instead of falling through into the dictation-style
			// fallback path.
			if policy := c.state.clearWakewordSessionPolicy(); policy != nil {
				policy.Close()
			}
			c.presentPreflightHint(msg)
			return
		}
		c.activateVoiceAgentWakewordSession(ctx)
		return
	}

	if c.shouldUseVoiceAgentPipelineFallback() {
		c.logVoiceAgentRoute(evt.Binding, "pipeline fallback", "info", evt.Type)
		c.routeCaptureHotkey(ctx, modeVoiceAgent, evt)
		return
	}

	behavior := c.hotkeyBehavior(modeVoiceAgent)
	switch behavior {
	case config.HotkeyBehaviorHoldToTalk:
		switch evt.Type {
		case hotkey.EventKeyDown:
			c.logVoiceAgentRoute(evt.Binding, "hold-to-talk", "info", evt.Type)
			session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
			if session == nil || session.CurrentState() == voiceagent.StateInactive {
				c.activateVoiceAgent(ctx)
			}
		case hotkey.EventKeyUp:
			c.releaseVoiceAgentHoldToTalk(ctx)
		}
	default:
		if evt.Type != hotkey.EventKeyDown {
			return
		}
		c.logVoiceAgentRoute(evt.Binding, "toggle", "info", evt.Type)
		c.toggleVoiceAgent(ctx)
	}
}

func (c desktopInputController) releaseVoiceAgentHoldToTalk(ctx context.Context) {
	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
	if session == nil {
		return
	}

	// Activation goroutine may still be inside session.Start — either before
	// it set StateConnecting, or already blocked on provider.Connect. In both
	// cases pulling the cancel hook unblocks the WebSocket handshake so
	// session.Start returns ctx.Err() and the activation tears itself down.
	// We do this BEFORE inspecting state because the race window between
	// activateVoiceAgent dispatching the goroutine and the goroutine reaching
	// setState(Connecting) lets a fast KeyUp see StateInactive even though
	// the activation is genuinely in flight.
	activationCancel := c.takeVoiceAgentActivationCancel()

	state := session.CurrentState()
	if state == voiceagent.StateInactive {
		if activationCancel == nil {
			// Nothing was in flight; ignore the spurious KeyUp.
			return
		}
		c.log("Voice Agent: hold released before connect — cancelling activation", "info")
		activationCancel()
		// Drop the microphone synchronously so the next user gesture sees a
		// clean controller state even if the activation goroutine is still
		// unwinding (it will also call tearDownVoiceAgentAudioCapture, which
		// is idempotent — SetPCMHandler(nil) over a nil handler and
		// audioSender.Stop() under the once-guard are both safe to repeat).
		if c.audioCapturer != nil {
			c.audioCapturer.SetPCMHandler(nil)
		}
		if c.state != nil {
			c.state.stopVoiceAgentAudioSender()
		}
		// deactivateVoiceAgentWithReason short-circuits when the session is
		// already Inactive (which it is in this branch) so the call just hides
		// the prompter if appropriate. It is kept for symmetry — when the
		// race resolves the other way (state already moved to Connecting by
		// the time we got here) the deactivate call below is what does the
		// real cleanup.
		c.deactivateVoiceAgentWithReason(ctx, true, "hold-to-talk released before connect")
		return
	}

	// Hold-to-talk semantics: when the user releases the shortcut while the
	// WebSocket handshake is still running, the realtime session never had a
	// chance to receive audio. There is nothing to wait for, so we abort the
	// activation immediately instead of stalling on the grace timer.
	if state == voiceagent.StateConnecting {
		c.log("Voice Agent: hold released during connect — cancelling activation", "info")
		if activationCancel != nil {
			activationCancel()
		}
		c.deactivateVoiceAgentWithReason(ctx, true, "hold-to-talk released during connect")
		return
	}

	// Activation already succeeded; the cancel hook would have been cleared,
	// but if a stale one survived (e.g. test contexts), drop it now.
	if activationCancel != nil {
		// no-op: session has moved past Start; the goroutine no longer reads ctx.
		_ = activationCancel
	}

	c.log("Voice Agent: hold-to-talk released", "info")
	if c.audioCapturer != nil {
		c.audioCapturer.SetPCMHandler(nil)
	}
	if c.state != nil {
		c.state.stopVoiceAgentAudioSender()
		c.state.updatePrompterActivity("user", 0)
	}
	if err := session.EndAudioStream(); err != nil {
		c.log(fmt.Sprintf("Voice Agent: audio stream end failed: %v", err), "warn")
		c.deactivateVoiceAgentWithReason(ctx, true, "hold-to-talk release")
		return
	}

	go c.deactivateVoiceAgentAfterHoldToTalkTurn(ctx, session)
}

// voiceAgentHoldReleaseGrace returns the time a hold-to-talk release is held
// open after EndAudioStream so the model can deliver its reply. The default
// (10 s) is generous enough for chatty agents but short enough that a stuck
// session does not appear "still on" to the user. Settings >0 are honoured up
// to voiceAgentHoldToTalkReleaseMaxWait; 0 or negative falls back to the
// default.
func (c desktopInputController) voiceAgentHoldReleaseGrace() time.Duration {
	grace := voiceAgentHoldToTalkReleaseGraceDefault
	if c.voiceAgentConfig != nil && c.voiceAgentConfig.HoldReleaseGraceSec > 0 {
		grace = time.Duration(c.voiceAgentConfig.HoldReleaseGraceSec) * time.Second
	}
	if grace > voiceAgentHoldToTalkReleaseMaxWait {
		grace = voiceAgentHoldToTalkReleaseMaxWait
	}
	return grace
}

func (c desktopInputController) deactivateVoiceAgentAfterHoldToTalkTurn(ctx context.Context, session *voiceagent.Session) {
	if session == nil {
		return
	}
	grace := c.voiceAgentHoldReleaseGrace()
	ticker := time.NewTicker(voiceAgentHoldToTalkReleasePollInterval)
	defer ticker.Stop()
	timer := time.NewTimer(grace)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// Grace period elapsed: forcibly tear the session down even if the
			// model is mid-reply. This is what makes hold-to-talk feel like a
			// walkie-talkie — releasing the key ends the conversation within a
			// bounded window regardless of how chatty the agent is.
			c.deactivateVoiceAgentWithReason(ctx, true, "hold-to-talk release grace expired")
			return
		case <-ticker.C:
			if session.CurrentState() == voiceagent.StateInactive {
				// Session already deactivated by another path (idle timer,
				// error path, user pressing the shortcut again). Nothing left
				// to do.
				return
			}
		}
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
	return config.HotkeyBehaviorHoldToTalk
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

func (c desktopInputController) modeCurrentlyEnabled(mode string) bool {
	if c.cfg != nil {
		switch mode {
		case modeDictate:
			if strings.TrimSpace(c.cfg.General.DictateHotkey) != "" {
				return c.cfg.General.DictateEnabled
			}
		case modeAssist:
			if strings.TrimSpace(c.cfg.General.AssistHotkey) != "" {
				return c.cfg.General.AssistEnabled
			}
		case modeVoiceAgent:
			if strings.TrimSpace(c.cfg.General.VoiceAgentHotkey) != "" {
				return c.cfg.General.VoiceAgentEnabled
			}
		}
	}
	if c.state == nil {
		return true
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	switch mode {
	case modeDictate:
		if strings.TrimSpace(c.state.dictateHotkey) != "" {
			return c.state.dictateEnabled
		}
	case modeAssist:
		if strings.TrimSpace(c.state.assistHotkey) != "" {
			return c.state.assistEnabled
		}
	case modeVoiceAgent:
		if strings.TrimSpace(c.state.voiceAgentHotkey) != "" {
			return c.state.voiceAgentEnabled
		}
	default:
		return false
	}
	return true
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

func (c desktopInputController) takeVoiceAgentActivationCancel() context.CancelFunc {
	if c.state == nil {
		return nil
	}
	return c.state.takeVoiceAgentActivationCancel()
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
		c.log("Voice Agent session not initialized — check config and API key", "error")
		return
	}

	if session.CurrentState() != voiceagent.StateInactive {
		if c.state != nil && c.voiceAgentConfig != nil && c.voiceAgentConfig.ShowPrompter {
			c.state.showPrompterWindowForMode(modeVoiceAgent, false)
		}
		return
	}

	c.activateVoiceAgent(ctx)
}

func (c desktopInputController) activateVoiceAgent(ctx context.Context) {
	plan, ok := c.prepareVoiceAgentActivation(ctx)
	if !ok {
		return
	}
	// Arm the microphone synchronously so audio captured during the WebSocket
	// handshake is queued in the audio sender's channel and replayed in-order
	// once the realtime session is ready. Without this the user perceives a
	// 300-1500 ms dead window where the hotkey "does nothing" until they
	// release — see beads kombify-SpeechKit hold-to-talk activation lag bug.
	audioSender, ok := c.armVoiceAgentAudioCapture(plan)
	if !ok {
		// Mic capture failed; surface the error and bail. tearDown was
		// already performed inside armVoiceAgentAudioCapture so the sender
		// and capturer state are clean.
		c.deactivateVoiceAgentWithReason(ctx, true, "mic capture start failed")
		return
	}
	// Install a cancel hook so a hold-to-talk release that lands before the
	// activation goroutine has transitioned the session out of StateInactive
	// can still tear it down. Without this the goroutine would happily finish
	// connecting after the user already let go. In contexts without an
	// appState (rare; only legacy tests) skip the wrapping context to avoid
	// leaking the cancel function — those tests cannot use the release-before-
	// connect abort path anyway.
	activationCtx := ctx
	if c.state != nil {
		var cancel context.CancelFunc
		activationCtx, cancel = context.WithCancel(ctx)
		c.state.setVoiceAgentActivationCancel(cancel)
	}

	// Hotkey-toggle Voice Agent gets the same silence-cutoff + exit-phrase
	// auto-end policy as the wake-word path. Without this the toggle-mode
	// VA session would stay open forever after the user stopped speaking.
	// Hold-to-talk does not need it either way — the KeyUp tears the
	// session down — but installing the policy is harmless there: any
	// NotifyActivity from the PCM handler resets the timer well before
	// SilenceCutoff fires.
	if c.state != nil && c.cfg != nil &&
		c.hotkeyBehavior(modeVoiceAgent) == config.HotkeyBehaviorToggle {
		policy := wakeword.NewAutoEndPolicy(
			autoEndConfigFromTOML(c.cfg.Wakeword.AutoEnd),
			slog.Default(),
		)
		c.state.setWakewordSessionPolicy(policy)
		policy.Start()
		go c.watchWakewordAutoEnd(ctx, policy)
	}

	go c.startVoiceAgentSession(activationCtx, plan, audioSender)
}

func voiceAgentProfileID(cfg *config.VoiceAgentConfig) string {
	if cfg == nil {
		return voiceagentprofile.DefaultID
	}
	return cfg.AgentProfileID
}

func voiceAgentSequenceID(cfg *config.VoiceAgentConfig) string {
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.AgentSequenceID)
}

func applyVoiceAgentProfileSelection(profileID, sequenceID, voice, frameworkPrompt string) (string, string, *voiceagent.WorkflowConfig) {
	profileID = voiceagentprofile.NormalizeID(profileID)
	profile, ok := voiceagentprofile.Resolve(profileID)
	if !ok {
		return voice, frameworkPrompt, nil
	}
	if profileID != voiceagentprofile.DefaultID {
		if selectedVoice := strings.TrimSpace(profile.Voice); selectedVoice != "" {
			voice = selectedVoice
		}
		if selectedPrompt := strings.TrimSpace(profile.FrameworkPrompt); selectedPrompt != "" {
			frameworkPrompt = selectedPrompt
		}
	}
	if sequenceID = strings.TrimSpace(sequenceID); sequenceID == "" {
		sequenceID = strings.TrimSpace(profile.DefaultSequenceID)
	}
	workflow := builtInWorkflowConfig(profileID, sequenceID, frameworkPrompt)
	return voice, frameworkPrompt, workflow
}

func builtInWorkflowConfig(profileID, sequenceID, basePrompt string) *voiceagent.WorkflowConfig {
	if strings.TrimSpace(sequenceID) == "" {
		return nil
	}
	catalog := voicebehavior.BuiltInCatalog()
	if _, err := catalog.Resolve(profileID, "", sequenceID, 0); err != nil {
		return nil
	}
	sequence, ok := catalog.Sequence(sequenceID)
	if !ok || len(sequence.Steps) == 0 {
		return nil
	}
	workflow := &voiceagent.WorkflowConfig{
		SequenceID: sequence.ID,
		Completion: sequence.Completion,
		MaxTurns:   sequence.MaxTurns,
		BasePrompt: strings.TrimSpace(basePrompt),
		Steps:      make([]voiceagent.WorkflowStep, 0, len(sequence.Steps)),
	}
	for _, step := range sequence.Steps {
		workflow.Steps = append(workflow.Steps, voiceagent.WorkflowStep{
			ID:           step.ID,
			Instruction:  step.Instruction,
			ExitCriteria: step.ExitCriteria,
			RequireTools: append([]string(nil), step.RequireTools...),
			MaxTurns:     step.MaxTurns,
		})
	}
	return workflow
}

func (c desktopInputController) deactivateVoiceAgent(ctx context.Context, keepPrompterVisible bool) {
	c.deactivateVoiceAgentWithReason(ctx, keepPrompterVisible, "manual control")
}

func (c desktopInputController) deactivateVoiceAgentWithReason(ctx context.Context, keepPrompterVisible bool, reason string) {
	session := c.currentVoiceAgentSession() //nolint:contextcheck // getter, no context needed
	if session == nil || session.CurrentState() == voiceagent.StateInactive {
		if c.audioCapturer != nil {
			c.audioCapturer.SetPCMHandler(nil)
		}
		if c.state != nil {
			if activationCancel := c.state.takeVoiceAgentActivationCancel(); activationCancel != nil {
				activationCancel()
			}
			c.state.stopVoiceAgentAudioSender()
			c.state.stopVoiceAgentStream()
			if policy := c.state.clearWakewordSessionPolicy(); policy != nil {
				policy.Close()
			}
		}
		if c.state != nil && !keepPrompterVisible {
			c.state.hidePrompterWindow()
		}
		return
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "unspecified"
	}
	c.log(fmt.Sprintf("Voice Agent: deactivating (%s)", reason), "info")
	if c.audioCapturer != nil {
		c.audioCapturer.SetPCMHandler(nil)
	}
	if c.state != nil {
		c.state.resetVoiceAgentEchoGuard()
	} else if c.voiceAgentEchoGuard != nil {
		c.voiceAgentEchoGuard.reset()
	}

	// Capture session identity before Stop() so we can emit the end event.
	// session.Stop() does not fire OnSessionEnd, so the audit event would be
	// missing for user-initiated deactivation without this explicit emit.
	// Clearing voiceAgentSessionID here also prevents the idle-path branch in
	// OnStateChange from double-emitting (it no-ops when session ID is empty).
	var auditSessionID string
	var auditSessionStart time.Time
	if c.state != nil {
		c.state.mu.Lock()
		auditSessionID = c.state.voiceAgentSessionID
		auditSessionStart = c.state.voiceAgentSessionStart
		c.state.voiceAgentSessionID = ""
		c.state.voiceAgentSessionStart = time.Time{}
		c.state.voiceAgentTerminatedBy = ""
		c.state.mu.Unlock()
	}

	session.Stop()
	emitVoiceAgentSessionEnd(ctx, auditSessionID, auditSessionStart, "user")

	if c.state != nil {
		c.state.stopVoiceAgentAudioSender()
		c.state.finishVoiceAgentSessionSummary(ctx, c.cfg)
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
		c.deactivateVoiceAgentWithReason(ctx, true, "prompter close")
		c.state.clearPrompterMessages()
		c.state.updatePrompterState("inactive")
		c.state.hidePrompterWindow()
	default:
		c.state.hidePrompterWindow()
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

func (c desktopInputController) preflightCaptureStart(ctx context.Context, mode string) bool {
	if msg := c.captureStartBlockedReason(ctx, mode); msg != "" {
		c.presentPreflightHint(msg)
		return false
	}
	if mode == modeVoiceAgent && c.shouldUseVoiceAgentPipelineFallback() {
		if msg := c.voiceAgentPipelineStartBlockedReason(); msg != "" {
			c.presentPreflightHint(msg)
			return false
		}
	}
	if mode == modeAssist {
		if msg := c.assistStartBlockedReason(); msg != "" {
			c.presentPreflightHint(msg)
			return false
		}
	}
	return true
}

func (c desktopInputController) captureStartBlockedReason(ctx context.Context, mode string) string {
	r := c.currentSTTRouter()
	if r == nil && c.installState == nil {
		return ""
	}
	if c.serverDictationReady() {
		return ""
	}
	strategy := router.StrategyDynamic
	if c.cfg != nil && c.cfg.Routing.Strategy != "" {
		strategy = router.Strategy(c.cfg.Routing.Strategy)
	}

	localReady := c.localSTTReady(ctx)
	cloudReady := c.hasConfiguredCloudSTTProvider(r)

	switch strategy {
	case router.StrategyLocalOnly:
		if localReady {
			return ""
		}
		return c.localSTTBlockedReason(ctx, mode)
	case router.StrategyCloudOnly:
		if cloudReady {
			return ""
		}
		return c.cloudSTTBlockedReason(mode)
	default:
		if localReady || cloudReady {
			return ""
		}
		if msg := c.localSTTBlockedReason(ctx, mode); msg != "" {
			return msg
		}
		if msg := c.cloudSTTBlockedReason(mode); msg != "" {
			return msg
		}
		return fmt.Sprintf("%s can't start because no speech provider is configured. Open Settings > STT.", modeDisplayName(mode))
	}
}

func (c desktopInputController) assistStartBlockedReason() string {
	if c.state == nil {
		return ""
	}
	if c.currentSTTRouter() == nil && c.installState == nil {
		return ""
	}
	c.state.mu.Lock()
	assistPipeline := c.state.assistPipeline
	serverAssistReady := c.state.serverDelegates.hasAssist()
	c.state.mu.Unlock()
	if serverAssistReady || (assistPipeline != nil && assistPipeline.HasDirectReplyModel()) {
		return ""
	}
	return "Assist can't start because no ready Assist model is available. Open Settings > Assist Mode and download a local model or choose a provider integration."
}

func (c desktopInputController) voiceAgentStartBlockedReason() string { //nolint:contextcheck // realtime session readiness is independent from a request lifecycle
	if c.shouldUseVoiceAgentPipelineFallback() {
		return c.voiceAgentPipelineStartBlockedReason()
	}
	if strings.TrimSpace(c.voiceAgentAPIKey()) == "" {
		return "Voice Agent can't start because no realtime provider/API key is configured. Open Settings > Voice Agent."
	}
	if c.currentVoiceAgentSession() == nil { //nolint:contextcheck // getter, no context needed
		return "Voice Agent can't start because the realtime session is not ready. Open Settings > Voice Agent."
	}
	return ""
}

func (c desktopInputController) voiceAgentPipelineStartBlockedReason() string {
	if c.state == nil {
		return ""
	}
	c.state.mu.Lock()
	agentFlowReady := c.state.agentFlow != nil
	c.state.mu.Unlock()
	if agentFlowReady {
		return ""
	}
	return "Voice Agent can't start because no Voice Agent model is configured. Open Settings > Models and select a Voice Agent model."
}

func (c desktopInputController) localSTTBlockedReason(ctx context.Context, mode string) string {
	if c.installState != nil && c.installState.Mode == config.InstallModeLocal && !c.installState.SetupDone {
		return fmt.Sprintf("%s can't start because no local speech model is configured yet. Open Settings > STT and download/select a model.", modeDisplayName(mode))
	}

	r := c.currentSTTRouter()
	if r == nil || r.Local() == nil {
		return ""
	}

	if provider, ok := r.Local().(localProviderStarter); ok {
		status := provider.VerifyInstallation()
		if !status.BinaryFound {
			return fmt.Sprintf("%s can't start because the local speech runtime is missing. Reinstall SpeechKit or repair Local STT in Settings.", modeDisplayName(mode))
		}
		if !status.ModelFound {
			return fmt.Sprintf("%s can't start because no local speech model is configured. Open Settings > STT and download/select a model.", modeDisplayName(mode))
		}
		if !provider.IsReady() {
			return fmt.Sprintf("%s is waiting for Local STT to finish starting. Try again in a moment.", modeDisplayName(mode))
		}
		return ""
	}

	if !providerReady(ctx, r.Local()) {
		return fmt.Sprintf("%s can't start because Local STT is unavailable right now. Check Settings > STT.", modeDisplayName(mode))
	}
	return ""
}

func (c desktopInputController) cloudSTTBlockedReason(mode string) string {
	if c.cfg != nil {
		if hint := missingProviderHint(c.cfg); hint != "" {
			return fmt.Sprintf("%s can't start yet. %s", modeDisplayName(mode), hint)
		}
	}
	if c.cfg == nil {
		return fmt.Sprintf("%s can't start because no speech provider is configured. Open Settings > STT.", modeDisplayName(mode))
	}
	return fmt.Sprintf("%s can't start because no cloud speech provider is configured. Open Settings > Provider.", modeDisplayName(mode))
}

func (c desktopInputController) serverDictationReady() bool {
	if c.state == nil {
		return false
	}
	delegates := currentServerDelegates(c.state)
	return delegates.hasDictation()
}

func (c desktopInputController) localSTTReady(ctx context.Context) bool {
	r := c.currentSTTRouter()
	if r == nil || r.Local() == nil {
		return false
	}
	return providerReady(ctx, r.Local())
}

func (c desktopInputController) hasConfiguredCloudSTTProvider(r *router.Router) bool {
	if r == nil {
		return false
	}
	for _, providerName := range r.AvailableProviders() {
		if providerName != "local" {
			return true
		}
	}
	return false
}

func (c desktopInputController) currentSTTRouter() *router.Router {
	if c.sttRouter != nil {
		return c.sttRouter
	}
	if c.state == nil {
		return nil
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	return c.state.sttRouter
}

func (c desktopInputController) currentVoiceAgentEchoGuard() *voiceAgentEchoGuard {
	if c.state != nil {
		return c.state.currentVoiceAgentEchoGuard()
	}
	return c.voiceAgentEchoGuard
}

func (c desktopInputController) presentPreflightHint(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	c.log(message, "warn")
	if c.state != nil {
		c.state.showAssistBubble(message)
	}
}

func modeDisplayName(mode string) string {
	switch normalizeRuntimeMode(mode, "") {
	case modeAssist:
		return "Assist"
	case modeVoiceAgent:
		return "Voice Agent"
	default:
		return "Dictation"
	}
}
