//go:build windows && cgo

// Local Voice Agent transport: the Box talks directly to the realtime
// provider (Deepgram Voice Agent, Gemini Live, ...); no speechkit-server is
// needed. Function calls (home_assistant) run client-side through the
// toolbridge; the kombify AI Gateway can serve as the brain of the Deepgram
// agent through BYO think ([voice_agent].think_endpoint_url).
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
	livedeepgram "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/local"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

type localVoiceAgentRuntime struct {
	cfg      *Config
	audio    *AudioIO
	boxLink  *BoxLink
	provider *local.Provider

	mu       sync.Mutex
	startMu  sync.Mutex // serializes ONLY the session setup (never take under r.mu)
	playMu   sync.Mutex // serializes answer playback (turn versus idle reminder)
	started  bool
	turnDone chan struct{}

	responseMu    sync.Mutex
	responseAudio map[uint64]*bytes.Buffer
	authority     *haTurnGate
	authoritySTT  stt.STTProvider
}

func newLocalVoiceAgentRuntime(cfg *Config, audio *AudioIO, boxLink *BoxLink, authoritySTT stt.STTProvider) (*localVoiceAgentRuntime, error) {
	if authoritySTT == nil || !localAuthoritySTTProvider(authoritySTT.Name()) {
		return nil, fmt.Errorf("voice_agent: local Whisper host transcript authority is required before realtime playback can start")
	}
	r := &localVoiceAgentRuntime{
		cfg:           cfg,
		audio:         audio,
		boxLink:       boxLink,
		responseAudio: make(map[uint64]*bytes.Buffer),
		authoritySTT:  authoritySTT,
	}

	registry := agentkit.NewRegistry()
	ha, err := registerHomeAssistantTool(registry, cfg, "kbx:"+boxSessionSerial())
	if err != nil {
		return nil, err
	}
	r.authority = newHATurnGate(ha, cfg.VoiceAgent.Locale)

	idle := live.DefaultIdleConfig()
	// $4.50/h of Deepgram connection time: the idle teardown is cost control.
	idle.ReminderAfter = 90 * time.Second
	idle.DeactivateAfter = 3 * time.Minute
	if cfg.VoiceAgent.IdleReminderSec > 0 {
		idle.ReminderAfter = time.Duration(cfg.VoiceAgent.IdleReminderSec) * time.Second
	}
	if cfg.VoiceAgent.IdleDeactivateSec > 0 {
		idle.DeactivateAfter = time.Duration(cfg.VoiceAgent.IdleDeactivateSec) * time.Second
	}

	provider, err := local.New(local.Options{
		Live: live.LiveConfig{
			Provider:        cfg.VoiceAgent.Provider,
			Model:           cfg.VoiceAgent.Model,
			Voice:           cfg.VoiceAgent.Voice,
			Locale:          cfg.VoiceAgent.Locale,
			FrameworkPrompt: withHomeAssistantAuthority(cfg.VoiceAgent.SystemPrompt),
			APIKey:          resolveCompanionSecret(liveProviderKeyEnv(cfg.VoiceAgent.Provider)),
		},
		Idle:  idle,
		Tools: registry,
		Hooks: agentkit.LifecycleHooks{
			OnSessionStart: func(_ context.Context, sc agentkit.SessionContext) {
				r.authority.beginSession(sc.SessionID)
			},
			AuthorizeToolCall: func(_ context.Context, sc agentkit.SessionContext, call agentkit.ToolCall) (map[string]any, error) {
				return r.authority.authorizeToolCall(sc.SessionID, call)
			},
			OnToolResult: func(_ context.Context, sc agentkit.SessionContext, call agentkit.ToolCall, response agentkit.ToolResponse) {
				r.authority.observeToolResult(sc.SessionID, call, response)
			},
			OnSessionEnd: func(_ context.Context, sc agentkit.SessionContext) {
				r.authority.endSession(sc.SessionID)
			},
		},
		Callbacks: live.Callbacks{
			OnStateChange: func(state live.State) {
				log.Printf("[voice_agent] state=%s", state)
				r.onState(state)
			},
			OnAudio: func(chunk []byte) {
				generation, allowed := r.authority.observeAudio()
				if allowed {
					r.appendResponseAudio(generation, chunk)
					// Close the observe→append race with timeout/session retirement:
					// either the generation remains active or the just-written bytes
					// are deterministically discarded.
					if !r.authority.acceptsAudioGeneration(generation) {
						r.discardResponseAudio(generation)
					}
				}
			},
			OnOutputTranscript: func(text string, done bool) {
				r.authority.observeOutputTranscript(text, done)
				if done && strings.TrimSpace(text) != "" {
					log.Printf("[agent] %q", text)
				}
			},
			OnInputTranscript: func(text string, done bool) {
				r.authority.observeInputTranscript(text, done)
				if done && strings.TrimSpace(text) != "" {
					log.Printf("[user] %q", text)
				}
			},
			OnHostPrompt: r.onHostPrompt,
			OnInterrupted: func() {
				// Full barge-in (playback flush) comes with the PlaybackStream
				// rework; until then only make it visible.
				log.Printf("[voice_agent] interrupted (barge-in)")
			},
			OnError: func(err error) { log.Printf("[voice_agent] %v", err) },
		},
		Factory: r.providerFactory,
	})
	if err != nil {
		return nil, err
	}
	r.provider = provider
	return r, nil
}

