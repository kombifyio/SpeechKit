//go:build windows && cgo

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/client"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

type voiceAgentRuntime struct {
	cfg   *Config
	audio *AudioIO
	api   *client.Client

	mu            sync.Mutex
	session       *client.VoiceAgentSession
	sessionID     string
	readerCancel  context.CancelFunc
	lastState     string
	turnDone      chan struct{}
	responseMu    sync.Mutex
	responseAudio bytes.Buffer
}

func newVoiceAgentRuntime(cfg *Config, audio *AudioIO) (*voiceAgentRuntime, error) {
	api, err := client.New(client.Options{
		BaseURL: cfg.SpeechKitServer.BaseURL,
		Token:   cfg.speechKitServerToken(),
		Timeout: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if cfg.speechKitServerToken() == "" {
		log.Printf("[voice_agent] warning: %s is not set; server auth must be public/none or session creation will fail", cfg.SpeechKitServer.TokenEnv)
	}
	return &voiceAgentRuntime{
		cfg:   cfg,
		audio: audio,
		api:   api,
	}, nil
}

func handleVoiceAgentDetection(ctx context.Context, cfg *Config, audio *AudioIO, va *voiceAgentRuntime, ev wakeword.DetectionEvent) {
	log.Printf("[wake] %q (keyword=%s)", ev.Phrase, ev.Keyword)
	log.Printf("[event] voice_agent_wake mode=listening text=%q", ev.Phrase)

	audio.Ding()

	if err := va.CaptureAndSendTurn(ctx, cfg, audio); err != nil {
		log.Printf("[voice_agent] %v", err)
		log.Printf("[event] error mode=voice_agent text=%q", err.Error())
	}
}

func (r *voiceAgentRuntime) CaptureAndSendTurn(ctx context.Context, cfg *Config, audio *AudioIO) error {
	session, err := r.ensureStarted(ctx)
	if err != nil {
		return err
	}

	done := make(chan struct{}, 1)
	r.setTurnDone(done)
	defer r.clearTurnDone(done)
	r.resetResponseAudio()

	bytesSent, spoke, err := streamUtterance(ctx, cfg, audio, func(chunk []byte) error {
		return session.SendAudio(ctx, chunk)
	})
	if err != nil {
		return err
	}
	if bytesSent < 16000 || !spoke {
		log.Printf("[capture] zu kurz/leer - ignoriert")
		return nil
	}
	if err := session.SendAudioEnd(ctx); err != nil {
		return fmt.Errorf("voice_agent audio_end: %w", err)
	}
	log.Printf("[capture] accepted %d bytes - processing", bytesSent)
	log.Printf("[event] capture_done mode=think text=%q", "verstanden")
	go audio.CaptureAccepted()

	timeout := time.Duration(cfg.VoiceAgent.WaitTimeoutSec) * time.Second
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		log.Printf("[voice_agent] warning: no turn completion within %s", timeout)
	}

	response := r.takeResponseAudio()
	if len(response) == 0 {
		log.Printf("[voice_agent] no audio returned")
		log.Printf("[event] voice_agent_turn_done mode=idle text=%q", "")
		return nil
	}
	log.Printf("[voice_agent] playing %d bytes @24k mono", len(response))
	if err := r.audio.PlayPCM(response, 24000, 1); err != nil {
		return fmt.Errorf("voice_agent playback: %w", err)
	}
	log.Printf("[event] voice_agent_turn_done mode=idle text=%q", "")
	return nil
}

func streamUtterance(ctx context.Context, cfg *Config, audio *AudioIO, send func([]byte) error) (int, bool, error) {
	result, err := streamUtteranceChecked(ctx, cfg, audio, send)
	if err == nil && result.DroppedFrames != 0 {
		err = fmt.Errorf("voice_agent capture dropped %d microphone frame(s)", result.DroppedFrames)
	}
	if err == nil && result.End == utteranceEndSourceClosed {
		err = fmt.Errorf("voice_agent capture source closed before a verified utterance boundary")
	}
	return result.Bytes, result.Spoke, err
}

func streamUtteranceChecked(ctx context.Context, cfg *Config, audio *AudioIO, send func([]byte) error) (utteranceCaptureResult, error) {
	return streamUtteranceCheckedFromSubscription(ctx, cfg, audio, audio.SubscribeMonitored(64), send)
}

func streamUtteranceCheckedFromSubscription(ctx context.Context, cfg *Config, audio *AudioIO, subscription *audioSubscription, send func([]byte) error) (utteranceCaptureResult, error) {
	return streamUtteranceCheckedWithBoundary(ctx, cfg, audio, subscription, send,
		newUtteranceBoundary(cfg.Capture.MaxUtteranceSec, cfg.Capture.SilenceCutoffMS))
}

func streamAuthorityUtteranceCheckedFromSubscription(ctx context.Context, cfg *Config, audio *AudioIO, subscription *audioSubscription, send func([]byte) error) (utteranceCaptureResult, error) {
	return streamUtteranceCheckedWithBoundary(ctx, cfg, audio, subscription, send,
		newAuthorityUtteranceBoundary(cfg.Capture.MaxUtteranceSec, cfg.Capture.SilenceCutoffMS))
}

func streamUtteranceCheckedWithBoundary(ctx context.Context, cfg *Config, audio *AudioIO, subscription *audioSubscription, send func([]byte) error, boundary *utteranceBoundary) (utteranceCaptureResult, error) {
	if subscription == nil {
		return utteranceCaptureResult{}, fmt.Errorf("voice_agent capture subscription is nil")
	}
	frames := subscription.frames
	finish := func(result utteranceCaptureResult, err error) (utteranceCaptureResult, error) {
		audio.Unsubscribe(frames)
		result.DroppedFrames = subscription.DroppedFrames()
		result.PlaybackContaminated = subscription.PlaybackContaminated()
		return result, err
	}

	// Sample counts define semantic boundaries. This timer is only a liveness
	// guard for a capture device that stops producing frames; it can never
	// produce an authoritative end reason.
	frameStall := time.NewTimer(5 * time.Second)
	defer frameStall.Stop()
	resetFrameStall := func() {
		if !frameStall.Stop() {
			select {
			case <-frameStall.C:
			default:
			}
		}
		frameStall.Reset(5 * time.Second)
	}
	for {
		select {
		case <-ctx.Done():
			return finish(boundary.result(""), ctx.Err())
		case <-frameStall.C:
			return finish(boundary.result(utteranceEndSourceClosed), nil)
		case pcm, ok := <-frames:
			if !ok {
				return finish(boundary.result(utteranceEndSourceClosed), nil)
			}
			resetFrameStall()
			if boundary.wouldExceed(len(pcm)) {
				return finish(boundary.result(utteranceEndMaxDuration), nil)
			}
			if err := send(pcm); err != nil {
				return finish(boundary.result(""), fmt.Errorf("voice_agent send audio: %w", err))
			}
			if end := boundary.observe(len(pcm), rms16(pcm) >= cfg.Capture.SilenceRMS); end != "" {
				return finish(boundary.result(end), nil)
			}
		}
	}
}

func (r *voiceAgentRuntime) ensureStarted(ctx context.Context) (*client.VoiceAgentSession, error) {
	r.mu.Lock()
	if r.session != nil {
		session := r.session
		r.mu.Unlock()
		return session, nil
	}
	r.mu.Unlock()

	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	ticket, err := r.api.CreateVoiceAgentSession(connectCtx)
	if err != nil {
		return nil, fmt.Errorf("speechkit server create voice-agent session (%s): %w", r.cfg.SpeechKitServer.BaseURL, err)
	}
	session, err := r.api.DialVoiceAgent(connectCtx, ticket)
	if err != nil {
		_ = r.api.DeleteVoiceAgentSession(context.WithoutCancel(ctx), ticket.SessionID)
		return nil, err
	}

	start := client.VoiceAgentStartFrame{
		PersonaID:            r.cfg.VoiceAgent.PersonaID,
		RoleID:               r.cfg.VoiceAgent.RoleID,
		SequenceID:           r.cfg.VoiceAgent.SequenceID,
		Provider:             r.cfg.VoiceAgent.Provider,
		MediaTransport:       r.cfg.VoiceAgent.MediaTransport,
		Voice:                r.cfg.VoiceAgent.Voice,
		Locale:               r.cfg.VoiceAgent.Locale,
		Model:                r.cfg.VoiceAgent.Model,
		Thinking:             r.cfg.VoiceAgent.Thinking,
		SystemPromptOverride: r.cfg.VoiceAgent.SystemPrompt,
	}
	if err := session.SendStart(connectCtx, start); err != nil {
		_ = session.Close()
		_ = r.api.DeleteVoiceAgentSession(context.WithoutCancel(ctx), ticket.SessionID)
		return nil, fmt.Errorf("speechkit server start voice-agent session: %w", err)
	}

	readerCtx, readerCancel := context.WithCancel(context.WithoutCancel(ctx))
	r.mu.Lock()
	r.session = session
	r.sessionID = ticket.SessionID
	r.readerCancel = readerCancel
	r.lastState = ""
	r.mu.Unlock()

	go r.readLoop(readerCtx, session)
	log.Printf("[voice_agent] connected server=%s session=%s provider=%s", r.cfg.SpeechKitServer.BaseURL, ticket.SessionID, r.cfg.VoiceAgent.Provider)
	return session, nil
}

func (r *voiceAgentRuntime) readLoop(ctx context.Context, session *client.VoiceAgentSession) {
	for {
		msg, err := session.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				log.Printf("[voice_agent] server read: %v", err)
			}
			r.clearSession(session)
			r.signalTurnDone()
			return
		}
		if len(msg.Audio) > 0 {
			r.appendResponseAudio(msg.Audio)
			continue
		}
		if msg.Frame == nil {
			continue
		}
		r.handleServerFrame(msg.Frame)
	}
}

