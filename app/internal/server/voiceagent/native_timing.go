//go:build linux

package voiceagent

import (
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// nativeTurnTimer is the bound agent's phase report for one turn, keyed by
// the host's native final-transcript identity (a2a.Agent).
type nativeTurnTimer interface {
	TurnTiming(turnID string) (map[string]int64, bool)
}

// nativeDownlinkBytesPerSecond is the contracted downlink rate: 24 kHz S16
// mono. The server paces audio to the client in real time, so the answer's
// bytes estimate how long it plays after its first audio.
const nativeDownlinkBytesPerSecond = 24000 * 2

// nativeAgentTimingKeys are the agent phases a turn_timing record may carry:
// SpeechKit's A2A phases and the Companion run timings it forwards (numbers
// only). Anything else an agent reports is dropped.
var nativeAgentTimingKeys = map[string]bool{
	"a2a_header_ms": true, "a2a_first_delta_ms": true, "a2a_total_ms": true,
	"orch_rootRunMs": true, "orch_prepareMs": true, "orch_profileMs": true, "orch_workbenchMs": true,
	"orch_setupMs": true, "orch_capabilityMs": true, "orch_catalogsMs": true, "orch_enrichmentMs": true,
	"orch_knowledgeMs": true, "orch_privateSkillsWaitMs": true, "orch_firstPartyWaitMs": true,
	"orch_sessionSourcesMs": true, "orch_groundingMs": true, "orch_toolsetsMs": true, "orch_memoryWaitMs": true,
	"orch_modelMs": true, "orch_modelFirstTokenMs": true, "orch_firstAnswerTokenMs": true,
	"orch_toolMs": true, "orch_toolRounds": true, "orch_totalMs": true,
}

// nativePerimeterTimingKey admits the Gateway's Server-Timing phases, whose
// names the edge already restricts to lower-case identifiers.
var nativePerimeterTimingKey = regexp.MustCompile(`^perimeter_[a-z][a-z0-9_]{0,23}_ms$`)

// nativeTurnClock measures registered-native turns on the server: from the
// caller's final transcript (the vendor's end of turn) to the first answer
// audio and to the vendor's end of the answer. Only the provider's single
// read pump touches it, so it has no lock of its own.
type nativeTurnClock struct {
	heardAt         time.Time
	turnID          string
	disclosedBefore bool
	audioSeen       bool
	firstAudioAt    time.Time
	audioBytes      int
	// playsUntil estimates when the latest answer stops playing; an
	// interruption before it is a barge-in, latched into the turn the
	// caller's next final starts.
	playsUntil     time.Time
	pendingBargeIn bool
	bargeIn        bool
	answered       int64
}

// observeTurnTiming attaches `turn_timing` to the Done that ends an answered
// turn (docs/server/native-registered-voice.md). A Done with a turn error
// ends the turn unanswered and reports nothing; a Done before any answer audio
// does not end it. Vendors
// without a turn Done (GPT-Live, xAI) report no turn_timing.
func (p *registeredNativeProvider) observeTurnTiming(message *LiveMessage) {
	if message == nil {
		return
	}
	p.mu.Lock()
	timer, sessionID, turns, callback := p.timer, p.sessionID, p.turns, p.callback
	p.mu.Unlock()
	now := time.Now()
	c := &p.clock
	if message.Interrupted && now.Before(c.playsUntil) {
		c.pendingBargeIn, c.playsUntil = true, time.Time{}
	}
	if message.InputTranscriptDone && strings.TrimSpace(message.InputTranscript) != "" {
		// A later final of a split utterance restarts the turn: the answer
		// follows the last one, which is also the identity its agent run has.
		c.heardAt, c.audioSeen, c.audioBytes = now, false, 0
		c.bargeIn, c.pendingBargeIn = c.bargeIn || c.pendingBargeIn, false
		c.turnID = ""
		if turns != nil {
			c.turnID = turns.Current()
		}
		c.disclosedBefore = callback != nil && callback.Disclosed()
	}
	if c.heardAt.IsZero() {
		return
	}
	if len(message.Audio) > 0 {
		if !c.audioSeen {
			c.audioSeen, c.firstAudioAt = true, now
		}
		c.audioBytes += len(message.Audio)
		c.playsUntil = c.firstAudioAt.Add(time.Duration(c.audioBytes) * time.Second / nativeDownlinkBytesPerSecond)
	}
	if !message.Done || (message.ErrorCode == "" && !c.audioSeen) {
		// A vendor turn without answer audio (Gemini's delegating function
		// call) does not end the caller's turn.
		return
	}
	heardAt, bargeIn := c.heardAt, c.bargeIn
	c.heardAt, c.bargeIn = time.Time{}, false
	if message.ErrorCode != "" {
		return
	}
	c.answered++
	timing := map[string]any{
		"turn_index": c.answered,
		"turn_ms":    now.Sub(heardAt).Milliseconds(),
	}
	firstAudioMs := c.firstAudioAt.Sub(heardAt).Milliseconds()
	if !c.disclosedBefore && callback != nil && callback.Disclosed() {
		// The disclosure is spoken before the agent's first delta: its first
		// audio is not the agent's and stays out of first_audio_ms.
		timing["includes_disclosure"] = int64(1)
		timing["disclosure_first_audio_ms"] = firstAudioMs
	} else {
		timing["first_audio_ms"] = firstAudioMs
	}
	if bargeIn {
		timing["barge_in"] = int64(1)
	}
	if callback != nil && c.turnID != "" {
		// The vendor's hop: from the caller's final transcript to its think request reaching this server (negative
		// when the vendor dispatched before the final reached us), admission with the authority revalidation, and the
		// agent's first answer text leaving the callback. first_audio_ms minus callback_wait_ms and
		// callback_first_answer_ms is the vendor's speech synthesis and delivery.
		if hop, ok := callback.CallbackTiming(c.turnID); ok {
			timing["callback_wait_ms"] = hop.Arrived.Sub(heardAt).Milliseconds()
			timing["callback_admit_ms"] = hop.AdmitMs
			if hop.FirstAnswerMs >= 0 {
				timing["callback_first_answer_ms"] = hop.FirstAnswerMs
			}
		}
	}
	if timer != nil && c.turnID != "" {
		if agent, ok := timer.TurnTiming(c.turnID); ok {
			for name, ms := range agent {
				if ms >= 0 && (nativeAgentTimingKeys[name] || nativePerimeterTimingKey.MatchString(name)) {
					timing["agent_"+name] = ms
				}
			}
		}
	}
	metadata := make(map[string]any, len(message.ProviderMetadata)+1)
	for key, value := range message.ProviderMetadata {
		metadata[key] = value
	}
	metadata["turn_timing"] = timing
	message.ProviderMetadata = metadata
	attrs := make([]any, 0, 4+2*len(timing))
	attrs = append(attrs, "provider", p.provider, "session_id", sessionID)
	for name, value := range timing {
		attrs = append(attrs, name, value)
	}
	slog.Info("voiceagent native turn timing", attrs...)
}
