package deviceagent

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	wire "github.com/kombifyio/SpeechKit/pkg/speechkit/deviceagent"
)

// TurnRequest is the authenticated, host-side execution contract used by
// bounded media ingress adapters. PairingToken is consumed only for a
// constant-time binding check and is never serialized or returned.
//
// InputSHA256 is optional for existing non-media callers. A media adapter must
// supply the lowercase digest it independently verified over the exact input
// bytes. The digest joins the durable HA claim, preventing a request ID from
// being reused with different audio.
type TurnRequest struct {
	RequestID    string
	SessionID    string
	CommandID    string
	DeviceID     string
	PairingID    string
	RoomID       string
	PairingToken string
	Text         string
	Locale       string
	InputSHA256  string
}

// TurnResult combines the existing durable Home Assistant result with the
// existing claim-bound local TTS result. Audio is the validated provider
// payload returned by the v1 TTS implementation (currently WAV); media
// adapters remain responsible for their own final wire encoding.
type TurnResult struct {
	Assist     wire.AssistResponse
	Audio      []byte
	Format     string
	SampleRate int
	DurationMS int64
	Provider   string
	Voice      string
}

// TurnExecutionError preserves the stable error/reason/guidance envelope from
// the existing device-agent bridge when an in-process adapter executes a turn.
type TurnExecutionError struct {
	StatusCode int
	Envelope   wire.ErrorEnvelope
}

func (e *TurnExecutionError) Error() string {
	if e == nil {
		return "device-agent turn execution failed"
	}
	return fmt.Sprintf("device-agent turn execution returned %d (%s/%s)", e.StatusCode, e.Envelope.Error.ErrorCode, e.Envelope.Error.ReasonCode)
}

type turnInputSHAKey struct{}

func withTurnInputSHA256(ctx context.Context, digest string) context.Context {
	if strings.TrimSpace(digest) == "" {
		return ctx
	}
	return context.WithValue(ctx, turnInputSHAKey{}, strings.TrimSpace(digest))
}

func turnInputSHA256(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(turnInputSHAKey{}).(string)
	return strings.TrimSpace(value)
}

// ExecuteTurn runs the real, durable local HA G0 plus claim-bound TTS path
// without opening another authority surface. It deliberately reuses the
// existing v1 handlers in-process; the four public speechkit.device_agent.v1
// routes and their JSON contracts remain unchanged.
func (b *Bridge) ExecuteTurn(ctx context.Context, request TurnRequest) (*TurnResult, error) {
	if b == nil {
		return nil, errors.New("device-agent turn execution requires a bridge")
	}
	binding, err := b.authenticateTurn(request)
	if err != nil {
		return nil, err
	}
	inputSHA := strings.TrimSpace(request.InputSHA256)
	if inputSHA != "" && !validLowerSHA256(inputSHA) {
		return nil, turnError(http.StatusUnprocessableEntity, "assist_request_invalid", "input_sha256_invalid", "no", "Supply the lowercase SHA-256 digest of the exact authenticated media payload.")
	}
	ctx = withTurnInputSHA256(ctx, inputSHA)

	var assist wire.AssistResponse
	if err := b.executeHandlerJSON(ctx, b.assist, binding, wire.AssistRequest{
		RequestID: request.RequestID,
		SessionID: request.SessionID,
		CommandID: request.CommandID,
		DeviceID:  request.DeviceID,
		RoomID:    request.RoomID,
		Text:      request.Text,
		Locale:    request.Locale,
	}, &assist); err != nil {
		return nil, err
	}

	var synthesized wire.TTSResponse
	if err := b.executeHandlerJSON(ctx, b.synthesize, binding, wire.TTSRequest{
		RequestID: request.RequestID,
		Format:    "wav",
	}, &synthesized); err != nil {
		return nil, err
	}
	audioBytes, err := base64.StdEncoding.Strict().DecodeString(synthesized.AudioBase64)
	if err != nil {
		return nil, turnError(http.StatusBadGateway, "tts_response_invalid", "tts_audio_base64_invalid", "not_applicable", "Inspect the local TTS provider.")
	}
	if len(audioBytes) == 0 || len(audioBytes) > maxTTSAudioBytes {
		return nil, turnError(http.StatusBadGateway, "tts_response_invalid", "tts_audio_contract_invalid", "not_applicable", "Inspect the local TTS provider.")
	}
	return &TurnResult{
		Assist:     assist,
		Audio:      audioBytes,
		Format:     synthesized.Format,
		SampleRate: synthesized.SampleRate,
		DurationMS: synthesized.DurationMS,
		Provider:   synthesized.Provider,
		Voice:      synthesized.Voice,
	}, nil
}