// providerFactory extends the default factory with the BYO-think wiring: when
// [voice_agent].think_endpoint_url points at the kombify AI Gateway, the
// Deepgram agent is configured with the gateway as its brain.
func (r *localVoiceAgentRuntime) providerFactory(cfg live.LiveConfig) (live.LiveProvider, live.LiveConfig, error) {
	provider, normalized, err := live.NewProviderForConfig(cfg)
	if err != nil {
		return nil, normalized, err
	}
	if url := strings.TrimSpace(r.cfg.VoiceAgent.ThinkEndpointURL); url != "" {
		if dg, ok := provider.(*livedeepgram.Provider); ok {
			dg.ConfigureThink(
				r.cfg.VoiceAgent.ThinkProvider,
				r.cfg.VoiceAgent.ThinkModel,
				url,
				resolveCompanionSecret(r.cfg.VoiceAgent.ThinkAPIKeyEnv),
			)
			log.Printf("[voice_agent] BYO think: %s (%s)", url, r.cfg.VoiceAgent.ThinkModel)
		} else {
			log.Printf("[voice_agent] warning: think_endpoint_url is set, but provider %q does not support BYO think", cfg.Provider)
		}
	}
	return provider, normalized, nil
}

func liveProviderKeyEnv(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "assemblyai":
		return "ASSEMBLYAI_API_KEY"
	case "google":
		return "GOOGLE_AI_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	default:
		return "DEEPGRAM_API_KEY"
	}
}

func handleLocalVoiceAgentDetection(ctx context.Context, cfg *Config, audio *AudioIO, va *localVoiceAgentRuntime, ev wakeword.DetectionEvent) {
	// Subscribe before provider startup. The monitored live buffer prevents a
	// correction spoken after the wake event from disappearing during setup.
	// Authority begins only after a clean sample-counted silence boundary, so a
	// command already in progress when the wake event arrives is rejected rather
	// than partially transcribed.
	// This authority path intentionally has no audible wake cue: capturing the
	// host's own tone would let self-generated audio satisfy the speech gate.
	captureSubscription := audio.SubscribeMonitored(4096)
	log.Printf("[wake] %q (keyword=%s)", ev.Phrase, ev.Keyword)
	va.boxLink.SetStage(companion.StageWake)

	if err := va.CaptureAndSendTurn(ctx, cfg, audio, captureSubscription); err != nil {
		log.Printf("[voice_agent] %v", err)
		va.boxLink.SetStage(companion.StageError)
		_ = audio.PlayCue("error")
		return
	}
	va.boxLink.SetStage(companion.StageIdle)
}

