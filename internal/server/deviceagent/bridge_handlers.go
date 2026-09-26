package deviceagent

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

func (b *Bridge) register(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.Registration
	if !b.decode(w, r, &body) {
		return
	}
	if body.Version != wire.CurrentProtocolVersion {
		b.writeError(w, http.StatusUnprocessableEntity, "protocol_version_unsupported", "device_agent_protocol_mismatch", false, "no", "Update the device agent to the server-supported protocol version.")
		return
	}
	if !b.requireBoundDevice(w, binding, body.Device.DeviceID, body.Device.RoomID) {
		return
	}
	haCapability := wire.CapabilityState{Status: wire.CapabilityUnavailable, ReasonCode: "ha_probe_unavailable"}
	probeCtx, cancel := context.WithTimeout(r.Context(), b.probeTimeout)
	probeErr := b.ha.Probe(probeCtx)
	cancel()
	if probeErr == nil {
		haCapability = wire.CapabilityState{Status: wire.CapabilityReady}
	} else {
		var dispatchErr *HomeAssistantDispatchError
		if errors.As(probeErr, &dispatchErr) && dispatchErr.ReasonCode != "" {
			haCapability.ReasonCode = dispatchErr.ReasonCode
		}
	}
	ttsCapability := wire.CapabilityState{Status: wire.CapabilityUnavailable, ReasonCode: "tts_unavailable"}
	if b.tts != nil && b.ttsReady {
		ttsCtx, ttsCancel := context.WithTimeout(r.Context(), b.probeTimeout)
		health := b.tts.ReadyHealthCheck(ttsCtx)
		ttsCancel()
		for _, healthErr := range health {
			if healthErr == nil {
				ttsCapability = wire.CapabilityState{Status: wire.CapabilityReady}
				break
			}
		}
		if ttsCapability.Status != wire.CapabilityReady && len(health) > 0 {
			ttsCapability.ReasonCode = "tts_probe_unavailable"
		}
	}
	b.writeJSON(w, http.StatusOK, wire.RegistrationAck{
		Status:           "paired",
		PairingID:        binding.pairingID,
		ServerInstanceID: b.serverInstanceID,
		ServerTime:       b.now().UTC().Format(time.RFC3339Nano),
		Capabilities: wire.BridgeCapabilities{
			HomeAssistant: haCapability,
			TTS:           ttsCapability,
		},
	})
}

func (b *Bridge) events(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.Event
	if !b.decode(w, r, &body) {
		return
	}
	if !b.requireBoundDevice(w, binding, body.DeviceID, body.RoomID) {
		return
	}
	if !allowedEventType(body.Type) || body.Surface != "device_agent" || body.Transport != "local_http" {
		b.writeError(w, http.StatusUnprocessableEntity, "event_rejected", "device_event_contract_invalid", false, "not_applicable", "Send only documented local device-agent lifecycle events.")
		return
	}
	if len(body.SessionID) > 128 || len(body.RequestID) > 64 || len(body.Text) > maxAssistTextBytes || len(body.SpeakText) > maxAssistTextBytes {
		b.writeError(w, http.StatusRequestEntityTooLarge, "event_rejected", "device_event_too_large", false, "not_applicable", "Reduce event metadata to the documented bounds.")
		return
	}
	// Never log Text, SpeakText, tokens, or arbitrary metadata.
	slog.Info("device-agent lifecycle event",
		"event_type", body.Type,
		"device_id", binding.deviceID,
		"room_id", binding.roomID,
		"request_id", body.RequestID,
		"session_id", body.SessionID)
	b.writeJSON(w, http.StatusAccepted, wire.EventAck{Status: "accepted"})
}

func (b *Bridge) synthesize(w http.ResponseWriter, r *http.Request, binding deviceBinding) {
	var body wire.TTSRequest
	if !b.decode(w, r, &body) {
		return
	}
	body.RequestID = strings.TrimSpace(body.RequestID)
	body.Format = strings.ToLower(strings.TrimSpace(body.Format))
	now := b.now().UTC()
	if body.RequestID == "" || (body.Format != "" && body.Format != "wav") {
		b.writeError(w, http.StatusUnprocessableEntity, "tts_request_invalid", "tts_fields_invalid", false, "not_applicable", "Provide the completed assist request_id and request WAV output.")
		return
	}
	if reason := validateUUIDv7Window(body.RequestID, now, b.maxRequestAge, b.futureSkew); reason != "" {
		b.writeError(w, http.StatusUnprocessableEntity, "request_id_invalid", reason, false, "not_applicable", "Use the UUIDv7 request_id returned by the completed Assist call.")
		return
	}
	decision, err := b.claims.Lookup(r.Context(), ClaimKey{PairingID: binding.pairingID, RequestID: body.RequestID}, now)
	if err != nil {
		b.writeError(w, http.StatusServiceUnavailable, "claim_store_unavailable", "durable_lookup_failed", false, "not_applicable", "Retry after repairing the local safety ledger.")
		return
	}
	if decision.Disposition == ClaimNotFound {
		b.writeError(w, http.StatusNotFound, "tts_source_not_found", "assist_result_not_found", false, "not_applicable", "Use a completed Assist request from this pairing epoch.")
		return
	}
	if decision.Disposition == ClaimIndeterminate {
		b.writeError(w, http.StatusConflict, "request_outcome_indeterminate", "prior_dispatch_outcome_unknown", false, "unknown", "Verify the Home Assistant state manually; TTS cannot be generated for this request.")
		return
	}
	if decision.Disposition != ClaimReplayCompleted || decision.Result == nil {
		b.writeError(w, http.StatusInternalServerError, "claim_store_invalid", "tts_source_invalid", false, "unknown", "Inspect the local safety ledger before requesting TTS.")
		return
	}
	stored := *decision.Result
	if strings.TrimSpace(stored.Speech) == "" || strings.TrimSpace(stored.Language) == "" {
		b.writeError(w, http.StatusUnprocessableEntity, "tts_source_invalid", "assist_speech_missing", false, firstNonEmpty(stored.ActionExecuted, "not_applicable"), "The completed Home Assistant result has no safe spoken response.")
		return
	}
	result, err := b.tts.Synthesize(r.Context(), stored.Speech, tts.SynthesizeOpts{Locale: stored.Language, Format: "wav"})
	if err != nil || result == nil {
		b.writeError(w, http.StatusServiceUnavailable, "tts_unavailable", "local_tts_failed", true, "not_applicable", "Retry TTS; the Home Assistant command will not be repeated.")
		return
	}
	format := strings.ToLower(strings.TrimSpace(result.Format))
	provider := strings.TrimSpace(result.Provider)
	if len(result.Audio) == 0 || len(result.Audio) > maxTTSAudioBytes || format != "wav" || result.SampleRate <= 0 || result.SampleRate > 384000 || provider == "" || len(provider) > 128 {
		b.writeError(w, http.StatusBadGateway, "tts_response_invalid", "tts_audio_contract_invalid", false, "not_applicable", "Inspect the local TTS provider.")
		return
	}
	b.writeJSON(w, http.StatusOK, wire.TTSResponse{
		RequestID:   body.RequestID,
		AudioBase64: base64.StdEncoding.EncodeToString(result.Audio),
		Format:      format,
		SampleRate:  result.SampleRate,
		DurationMS:  result.Duration.Milliseconds(),
		Provider:    provider,
		Voice:       result.Voice,
	})
}

func allowedEventType(value string) bool {
	switch value {
	case "device.wake_detected", "voice.capture_started", "voice.capture_stopped",
		"voice.assist_result", "voice.tts_started", "voice.tts_finished":
		return true
	default:
		return false
	}
}
