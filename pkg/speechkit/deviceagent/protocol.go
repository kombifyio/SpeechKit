package deviceagent

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// CurrentProtocolVersion is the only version emitted or accepted by the
	// credential-minimal local bridge. The v0 protocol constant was removed
	// in v0.68.0; v0 clients never had server-owned pairing or authority
	// guarantees.
	CurrentProtocolVersion   = "speechkit.device_agent.v1"
	defaultLocale            = "de-DE"
	defaultUserAgent         = "speechkit-device-agent/1.0"
	minimumPairingTokenBytes = 32
	maxJSONResponseBytes     = 2 << 20
	maxTTSResponseBytes      = 12 << 20
	maxErrorResponseBytes    = 64 << 10

	// CapabilityReady marks a device facet or server-side bridge as proven
	// ready. [Agent.RunFakeAssistCycle] requires both bridges in the
	// [RegistrationAck] to report it.
	CapabilityReady = "ready"
	// CapabilityUnavailable marks a server-side bridge whose readiness probe
	// failed; [CapabilityState.ReasonCode] names the stable cause.
	CapabilityUnavailable = "unavailable"
	// CapabilityUnverified marks a device facet whose readiness has not been
	// proven. It is the default for an empty [Wakeword.Status] or
	// [Health.Status].
	CapabilityUnverified = "unverified"
	// ServerInstanceHeader is the response header carrying the server's stable
	// instance ID. The agent requires it on every response and rejects any
	// value other than [Config.ExpectedServerInstanceID].
	ServerInstanceHeader = "X-SpeechKit-Server-Instance-ID"
)

// Sentinel errors returned by [New], [Agent.Register],
// [Agent.RunFakeAssistCycle], and the underlying transport. Match them with
// [errors.Is]; several are wrapped with request-specific detail.
var (
	// ErrMissingServerURL is returned by [New] when [Config.ServerURL] is empty
	// or is not a valid local origin. The URL validation error is wrapped
	// alongside it, so [errors.Is] also matches causes such as
	// [ErrInsecureServerTransport].
	ErrMissingServerURL = errors.New("speechkit deviceagent: server_url is required")
	// ErrMissingPairingToken is returned by [New] when [Config.PairingToken] is
	// blank.
	ErrMissingPairingToken = errors.New("speechkit deviceagent: pairing_token is required")
	// ErrPairingTokenTooShort is returned by [New] when the trimmed pairing
	// token is shorter than 32 bytes.
	ErrPairingTokenTooShort = errors.New("speechkit deviceagent: pairing_token must contain at least 32 bytes")
	// ErrPairingTokenInvalid is returned by [New] when the pairing token is
	// longer than 512 bytes or contains characters outside the bearer-token
	// alphabet of letters, digits, and "-._~+/=".
	ErrPairingTokenInvalid = errors.New("speechkit deviceagent: pairing_token must be a bounded bearer credential")
	// ErrMissingExpectedServerInstance is returned by [New] when
	// [Config.ExpectedServerInstanceID] is blank.
	ErrMissingExpectedServerInstance = errors.New("speechkit deviceagent: expected_server_instance_id is required")
	// ErrMissingExpectedPairingID is returned by [New] when
	// [Config.ExpectedPairingID] is blank.
	ErrMissingExpectedPairingID = errors.New("speechkit deviceagent: expected_pairing_id is required")
	// ErrMissingAssistText is returned by [Agent.RunFakeAssistCycle] when
	// [CycleOptions.Text] is blank.
	ErrMissingAssistText = errors.New("speechkit deviceagent: assist text is required")
	// ErrMissingCommandID is returned by [Agent.RunFakeAssistCycle] when
	// [CycleOptions.CommandID] is blank.
	ErrMissingCommandID = errors.New("speechkit deviceagent: command_id is required")
	// ErrServerIdentityMismatch is returned when the [ServerInstanceHeader] of
	// any response, or [RegistrationAck.ServerInstanceID], differs from
	// [Config.ExpectedServerInstanceID].
	ErrServerIdentityMismatch = errors.New("speechkit deviceagent: server instance identity mismatch")
	// ErrServerIdentityMissing is returned when a response carries no
	// [ServerInstanceHeader].
	ErrServerIdentityMissing = errors.New("speechkit deviceagent: server instance identity header is missing")
	// ErrPairingIdentityMismatch is returned by [Agent.Register] when
	// [RegistrationAck.PairingID] differs from [Config.ExpectedPairingID],
	// typically because the server rotated the pairing epoch.
	ErrPairingIdentityMismatch = errors.New("speechkit deviceagent: pairing epoch identity mismatch")
	// ErrAssistResponseMismatch is returned when an Assist or TTS response does
	// not correlate with its request: a different request_id, an unsupported
	// status, or an unsupported action_executed value.
	ErrAssistResponseMismatch = errors.New("speechkit deviceagent: assist response does not match the request")
	// ErrResponseTooLarge is returned when a response body exceeds the
	// per-endpoint limit: 2 MiB for JSON, 12 MiB for TTS audio, and 64 KiB for
	// error responses.
	ErrResponseTooLarge = errors.New("speechkit deviceagent: server response exceeds the protocol limit")
	// ErrInsecureServerTransport is returned by [New], wrapped in
	// [ErrMissingServerURL], when the server URL uses plaintext http for a host
	// other than localhost or a loopback address.
	ErrInsecureServerTransport = errors.New("speechkit deviceagent: plaintext HTTP is allowed only for a loopback server")
	// ErrHomeAssistantBridgeUnavailable is returned by
	// [Agent.RunFakeAssistCycle] when the registration ack does not report the
	// Home Assistant bridge as [CapabilityReady]; the server's reason code is
	// appended.
	ErrHomeAssistantBridgeUnavailable = errors.New("speechkit deviceagent: Home Assistant bridge is not ready")
	// ErrTTSBridgeUnavailable is returned by [Agent.RunFakeAssistCycle] when the
	// registration ack does not report the TTS bridge as [CapabilityReady]; the
	// server's reason code is appended.
	ErrTTSBridgeUnavailable = errors.New("speechkit deviceagent: TTS bridge is not ready")
)

