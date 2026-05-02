package main

import (
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func voiceAgentProfileSnapshots() []voiceAgentProfileSnapshot {
	profiles := voiceagentprofile.BuiltInProfiles()
	out := make([]voiceAgentProfileSnapshot, 0, len(profiles))
	for _, profile := range profiles {
		out = append(out, voiceAgentProfileSnapshot{
			ID:                profile.ID,
			DisplayName:       profile.DisplayName,
			Description:       profile.Description,
			Voice:             profile.Voice,
			DefaultSequenceID: profile.DefaultSequenceID,
			BuiltIn:           profile.BuiltIn,
		})
	}
	return out
}

func resetInactiveVoiceAgentSession(state *appState) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.voiceAgentSession == nil {
		return
	}
	if state.voiceAgentSession.CurrentState() == voiceagent.StateInactive {
		state.voiceAgentSession = nil
	}
}
