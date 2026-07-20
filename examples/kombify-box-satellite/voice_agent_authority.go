//go:build windows && cgo

package main

import (
	"errors"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
)

const maxHAAuthorityTextBytes = 4096

var errHAToolCallDenied = errors.New("home assistant tool call denied")

// haTurnGate is the local playback authorization boundary for realtime turns.
// The realtime provider may suggest a tool call, but it cannot authorize its
// own spoken smart-home result. Authorization requires a deterministic host
// classification, a terminal result for the exact same tool call and turn,
// and an exact transcript match to the Home Assistant result.
type haTurnGate struct {
	mu        sync.Mutex
	bridge    *haBridge
	locale    string
	sessionID string

	nextID               uint64
	active               uint64
	idle                 uint64
	idlePromptID         uint64
	idlePlayable         uint64
	idlePlayablePromptID uint64

	inputFinal               bool
	inputFinalSegments       int
	inputAmbiguous           bool
	inputClosed              bool
	inputText                string
	inputClassificationKnown bool
	requiresHA               bool
	hostInputSealed          bool
	hostInputAudioDigest     [32]byte
	hostInputText            string
	hostClassificationKnown  bool
	hostRequiresHA           bool
	haCallAuthorized         bool
	toolAuthorizationFailed  bool
	outputFinal              bool
	outputAmbiguous          bool
	outputText               string
	receiptText              string
	receiptAmbiguous         bool
	audioBeforeReceipt       bool
	pendingCalls             map[string]uint64
	idleObservedOutput       bool
	idleBlocked              bool
}

func newHATurnGate(bridge *haBridge, locale string) *haTurnGate {
	return &haTurnGate{
		bridge:       bridge,
		locale:       locale,
		pendingCalls: make(map[string]uint64),
	}
}

func (g *haTurnGate) beginSession(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sessionID = strings.TrimSpace(sessionID)
	g.active = 0
	g.idle = 0
	g.idlePlayable = 0
	g.idleBlocked = g.sessionID == ""
	clear(g.pendingCalls)
}

func (g *haTurnGate) endSession(sessionID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if strings.TrimSpace(sessionID) == "" || g.sessionID != strings.TrimSpace(sessionID) {
		return
	}
	g.sessionID = ""
	g.active = 0
	g.idle = 0
	g.idlePlayable = 0
	g.idleBlocked = true
	clear(g.pendingCalls)
}

func (g *haTurnGate) beginTurn() uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nextID++
	g.active = g.nextID
	g.idle = 0
	g.idlePromptID = 0
	g.idlePlayable = 0
	g.idlePlayablePromptID = 0
	g.idleObservedOutput = false
	g.idleBlocked = false
	g.inputFinal = false
	g.inputFinalSegments = 0
	g.inputAmbiguous = false
	g.inputClosed = false
	g.inputText = ""
	g.inputClassificationKnown = false
	g.requiresHA = false
	g.hostInputSealed = false
	g.hostInputAudioDigest = [32]byte{}
	g.hostInputText = ""
	g.hostClassificationKnown = false
	g.hostRequiresHA = false
	g.haCallAuthorized = false
	g.toolAuthorizationFailed = false
	g.outputFinal = false
	g.outputAmbiguous = false
	g.outputText = ""
	g.receiptText = ""
	g.receiptAmbiguous = false
	g.audioBeforeReceipt = false
	clear(g.pendingCalls)
	return g.active
}

func (g *haTurnGate) abandonTurn(turnID uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active == turnID {
		g.active = 0
		g.idleBlocked = true
		clear(g.pendingCalls)
	}
}

// closeInput records the host-side capture boundary for this exact turn. It
// may be set only after local capture has ended and EndAudioStream succeeded.
// A provider-side final segment or server VAD event cannot close host input.
func (g *haTurnGate) closeInput(turnID uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if turnID == 0 || g.active != turnID || g.inputClosed {
		return false
	}
	g.inputClosed = true
	return true
}

