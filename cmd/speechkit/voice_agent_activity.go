package main

import "github.com/kombifyio/SpeechKit/internal/voiceagent"

func voiceAgentUserActivityLevel(state voiceagent.State, level float64, guard *voiceAgentEchoGuard) float64 {
	if !voiceAgentMicFrameAllowed(state, guard) {
		return 0
	}
	if level < 0 || level != level {
		return 0
	}
	if level > 1 {
		return 1
	}
	return level
}