func (b *Bridge) authenticateTurn(request TurnRequest) (deviceBinding, error) {
	deviceID := strings.TrimSpace(request.DeviceID)
	binding, ok := b.bindings[deviceID]
	if !ok {
		return deviceBinding{}, turnError(http.StatusUnauthorized, "device_auth_required", "pairing_credential_invalid", "no", "Pair this device again with the local SpeechKit server.")
	}
	token := strings.TrimSpace(request.PairingToken)
	if len(token) != len(binding.token) || subtle.ConstantTimeCompare([]byte(token), []byte(binding.token)) != 1 {
		return deviceBinding{}, turnError(http.StatusUnauthorized, "device_auth_required", "pairing_credential_invalid", "no", "Pair this device again with the local SpeechKit server.")
	}
	if strings.TrimSpace(request.PairingID) != binding.pairingID {
		return deviceBinding{}, turnError(http.StatusForbidden, "device_binding_denied", "pairing_id_mismatch", "no", "Use the server-issued pairing epoch for this device.")
	}
	if strings.TrimSpace(request.RoomID) != binding.roomID {
		return deviceBinding{}, turnError(http.StatusForbidden, "device_binding_denied", "room_id_mismatch", "no", "Use the server-assigned room id for this paired device.")
	}
	return binding, nil
}

func (b *Bridge) executeHandlerJSON(
	ctx context.Context,
	handler func(http.ResponseWriter, *http.Request, deviceBinding),
	binding deviceBinding,
	requestBody any,
	responseBody any,
) error {
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://speechkit.invalid/internal-turn", bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	recorder := newExecutionResponseRecorder()
	handler(recorder, req, binding)
	if recorder.status < http.StatusOK || recorder.status >= http.StatusMultipleChoices {
		var envelope wire.ErrorEnvelope
		if err := json.Unmarshal(recorder.body.Bytes(), &envelope); err != nil {
			return fmt.Errorf("decode device-agent execution error: %w", err)
		}
		return &TurnExecutionError{StatusCode: recorder.status, Envelope: envelope}
	}
	if err := json.Unmarshal(recorder.body.Bytes(), responseBody); err != nil {
		return fmt.Errorf("decode device-agent execution result: %w", err)
	}
	return nil
}

type executionResponseRecorder struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func newExecutionResponseRecorder() *executionResponseRecorder {
	return &executionResponseRecorder{header: make(http.Header), status: http.StatusOK}
}

func (r *executionResponseRecorder) Header() http.Header { return r.header }

func (r *executionResponseRecorder) WriteHeader(status int) {
	if r.status != http.StatusOK || r.body.Len() != 0 {
		return
	}
	r.status = status
}

func (r *executionResponseRecorder) Write(p []byte) (int, error) {
	return r.body.Write(p)
}

func turnError(status int, errorCode, reasonCode, actionExecuted, guidance string) *TurnExecutionError {
	return &TurnExecutionError{StatusCode: status, Envelope: wire.ErrorEnvelope{Error: wire.BridgeError{
		ErrorCode:      errorCode,
		ReasonCode:     reasonCode,
		Retryable:      false,
		ActionExecuted: actionExecuted,
		UserGuidance:   guidance,
	}}}
}

func validLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
