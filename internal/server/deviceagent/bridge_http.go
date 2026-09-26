package deviceagent

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

func (b *Bridge) requireBoundDevice(w http.ResponseWriter, binding deviceBinding, deviceID, roomID string) bool {
	if strings.TrimSpace(deviceID) != binding.deviceID {
		b.writeError(w, http.StatusForbidden, "device_binding_denied", "device_id_mismatch", false, "no", "Use the device id assigned during local pairing.")
		return false
	}
	if strings.TrimSpace(roomID) != binding.roomID {
		b.writeError(w, http.StatusForbidden, "device_binding_denied", "room_id_mismatch", false, "no", "Use the server-assigned room id for this paired device.")
		return false
	}
	return true
}

func (b *Bridge) decode(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close() //nolint:errcheck // bounded request body is fully consumed
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		b.writeError(w, http.StatusBadRequest, "invalid_json", "request_body_invalid", false, "no", "Send one JSON object containing only documented fields.")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		b.writeError(w, http.StatusBadRequest, "invalid_json", "multiple_json_values", false, "no", "Send exactly one JSON object.")
		return false
	}
	return true
}

func (b *Bridge) writeStoredResult(w http.ResponseWriter, requestID string, result StoredResult, replayed bool) {
	if result.Status == "denied" && result.Speech == "" {
		b.writeError(w, http.StatusBadGateway, firstNonEmpty(result.ErrorCode, "home_assistant_rejected"), firstNonEmpty(result.ReasonCode, "ha_request_rejected"), result.Retryable, firstNonEmpty(result.ActionExecuted, "no"), "Review Home Assistant permissions or command details before using a new request id.")
		return
	}
	b.writeJSON(w, http.StatusOK, wire.AssistResponse{
		Status:         result.Status,
		RequestID:      requestID,
		ConversationID: result.ConversationID,
		ResponseType:   result.ResponseType,
		Speech:         result.Speech,
		ActionExecuted: result.ActionExecuted,
		Replayed:       replayed,
		ErrorCode:      result.ErrorCode,
		ReasonCode:     result.ReasonCode,
		Retryable:      result.Retryable,
		UserGuidance:   resultGuidance(result),
	})
}

func (b *Bridge) writeError(w http.ResponseWriter, status int, errorCode, reasonCode string, retryable bool, actionExecuted, guidance string) {
	b.writeJSON(w, status, wire.ErrorEnvelope{Error: wire.BridgeError{
		ErrorCode:      errorCode,
		ReasonCode:     reasonCode,
		Retryable:      retryable,
		ActionExecuted: actionExecuted,
		UserGuidance:   guidance,
	}})
}

func (*Bridge) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func validateUUIDv7Window(raw string, now time.Time, maxAge, futureSkew time.Duration) string {
	id, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil || id.Version() != 7 {
		return "request_id_not_uuidv7"
	}
	seconds, nanoseconds := id.Time().UnixTime()
	issuedAt := time.Unix(seconds, nanoseconds)
	if issuedAt.After(now.Add(futureSkew)) {
		return "request_id_from_future"
	}
	if issuedAt.Before(now.Add(-maxAge)) {
		return "request_id_too_old"
	}
	return ""
}

func boundedString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func resultGuidance(result StoredResult) string {
	if result.Status == "denied" {
		return "Follow the Home Assistant response; do not ask a general LLM to reinterpret the command."
	}
	return ""
}
