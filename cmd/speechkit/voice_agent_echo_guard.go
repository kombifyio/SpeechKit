package main

import (
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/voiceagent"
)

const defaultVoiceAgentEchoMuteTail = 180 * time.Millisecond

type voiceAgentEchoGuard struct {
	mu        sync.Mutex
	muteUntil time.Time
	tail      time.Duration
	now       func() time.Time
}

func newVoiceAgentEchoGuard(tail time.Duration) *voiceAgentEchoGuard {
	if tail <= 0 {
		tail = defaultVoiceAgentEchoMuteTail
	}
	return &voiceAgentEchoGuard{
		tail: tail,
		now:  time.Now,
	}
}

func (g *voiceAgentEchoGuard) markAssistantAudio() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.muteUntil = g.currentTimeLocked().Add(g.tail)
}

func (g *voiceAgentEchoGuard) reset() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.muteUntil = time.Time{}
}

func (g *voiceAgentEchoGuard) allows(state voiceagent.State) bool {
	if state == voiceagent.StateInactive || state == voiceagent.StateDeactivating || state == voiceagent.StateSpeaking {
		return false
	}
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return !g.currentTimeLocked().Before(g.muteUntil)
}

func (g *voiceAgentEchoGuard) currentTimeLocked() time.Time {
	if g.now == nil {
		return time.Now()
	}
	return g.now()
}

func (s *appState) currentVoiceAgentEchoGuard() *voiceAgentEchoGuard {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.voiceAgentEchoGuard == nil {
		s.voiceAgentEchoGuard = newVoiceAgentEchoGuard(0)
	}
	return s.voiceAgentEchoGuard
}

func (s *appState) markVoiceAgentAssistantAudio() {
	if s == nil {
		return
	}
	s.currentVoiceAgentEchoGuard().markAssistantAudio()
}

func (s *appState) resetVoiceAgentEchoGuard() {
	if s == nil {
		return
	}
	s.currentVoiceAgentEchoGuard().reset()
}

func voiceAgentMicFrameAllowed(state voiceagent.State, guard *voiceAgentEchoGuard) bool {
	if guard == nil {
		return state != voiceagent.StateInactive && state != voiceagent.StateDeactivating && state != voiceagent.StateSpeaking
	}
	return guard.allows(state)
}