// sealHostTranscript binds one immutable whole-capture transcript to the turn.
// The caller must derive text from the completed host audio buffer through a
// host-owned batch STT path; provider streaming transcript segments are never
// accepted here. This is the semantic authority used for side effects.
func (g *haTurnGate) sealHostTranscript(turnID uint64, audioDigest [32]byte, text string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if turnID == 0 || g.active != turnID || g.hostInputSealed || g.bridge == nil ||
		audioDigest == ([32]byte{}) ||
		strings.TrimSpace(text) == "" || len(text) > maxHAAuthorityTextBytes || !utf8.ValidString(text) {
		return false
	}
	requiresHA, err := g.bridge.classifiesTranscript(text, g.locale)
	if err != nil {
		return false
	}
	g.hostInputSealed = true
	g.hostInputAudioDigest = audioDigest
	g.hostInputText = text
	g.hostClassificationKnown = true
	g.hostRequiresHA = requiresHA
	return true
}

func (g *haTurnGate) observeInputTranscript(text string, done bool) {
	if !done {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active == 0 {
		return
	}
	g.inputFinalSegments++
	if g.inputFinalSegments != 1 {
		g.inputFinal = false
		g.inputAmbiguous = true
		g.inputText = ""
		g.inputClassificationKnown = false
		g.requiresHA = false
		return
	}
	g.inputFinal = strings.TrimSpace(text) != "" && len(text) <= maxHAAuthorityTextBytes && utf8.ValidString(text)
	if !g.inputFinal || g.bridge == nil {
		return
	}
	g.inputText = text
	requiresHA, err := g.bridge.classifiesTranscript(text, g.locale)
	if err != nil {
		return
	}
	g.inputClassificationKnown = true
	g.requiresHA = requiresHA
}

func (g *haTurnGate) observeOutputTranscript(text string, done bool) {
	if !done {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active == 0 && g.idle != 0 {
		if strings.TrimSpace(text) != "" {
			g.idleObservedOutput = true
		}
		return
	}
	if g.active == 0 {
		return
	}
	if g.outputFinal {
		g.outputAmbiguous = true
	}
	g.outputFinal = strings.TrimSpace(text) != ""
	g.outputText = text
}

func (g *haTurnGate) observeAudio() (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active != 0 {
		classificationKnown := g.hostInputSealed && g.hostClassificationKnown
		requiresHA := g.hostRequiresHA
		if !classificationKnown {
			classificationKnown = g.inputClassificationKnown
			requiresHA = g.requiresHA
		}
		if !classificationKnown || (requiresHA && g.receiptText == "") {
			g.audioBeforeReceipt = true
		}
		return g.active, true
	}
	if g.idle != 0 && !g.idleBlocked {
		g.idleObservedOutput = true
		return g.idle, true
	}
	return 0, false
}

func (g *haTurnGate) acceptsAudioGeneration(generation uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return generation != 0 && (g.active == generation || (g.idle == generation && !g.idleBlocked))
}

// beginIdleOutput opens a playback generation only for a trusted host prompt
// announced synchronously by live.Session. Arbitrary out-of-turn callbacks do
// not create a generation and are therefore discarded.
func (g *haTurnGate) beginIdleOutput(promptID uint64) (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if promptID == 0 || g.active != 0 || g.idleBlocked {
		return 0, false
	}
	g.nextID++
	g.idle = g.nextID
	g.idlePromptID = promptID
	g.idleObservedOutput = false
	return g.idle, true
}

func (g *haTurnGate) finishIdleOutput() (uint64, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.idle == 0 || !g.idleObservedOutput || g.idleBlocked {
		return 0, false
	}
	generation := g.idle
	promptID := g.idlePromptID
	g.idle = 0
	g.idlePromptID = 0
	g.idleObservedOutput = false
	g.idlePlayable = generation
	g.idlePlayablePromptID = promptID
	return generation, true
}

func (g *haTurnGate) abandonIdleOutput(promptID uint64) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if promptID != 0 && promptID != g.idlePromptID {
		return 0
	}
	generation := g.idle
	g.idle = 0
	g.idlePromptID = 0
	g.idleObservedOutput = false
	return generation
}

func (g *haTurnGate) abandonIdlePlayback(promptID uint64) uint64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if promptID != 0 && promptID != g.idlePlayablePromptID {
		return 0
	}
	generation := g.idlePlayable
	g.idlePlayable = 0
	g.idlePlayablePromptID = 0
	return generation
}

func (g *haTurnGate) claimIdlePlayback(generation uint64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if generation == 0 || g.active != 0 || g.idleBlocked || g.idlePlayable != generation {
		return false
	}
	g.idlePlayable = 0
	g.idlePlayablePromptID = 0
	return true
}