func (r *localVoiceAgentRuntime) CaptureAndSendTurn(ctx context.Context, cfg *Config, audio *AudioIO, captureSubscription *audioSubscription) error {
	if captureSubscription == nil {
		captureSubscription = audio.SubscribeMonitored(4096)
	}
	defer audio.Unsubscribe(captureSubscription.frames)
	if err := r.ensureStarted(ctx); err != nil {
		return err
	}
	turnID := r.beginCaptureTurn()
	turnFinished := false
	defer func() {
		if !turnFinished {
			r.authority.abandonTurn(turnID)
			r.discardResponseAudio(turnID)
			r.retireLocalSession()
		}
	}()

	done := make(chan struct{}, 1)
	r.setTurnDone(done)
	defer r.clearTurnDone(done)
	r.resetResponseAudio(turnID)

	r.boxLink.SetStage(companion.StageListening)
	var capturedInput bytes.Buffer
	capture, err := streamAuthorityUtteranceCheckedFromSubscription(ctx, cfg, audio, captureSubscription, func(chunk []byte) error {
		_, writeErr := capturedInput.Write(chunk)
		return writeErr
	})
	if err != nil {
		return err
	}
	if !capture.Authoritative() {
		log.Printf("[voice_agent] host capture is not authoritative (end=%s dropped_frames=%d playback_overlap=%t); realtime output remains fail-closed", capture.End, capture.DroppedFrames, capture.PlaybackContaminated)
		return nil
	}
	authorityPCM, ok := extractAuthorityPCM(capture, capturedInput.Bytes())
	if !ok {
		return fmt.Errorf("voice_agent: verified capture boundary is outside the captured PCM")
	}
	if len(authorityPCM) < 16000 {
		log.Printf("[capture] too short/empty - ignored")
		return nil
	}
	if !r.sealAuthorityTranscript(ctx, turnID, authorityPCM) {
		return nil
	}
	if err := sendBufferedInput(authorityPCM, r.provider.SendAudio); err != nil {
		return fmt.Errorf("voice_agent send sealed audio: %w", err)
	}
	if err := r.provider.EndAudioStream(); err != nil {
		return fmt.Errorf("voice_agent audio_end: %w", err)
	}
	if !r.authority.closeInput(turnID) {
		return fmt.Errorf("voice_agent: input capture boundary could not be bound to the active turn")
	}
	log.Printf("[capture] accepted %d authoritative bytes - processing", len(authorityPCM))
	r.boxLink.SetStage(companion.StageThinking)
	go audio.CaptureAccepted()

	timeout := time.Duration(cfg.VoiceAgent.WaitTimeoutSec) * time.Second
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		log.Printf("[voice_agent] warning: no turn completion within %s", timeout)
	}

	authorized, reason := r.authority.finishTurn(turnID)
	turnFinished = true
	if !authorized || reason == "home_assistant_receipt_verified" {
		r.retireLocalSession()
	}
	if !authorized {
		r.discardResponseAudio(turnID)
		log.Printf("[voice_agent] suppressed unverified realtime output (%s)", reason)
		r.boxLink.SetStage(companion.StageError)
		_ = r.audio.PlayCue("error")
		return nil
	}

	response := r.takeResponseAudio(turnID)
	if len(response) == 0 {
		log.Printf("[voice_agent] no audio returned")
		return nil
	}
	r.boxLink.SetStage(companion.StageSpeaking)
	_ = r.audio.PlayCue("done")
	log.Printf("[voice_agent] playing %d bytes @24k mono", len(response))
	r.playMu.Lock()
	err = r.audio.PlayPCM(normalizeResponseGain(response), 24000, 1)
	r.playMu.Unlock()
	if err != nil {
		return fmt.Errorf("voice_agent playback: %w", err)
	}
	return nil
}

func (r *localVoiceAgentRuntime) sealAuthorityTranscript(ctx context.Context, turnID uint64, pcm []byte) bool {
	if r.authoritySTT == nil || len(pcm) == 0 {
		log.Printf("[voice_agent] host transcript seal unavailable; realtime Home Assistant actions remain fail-closed")
		return false
	}
	sttCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	result, err := r.authoritySTT.Transcribe(sttCtx, wavFromPCM16(pcm, 16000), stt.TranscribeOpts{
		Language: r.cfg.VoiceAgent.Locale,
	})
	if err != nil || result == nil || strings.TrimSpace(result.Text) == "" {
		log.Printf("[voice_agent] host transcript seal failed; realtime Home Assistant actions remain fail-closed")
		return false
	}
	if !r.authority.sealHostTranscript(turnID, sha256.Sum256(pcm), result.Text) {
		log.Printf("[voice_agent] host transcript could not be bound to the active turn; realtime Home Assistant actions remain fail-closed")
		return false
	}
	return true
}