// DeviceDescriptor identifies the paired device as asserted in
// [Registration]. AgentID names the agent instance; DeviceID and RoomID are
// the paired identity the server binds every request to. [New] fills empty
// identity fields, device kinds, and wake-word status with defaults.
type DeviceDescriptor struct {
	AgentID       string      `json:"agent_id"`
	DeviceID      string      `json:"device_id"`
	DisplayName   string      `json:"display_name,omitempty"`
	RoomID        string      `json:"room_id"`
	CaptureDevice AudioDevice `json:"capture_device"`
	OutputDevice  AudioDevice `json:"output_device"`
	Wakeword      Wakeword    `json:"wakeword"`
}

// AudioDevice describes one capture or playback device. Kind is
// "microphone" or "speaker" and Transport is a free-form attachment label
// such as "local".
type AudioDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind"` // microphone | speaker
	Transport string `json:"transport,omitempty"`
}

// Wakeword reports the device's local wake-word detector: whether it is
// enabled, the configured phrase, the detector backend, and a Status of
// [CapabilityReady] or [CapabilityUnverified].
type Wakeword struct {
	Enabled bool   `json:"enabled"`
	Phrase  string `json:"phrase,omitempty"`
	Backend string `json:"backend,omitempty"`
	Status  string `json:"status"`
}

// Capabilities are device-reported facts. Pairing and server-side bridge
// readiness are deliberately absent: only the server can attest those.
type Capabilities struct {
	Dictation     bool `json:"dictation"`
	Assist        bool `json:"assist"`
	VoiceAgent    bool `json:"voice_agent"`
	WakewordLocal bool `json:"wakeword_local"`
	TTS           bool `json:"tts"`
	BargeIn       bool `json:"barge_in"`
}

// Health is the device-reported readiness summary sent with [Registration].
// Status condenses the three readiness flags into [CapabilityReady] or
// [CapabilityUnverified]; [New] defaults an empty Status to unverified.
type Health struct {
	Status       string `json:"status"`
	CaptureReady bool   `json:"capture_ready"`
	OutputReady  bool   `json:"output_ready"`
	WakeReady    bool   `json:"wake_ready"`
}

// Registration is the device-asserted half of the v1 handshake. Pairing state
// is never part of it; the server attests pairing in RegistrationAck.
type Registration struct {
	Version      string           `json:"version"`
	RegisteredAt time.Time        `json:"registered_at"`
	Device       DeviceDescriptor `json:"device"`
	Capabilities Capabilities     `json:"capabilities"`
	Health       Health           `json:"health"`
}

// CapabilityState is the server-attested readiness of one server-side
// bridge. Status is [CapabilityReady] or [CapabilityUnavailable]; ReasonCode
// names the stable cause when the bridge is not ready.
type CapabilityState struct {
	Status     string `json:"status"`
	ReasonCode string `json:"reason_code,omitempty"`
}

// BridgeCapabilities lists the server-side bridges a paired device may use,
// as attested by the server in [RegistrationAck].
type BridgeCapabilities struct {
	HomeAssistant CapabilityState `json:"home_assistant"`
	TTS           CapabilityState `json:"tts"`
}

// RegistrationAck is the server's answer to [Registration]. Status is
// "paired" for an accepted device; PairingID and ServerInstanceID must equal
// the agent's expected identities or [Agent.Register] fails closed.
// ServerTime is the server's UTC clock in RFC 3339 form when provided.
type RegistrationAck struct {
	Status           string             `json:"status"`
	PairingID        string             `json:"pairing_id"`
	ServerInstanceID string             `json:"server_instance_id"`
	ServerTime       string             `json:"server_time,omitempty"`
	Capabilities     BridgeCapabilities `json:"capabilities"`
}

