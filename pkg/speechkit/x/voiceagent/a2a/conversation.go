package a2a

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/cascaded"
)

// ErrConversationBusy proves that no mode command was dispatched. It is not
// an uncertain failure and must never force an existing voice mode to stop.
// ErrConversationUnsupported proves no command could be dispatched.
var ErrConversationUnsupported = errors.New("speechkit a2a: conversation command unsupported")

var ErrConversationBusy = errors.New("speechkit a2a: voice turn already running")

func (a *Agent) projectConversation(ctx context.Context, envelope any) error {
	root, _ := envelope.(map[string]any)
	result, _ := root["result"].(map[string]any)
	metadata, _ := result["metadata"].(map[string]any)
	value, exists := metadata["io.kombify.voice_mode"]
	if !exists {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 4096 {
		return errors.New("speechkit a2a: invalid conversation receipt")
	}
	var receipt VoiceModeReceipt
	if json.Unmarshal(encoded, &receipt) != nil {
		return errors.New("speechkit a2a: invalid conversation receipt")
	}
	return a.conversation.apply(ctx, receipt)
}

// ConversationCapability is a host-bound control surface for this voice
// lifetime. It is carried in the signed A2A turn, never in provider prompts.
type ConversationCapability struct {
	Version               int    `json:"version"`
	Mode                  string `json:"mode"`
	VoiceSessionID        string `json:"voice_session_id"`
	ConversationSessionID string `json:"conversation_session_id"`
	OneShotSessionID      string `json:"oneshot_session_id"`
}

// VoiceModeReceipt is emitted only by the selected agent's ordinary tool loop.
// ConversationSessionID identifies the owner-authorized canonical history.
type VoiceModeReceipt struct {
	Version               int                `json:"version"`
	Mode                  string             `json:"mode"`
	VoiceSessionID        string             `json:"voice_session_id"`
	ConversationSessionID string             `json:"conversation_session_id"`
	RequestID             string             `json:"request_id"`
	Status                string             `json:"status"`
	Code                  string             `json:"code,omitempty"`
	SessionProjection     *SessionProjection `json:"session_projection,omitempty"`
}

// SessionProjection reports whether the canonical session owner has linked
// this conversation to its actual project/environment; canonical history is separate.
type SessionProjection struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

// VoiceModeCommand is a panel's typed invocation of the same agent-owned command.
type VoiceModeCommand struct {
	Command   string `json:"command"`
	Mode      string `json:"mode"`
	RequestID string `json:"request_id"`
}

// SetVoiceMode invokes the ordinary agent-owned control without synthetic speech.
func (a *Agent) SetVoiceMode(ctx context.Context, mode, requestID string) error {
	if a.conversation == nil || !voiceMode(mode) || !controlID(requestID) {
		return ErrConversationUnsupported
	}
	_, err := a.streamTurn(ctx, cascaded.AgentInput{}, "", nil,
		&VoiceModeCommand{Command: "set_voice_mode", Mode: mode, RequestID: requestID})
	return err
}

func voiceModeTransportID(voiceID, requestID string) string {
	return fmt.Sprintf("voice-mode-%x", sha256.Sum256([]byte(voiceID+"\n"+requestID)))
}

// ConversationSession retains the agent-issued session switch for subsequent
// turns. Delivery must complete before the assistant can speak its tool result.
type ConversationSession struct {
	mu         sync.Mutex
	capability ConversationCapability
	deliver    func(context.Context, VoiceModeReceipt) error
}

// NewConversationSession creates an opt-in, voice-lifetime-scoped control.
func NewConversationSession(capability ConversationCapability, deliver func(context.Context, VoiceModeReceipt) error) (*ConversationSession, error) {
	if capability.Version != 1 || !voiceMode(capability.Mode) || !controlID(capability.VoiceSessionID) ||
		!controlID(capability.ConversationSessionID) || !controlID(capability.OneShotSessionID) || deliver == nil {
		return nil, errors.New("speechkit a2a: invalid conversation capability")
	}
	return &ConversationSession{capability: capability, deliver: deliver}, nil
}

// Snapshot returns the current signed turn metadata and history binding.
func (s *ConversationSession) Snapshot() ConversationCapability {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capability
}

func (s *ConversationSession) apply(ctx context.Context, receipt VoiceModeReceipt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if receipt.Version != 1 || !voiceMode(receipt.Mode) || receipt.VoiceSessionID != s.capability.VoiceSessionID ||
		!controlID(receipt.RequestID) || !controlID(receipt.ConversationSessionID) ||
		(receipt.Status != "accepted" && receipt.Status != "rejected") || len(receipt.Code) > 128 {
		return errors.New("speechkit a2a: invalid conversation receipt")
	}
	if receipt.Status == "accepted" && ((receipt.Mode == "oneshot" && receipt.ConversationSessionID != s.capability.OneShotSessionID) ||
		(receipt.Mode == "conversation" && receipt.ConversationSessionID == s.capability.OneShotSessionID)) {
		return errors.New("speechkit a2a: conversation history boundary rejected")
	}
	if receipt.Status == "rejected" && (receipt.Mode != s.capability.Mode || receipt.ConversationSessionID != s.capability.ConversationSessionID || receipt.Code == "") {
		return errors.New("speechkit a2a: invalid rejected conversation receipt")
	}
	if err := s.deliver(ctx, receipt); err != nil {
		return err
	}
	if receipt.Status == "accepted" {
		s.capability.Mode, s.capability.ConversationSessionID = receipt.Mode, receipt.ConversationSessionID
	}
	return nil
}

func voiceMode(value string) bool { return value == "oneshot" || value == "conversation" }
func controlID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.') {
			return false
		}
	}
	return true
}