func sendBufferedInput(pcm []byte, send func([]byte) error) error {
	if len(pcm) == 0 || send == nil {
		return nil
	}
	const chunkBytes = 3200 // 100 ms of 16 kHz PCM16 mono.
	for offset := 0; offset < len(pcm); offset += chunkBytes {
		end := min(offset+chunkBytes, len(pcm))
		if err := send(pcm[offset:end]); err != nil {
			return err
		}
	}
	return nil
}

// playPendingResponse plays agent audio that arose outside an active turn
// (e.g. the idle reminder "Anything else I can do?"). It used to be dropped
// without comment, so the agent spoke into the void.
func (r *localVoiceAgentRuntime) playPendingResponse(generation uint64) {
	r.playMu.Lock()
	defer r.playMu.Unlock()
	if !r.authority.claimIdlePlayback(generation) {
		r.discardResponseAudio(generation)
		return
	}
	response := r.takeResponseAudio(generation)
	if len(response) == 0 {
		return
	}
	r.boxLink.SetStage(companion.StageSpeaking)
	log.Printf("[voice_agent] playing %d bytes @24k mono (outside a turn)", len(response))
	err := r.audio.PlayPCM(normalizeResponseGain(response), 24000, 1)
	if err != nil {
		log.Printf("[voice_agent] playback: %v", err)
	}
	r.boxLink.SetStage(companion.StageIdle)
}

// onHostPrompt holds playMu from Started through Sent/SendFailed. This makes
// the host-prompt send and the next capture generation mutually exclusive:
// SendText cannot run after a new user turn has already revoked the prompt.
func (r *localVoiceAgentRuntime) onHostPrompt(event live.HostPromptEvent) bool {
	switch event.Type {
	case live.HostPromptStarted:
		r.playMu.Lock()
		r.discardResponseAudio(r.authority.abandonIdleOutput(0))
		r.discardResponseAudio(r.authority.abandonIdlePlayback(0))
		_, ok := r.authority.beginIdleOutput(event.ID)
		if !ok {
			r.playMu.Unlock()
		}
		return ok
	case live.HostPromptSent:
		r.playMu.Unlock()
		return true
	case live.HostPromptSendFailed:
		r.discardResponseAudio(r.authority.abandonIdleOutput(event.ID))
		r.discardResponseAudio(r.authority.abandonIdlePlayback(event.ID))
		r.playMu.Unlock()
		return true
	default:
		return false
	}
}

func (r *localVoiceAgentRuntime) beginCaptureTurn() uint64 {
	r.playMu.Lock()
	defer r.playMu.Unlock()
	r.discardResponseAudio(r.authority.abandonIdleOutput(0))
	r.discardResponseAudio(r.authority.abandonIdlePlayback(0))
	return r.authority.beginTurn()
}

func (r *localVoiceAgentRuntime) ensureStarted(ctx context.Context) error {
	// r.mu must NOT be held across StartVoiceAgent here: the live callbacks
	// (OnStateChange -> onState -> r.mu) already fire during the connect from
	// the receive loop whose progress Start waits for -> circular deadlock
	// (observed live 2026-07-09: state=listening logged, Start never
	// returned). startMu serializes concurrent setup attempts instead.
	r.startMu.Lock()
	defer r.startMu.Unlock()

	r.mu.Lock()
	started := r.started
	r.mu.Unlock()
	if started {
		return nil
	}

	if err := r.provider.StartVoiceAgent(ctx, voiceagent.Config{}, voiceagent.Callbacks{}); err != nil {
		return err
	}

	r.mu.Lock()
	r.started = true
	r.mu.Unlock()
	log.Printf("[voice_agent] local session started (provider=%s)", r.cfg.VoiceAgent.Provider)
	return nil
}