// Event is one lifecycle event a device publishes to the server. Type is
// one of device.wake_detected, voice.capture_started,
// voice.capture_stopped, voice.assist_result, voice.tts_started, and
// voice.tts_finished; the server rejects any other type. [Agent.Publish]
// fills empty Surface, Mode, DeviceID, RoomID, CapturePolicy, and At with
// the agent's defaults and always sets Transport to "local_http".
type Event struct {
	Type          string            `json:"type"`
	Surface       string            `json:"surface"`
	Mode          string            `json:"mode"`
	RequestID     string            `json:"request_id,omitempty"`
	SessionID     string            `json:"session_id,omitempty"`
	DeviceID      string            `json:"device_id"`
	RoomID        string            `json:"room_id"`
	CapturePolicy string            `json:"capture_policy,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	Text          string            `json:"text,omitempty"`
	SpeakText     string            `json:"speak_text,omitempty"`
	ReasonCode    string            `json:"reason_code,omitempty"`
	At            time.Time         `json:"at"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// EventAck acknowledges a published [Event]; the server reports Status
// "accepted".
type EventAck struct {
	Status string `json:"status"`
}

// AssistRequest is one Assist turn for the local Home Assistant bridge.
// RequestID is a UUIDv7 idempotency key, CommandID names the
// server-configured local rule to run, and Locale is a BCP-47 tag.
type AssistRequest struct {
	RequestID string `json:"request_id"`
	SessionID string `json:"session_id"`
	CommandID string `json:"command_id"`
	DeviceID  string `json:"device_id"`
	RoomID    string `json:"room_id"`
	Text      string `json:"text"`
	Locale    string `json:"locale"`
}

// AssistResponse is the bridge's outcome for an [AssistRequest]. Status is
// "success" or "denied"; Speech is the text to speak; ConversationID and
// ResponseType echo Home Assistant's conversation result; Replayed reports
// that the stored outcome of an earlier request with the same RequestID was
// returned instead of dispatching again; ErrorCode, ReasonCode, Retryable,
// and UserGuidance explain a denial. [Agent] accepts only "yes", "no", or
// "not_applicable" in ActionExecuted.
type AssistResponse struct {
	Status         string `json:"status"`
	RequestID      string `json:"request_id"`
	ConversationID string `json:"conversation_id,omitempty"`
	ResponseType   string `json:"response_type,omitempty"`
	Speech         string `json:"speech,omitempty"`
	ActionExecuted string `json:"action_executed"` // yes | no | unknown
	Replayed       bool   `json:"replayed"`
	ErrorCode      string `json:"error_code,omitempty"`
	ReasonCode     string `json:"reason_code,omitempty"`
	Retryable      bool   `json:"retryable"`
	UserGuidance   string `json:"user_guidance,omitempty"`
}

// TTSRequest asks the server to synthesize the spoken response of a
// completed Assist turn. RequestID must be that turn's request_id; Format
// selects the container and only "wav" is supported.
type TTSRequest struct {
	RequestID string `json:"request_id"`
	Format    string `json:"format"`
}

// TTSResponse carries synthesized audio for a [TTSRequest]. AudioBase64 is
// the standard-base64 WAV payload, SampleRate is in hertz, DurationMS is the
// audio length in milliseconds, and Provider and Voice name the local TTS
// engine and voice that produced it.
type TTSResponse struct {
	RequestID   string `json:"request_id"`
	AudioBase64 string `json:"audio_base64"`
	Format      string `json:"format"`
	SampleRate  int    `json:"sample_rate"`
	DurationMS  int64  `json:"duration_ms"`
	Provider    string `json:"provider"`
	Voice       string `json:"voice,omitempty"`
}

// ErrorEnvelope is the JSON body of a non-2xx device-agent response,
// wrapping a single [BridgeError].
type ErrorEnvelope struct {
	Error BridgeError `json:"error"`
}

// BridgeError is the server's structured, stable description of a rejected
// request. ErrorCode classifies the failure and ReasonCode names the
// specific cause; Retryable reports whether the same request may be
// retried; ActionExecuted is "yes", "no", "not_applicable", or "unknown";
// UserGuidance is a speakable next step.
type BridgeError struct {
	ErrorCode      string `json:"error_code"`
	ReasonCode     string `json:"reason_code"`
	Retryable      bool   `json:"retryable"`
	ActionExecuted string `json:"action_executed"`
	UserGuidance   string `json:"user_guidance"`
}

// HTTPError is returned by [Agent] calls when the server answers with a
// non-2xx status. Envelope holds the decoded [ErrorEnvelope] when the body
// was structured JSON and stays zero otherwise; the raw body is never
// echoed into the error text. Match it with [errors.As].
type HTTPError struct {
	Method     string
	Path       string
	StatusCode int
	Envelope   ErrorEnvelope
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "speechkit deviceagent: nil HTTP error"
	}
	code := strings.TrimSpace(e.Envelope.Error.ErrorCode)
	if code != "" {
		return fmt.Sprintf("%s %s returned %d (%s/%s)", e.Method, e.Path, e.StatusCode, code, e.Envelope.Error.ReasonCode)
	}
	return fmt.Sprintf("%s %s returned %d without a structured device-agent error", e.Method, e.Path, e.StatusCode)
}