func (r *voiceAgentRuntime) handleServerFrame(f *client.VoiceAgentFrame) {
	switch f.Type {
	case "state":
		log.Printf("[event] voice_agent_state mode=%s text=%q", f.State, "")
		prev := r.setLastState(f.State)
		if f.State == "listening" && (prev == "speaking" || prev == "processing") {
			r.signalTurnDone()
		}
	case "input_transcript":
		if f.Done && strings.TrimSpace(f.Text) != "" {
			log.Printf("[stt] %q", f.Text)
		}
	case "output_transcript":
		if f.Done && strings.TrimSpace(f.Text) != "" {
			log.Printf("[voice_agent] %q", f.Text)
		}
	case "sequence_step":
		log.Printf("[event] voice_agent_sequence_step mode=%s text=%q", f.Status, f.StepID)
	case "event":
		if f.EventType == "turn_end" || hasEventType(f.EventTypes, "turn_end") {
			r.signalTurnDone()
		}
	case "interrupted":
		log.Printf("[event] voice_agent_interrupted mode=voice_agent text=%q", "")
	case "error":
		log.Printf("[event] error mode=voice_agent text=%q", f.Code+": "+f.Message)
		r.signalTurnDone()
	case "session_end":
		log.Printf("[event] voice_agent_session_end mode=voice_agent text=%q", f.Reason)
		r.signalTurnDone()
		r.Stop()
	}
}