// onState detects the end of a turn: speaking/processing -> listening means
// the answer has been streamed completely. Idle deactivation shows up as a
// transition to inactive; the next wake detection starts afresh.
func (r *localVoiceAgentRuntime) onState(state live.State) {
	switch state {
	case live.StateListening:
		if !r.signalTurnDone() {
			// Only an explicit trusted host prompt can own out-of-turn audio.
			// A stale Listening transition has no generation and is ignored.
			if generation, ok := r.authority.finishIdleOutput(); ok {
				go r.playPendingResponse(generation)
			}
		}
	case live.StateInactive:
		r.discardResponseAudio(r.authority.abandonIdleOutput(0))
		r.discardResponseAudio(r.authority.abandonIdlePlayback(0))
		r.mu.Lock()
		r.started = false
		r.mu.Unlock()
		r.signalTurnDone()
	}
}

func (r *localVoiceAgentRuntime) Stop() {
	r.mu.Lock()
	started := r.started
	r.started = false
	r.mu.Unlock()
	if started {
		if _, err := r.provider.StopVoiceAgent(context.Background()); err != nil {
			log.Printf("[voice_agent] stop: %v", err)
		}
	}
	r.discardAllResponseAudio()
}

func (r *localVoiceAgentRuntime) retireLocalSession() {
	r.startMu.Lock()
	defer r.startMu.Unlock()
	r.mu.Lock()
	started := r.started
	r.started = false
	r.mu.Unlock()
	if started {
		if _, err := r.provider.StopVoiceAgent(context.Background()); err != nil {
			log.Printf("[voice_agent] retire session: %v", err)
		}
	}
}

func (r *localVoiceAgentRuntime) setTurnDone(ch chan struct{}) {
	r.mu.Lock()
	r.turnDone = ch
	r.mu.Unlock()
}

func (r *localVoiceAgentRuntime) clearTurnDone(ch chan struct{}) {
	r.mu.Lock()
	if r.turnDone == ch {
		r.turnDone = nil
	}
	r.mu.Unlock()
}

// signalTurnDone reports the end of a turn to a waiting turn and tells the
// caller whether one was waiting at all (false = the agent spoke outside a
// turn, so the caller has to handle the answer itself).
func (r *localVoiceAgentRuntime) signalTurnDone() bool {
	r.mu.Lock()
	ch := r.turnDone
	r.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- struct{}{}:
	default:
	}
	return true
}

func (r *localVoiceAgentRuntime) resetResponseAudio(generation uint64) {
	if generation == 0 {
		return
	}
	r.responseMu.Lock()
	r.responseAudio[generation] = &bytes.Buffer{}
	r.responseMu.Unlock()
}

func (r *localVoiceAgentRuntime) appendResponseAudio(generation uint64, chunk []byte) {
	if generation == 0 || len(chunk) == 0 {
		return
	}
	r.responseMu.Lock()
	buffer := r.responseAudio[generation]
	if buffer == nil {
		buffer = &bytes.Buffer{}
		r.responseAudio[generation] = buffer
	}
	_, _ = buffer.Write(chunk)
	r.responseMu.Unlock()
}

func (r *localVoiceAgentRuntime) takeResponseAudio(generation uint64) []byte {
	if generation == 0 {
		return nil
	}
	r.responseMu.Lock()
	defer r.responseMu.Unlock()
	buffer := r.responseAudio[generation]
	delete(r.responseAudio, generation)
	if buffer == nil {
		return nil
	}
	out := append([]byte(nil), buffer.Bytes()...)
	return out
}

func (r *localVoiceAgentRuntime) discardResponseAudio(generation uint64) {
	if generation == 0 {
		return
	}
	r.responseMu.Lock()
	delete(r.responseAudio, generation)
	r.responseMu.Unlock()
}

func (r *localVoiceAgentRuntime) discardAllResponseAudio() {
	r.responseMu.Lock()
	clear(r.responseAudio)
	r.responseMu.Unlock()
}