// authorizeToolCall is the synchronous side-effect boundary for realtime
// Home Assistant calls. The model may propose a tool call, but only the host's
// deterministic classification of the final user transcript can authorize it.
// The tool receives the host transcript and locale, never model-expanded
// arguments. At most one Home Assistant call can be authorized per turn.
func (g *haTurnGate) authorizeToolCall(sessionID string, call agentkit.ToolCall) (map[string]any, error) {
	if call.Name != intentHomeAssistant {
		return call.Args, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()

	deny := func() (map[string]any, error) {
		g.toolAuthorizationFailed = true
		g.idleBlocked = true
		return nil, errHAToolCallDenied
	}
	if g.active == 0 || g.sessionID == "" || strings.TrimSpace(sessionID) != g.sessionID || strings.TrimSpace(call.ID) == "" {
		return deny()
	}
	if g.toolAuthorizationFailed || !g.inputClosed || !g.hostInputSealed || !g.hostClassificationKnown || !g.hostRequiresHA {
		return deny()
	}
	if g.haCallAuthorized {
		return deny()
	}
	query, ok := call.Args["query"].(string)
	if !ok || len(query) > maxHAAuthorityTextBytes || !utf8.ValidString(query) ||
		normalizeAuthorityWhitespace(query) != normalizeAuthorityWhitespace(g.hostInputText) {
		return deny()
	}

	g.haCallAuthorized = true
	g.pendingCalls[call.ID] = g.active
	return map[string]any{
		"query":  g.hostInputText,
		"locale": g.locale,
	}, nil
}

func (g *haTurnGate) observeToolResult(sessionID string, call agentkit.ToolCall, response agentkit.ToolResponse) {
	if call.Name != intentHomeAssistant || response.Name != intentHomeAssistant || response.ID != call.ID {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sessionID == "" || strings.TrimSpace(sessionID) != g.sessionID {
		return
	}
	turnID, ok := g.pendingCalls[call.ID]
	delete(g.pendingCalls, call.ID)
	if !ok || turnID == 0 || turnID != g.active {
		return
	}
	text, terminal := terminalHAAuthorityText(response.Response)
	if !terminal {
		return
	}
	if g.receiptText != "" {
		g.receiptAmbiguous = true
		return
	}
	g.receiptText = text
}

func terminalHAAuthorityText(result map[string]any) (string, bool) {
	matched, _ := result["matched"].(bool)
	text, _ := result["text"].(string)
	if !matched || strings.TrimSpace(text) == "" || len(text) > maxHAAuthorityTextBytes || !utf8.ValidString(text) {
		return "", false
	}
	return text, true
}

func (g *haTurnGate) finishTurn(turnID uint64) (bool, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if turnID == 0 || g.active != turnID {
		return false, "turn_not_active"
	}
	defer func() {
		g.active = 0
		clear(g.pendingCalls)
	}()
	if !g.inputClosed {
		g.idleBlocked = true
		return false, "input_capture_unverified"
	}
	if g.toolAuthorizationFailed {
		g.idleBlocked = true
		return false, "home_assistant_tool_call_rejected"
	}
	if !g.hostInputSealed || !g.hostClassificationKnown || g.hostInputAudioDigest == ([32]byte{}) {
		g.idleBlocked = true
		return false, "host_input_seal_missing"
	}
	if !g.hostRequiresHA {
		return true, "general_conversation"
	}
	// Retire this realtime session after every HA-classified turn. This prevents
	// provider callbacks without response IDs from ever being reclassified as a
	// later idle generation.
	g.idleBlocked = true
	if g.receiptAmbiguous || g.receiptText == "" {
		return false, "home_assistant_receipt_missing"
	}
	if g.audioBeforeReceipt {
		return false, "audio_preceded_home_assistant_receipt"
	}
	if !g.outputFinal {
		return false, "output_transcript_missing"
	}
	if g.outputAmbiguous {
		return false, "output_transcript_ambiguous"
	}
	if normalizeAuthorityWhitespace(g.outputText) != normalizeAuthorityWhitespace(g.receiptText) {
		return false, "output_transcript_mismatch"
	}
	return true, "home_assistant_receipt_verified"
}

func normalizeAuthorityWhitespace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