func hasEventType(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (r *voiceAgentRuntime) setLastState(state string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	prev := r.lastState
	r.lastState = state
	return prev
}

func (r *voiceAgentRuntime) Stop() {
	r.mu.Lock()
	session := r.session
	sessionID := r.sessionID
	cancel := r.readerCancel
	r.session = nil
	r.sessionID = ""
	r.readerCancel = nil
	r.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if session != nil {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = session.SendStop(stopCtx)
		stopCancel()
		_ = session.Close()
	}
	if sessionID != "" {
		deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer deleteCancel()
		if err := r.api.DeleteVoiceAgentSession(deleteCtx, sessionID); err != nil {
			log.Printf("[voice_agent] delete session %s: %v", sessionID, err)
		}
	}
}

func (r *voiceAgentRuntime) clearSession(session *client.VoiceAgentSession) {
	r.mu.Lock()
	if r.session == session {
		r.session = nil
		r.sessionID = ""
		r.readerCancel = nil
	}
	r.mu.Unlock()
}

func (r *voiceAgentRuntime) setTurnDone(done chan struct{}) {
	r.mu.Lock()
	r.turnDone = done
	r.mu.Unlock()
}

func (r *voiceAgentRuntime) clearTurnDone(done chan struct{}) {
	r.mu.Lock()
	if r.turnDone == done {
		r.turnDone = nil
	}
	r.mu.Unlock()
}

func (r *voiceAgentRuntime) signalTurnDone() {
	r.mu.Lock()
	done := r.turnDone
	r.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case done <- struct{}{}:
	default:
	}
}

func (r *voiceAgentRuntime) appendResponseAudio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	r.responseMu.Lock()
	_, _ = r.responseAudio.Write(pcm)
	r.responseMu.Unlock()
}

func (r *voiceAgentRuntime) resetResponseAudio() {
	r.responseMu.Lock()
	r.responseAudio.Reset()
	r.responseMu.Unlock()
}

func (r *voiceAgentRuntime) takeResponseAudio() []byte {
	r.responseMu.Lock()
	defer r.responseMu.Unlock()
	if r.responseAudio.Len() == 0 {
		return nil
	}
	out := append([]byte(nil), r.responseAudio.Bytes()...)
	r.responseAudio.Reset()
	return out
}
