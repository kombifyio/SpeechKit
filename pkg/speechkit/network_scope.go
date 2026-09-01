package speechkit

// network_scope.go is the central privacy policy source for the Device-Target.
// A NetworkScope declares which network destinations the running SpeechKit
// process may contact. It is a device-side operating mode: the Server-Target
// keeps its own deployment hardening (bind address, auth, CORS) and is not
// gated by this type.
//
// The scope is enforced at backend boundaries (provider assembly, server
// connection construction, auxiliary network clients, settings write path) —
// the UI only mirrors the backend decision via stable disabled-reason IDs.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// NetworkScope selects one of the three privacy/operating modes.
type NetworkScope string

const (
	// NetworkScopeOpen is the default: every configured provider and
	// integration may be used, including public cloud endpoints.
	NetworkScopeOpen NetworkScope = "open"

	// NetworkScopeLocalNetwork permits on-device runtimes plus services
	// reachable via loopback or private/link-local addresses (e.g. Ollama on
	// another LAN machine, a self-hosted SpeechKit server). Public cloud
	// endpoints are blocked, including at dial time after DNS resolution.
	NetworkScopeLocalNetwork NetworkScope = "local_network"

	// NetworkScopeDeviceOnly permits only runtimes the SpeechKit app itself
	// manages as local child processes (whisper.cpp, managed llama-server,
	// local TTS engines, ONNX wake-word/VAD). Every external service — even
	// one on the same machine, such as a user-run Ollama — is blocked because
	// its own egress cannot be attested.
	NetworkScopeDeviceOnly NetworkScope = "device_only"
)

// ErrUnknownNetworkScope is returned for scope values outside the known set.
// Config loading fails closed on it instead of guessing.
var ErrUnknownNetworkScope = errors.New("speechkit: unknown network scope")

// Stable, localizable disabled-reason IDs. The backend attaches these to
// blocked settings/providers; UIs must render the matching catalog message
// instead of inventing copy.
const (
	NetworkScopeReasonCloudProvider   = "sk.privacy.disabled.cloud_provider"
	NetworkScopeReasonLocalService    = "sk.privacy.disabled.local_service_in_device_only"
	NetworkScopeReasonServerScope     = "sk.privacy.disabled.server_in_device_only"
	NetworkScopeReasonServerNotLocal  = "sk.privacy.disabled.server_url_not_local"
	NetworkScopeReasonAgentBridge     = "sk.privacy.disabled.agent_bridge"
	NetworkScopeReasonHomeAssistant   = "sk.privacy.disabled.home_assistant"
	NetworkScopeReasonEdgeBeta        = "sk.privacy.disabled.edge_beta"
	NetworkScopeReasonSetupTraffic    = "sk.privacy.disabled.setup_traffic"
	NetworkScopeReasonTelemetry       = "sk.privacy.disabled.telemetry"
	NetworkScopeReasonCloudAccount    = "sk.privacy.disabled.cloud_account"
	NetworkScopeReasonVoiceAgentCloud = "sk.privacy.disabled.voice_agent_cloud"
)

// ParseNetworkScope maps a raw config/API value onto a NetworkScope. The empty
// string is the backwards-compatible default (open); unknown values return
// ErrUnknownNetworkScope so callers fail closed instead of silently widening
// or narrowing the scope.
func ParseNetworkScope(raw string) (NetworkScope, error) {
	switch NetworkScope(strings.ToLower(strings.TrimSpace(raw))) {
	case "", NetworkScopeOpen:
		return NetworkScopeOpen, nil
	case NetworkScopeLocalNetwork:
		return NetworkScopeLocalNetwork, nil
	case NetworkScopeDeviceOnly:
		return NetworkScopeDeviceOnly, nil
	default:
		return "", fmt.Errorf("%w: %q (expected open, local_network, or device_only)", ErrUnknownNetworkScope, raw)
	}
}

// NormalizeNetworkScope is the runtime-safe variant of ParseNetworkScope:
// unparseable values collapse to the strictest scope so an invalid value can
// never widen network access.
func NormalizeNetworkScope(raw string) NetworkScope {
	scope, err := ParseNetworkScope(raw)
	if err != nil {
		return NetworkScopeDeviceOnly
	}
	return scope
}

// Valid reports whether s is one of the three known scopes.
func (s NetworkScope) Valid() bool {
	switch s {
	case NetworkScopeOpen, NetworkScopeLocalNetwork, NetworkScopeDeviceOnly:
		return true
	default:
		return false
	}
}

// Restricted reports whether s blocks public cloud endpoints.
func (s NetworkScope) Restricted() bool {
	return s == NetworkScopeLocalNetwork || s == NetworkScopeDeviceOnly
}

// AllowsProviderKind reports whether providers of the given kind may run under
// this scope. When blocked, the second return value is the stable disabled
// reason ID for the UI.
func (s NetworkScope) AllowsProviderKind(kind ProviderKind) (bool, string) {
	switch s {
	case NetworkScopeOpen:
		return true, ""
	case NetworkScopeLocalNetwork:
		switch kind {
		case ProviderKindLocalBuiltIn, ProviderKindLocalProvider:
			return true, ""
		default:
			return false, NetworkScopeReasonCloudProvider
		}
	default:
		// device_only — and any unknown scope value, which fails closed to
		// the strictest behaviour.
		switch kind {
		case ProviderKindLocalBuiltIn:
			return true, ""
		case ProviderKindLocalProvider:
			return false, NetworkScopeReasonLocalService
		default:
			return false, NetworkScopeReasonCloudProvider
		}
	}
}

// AllowsProfile reports whether the catalog profile may be activated under
// this scope, with the disabled reason ID when it may not.
func (s NetworkScope) AllowsProfile(p ProviderProfile) (bool, string) {
	return s.AllowsProviderKind(p.ProviderKind)
}

// EndpointClass groups outbound destinations by why SpeechKit dials them.
// The scope decides per class whether the dial is allowed and how strictly
// the URL and every resolved IP must be validated.
type EndpointClass string

const (
	// EndpointClassManagedLoopback is a child process SpeechKit spawned
	// itself and reaches over loopback (whisper-server, managed llama-server,
	// local TTS engines).
	EndpointClassManagedLoopback EndpointClass = "managed_loopback"

	// EndpointClassLocalService is an external service the user points
	// SpeechKit at (Ollama, an OpenAI-compatible endpoint, Home Assistant,
	// a SpeechKit server target).
	EndpointClassLocalService EndpointClass = "local_service"

	// EndpointClassTelemetry is the optional OTLP audit/trace exporter.
	EndpointClassTelemetry EndpointClass = "telemetry"

	// EndpointClassCloud is any public SaaS endpoint (provider APIs, the
	// kombify cloud account, update/download hosts outside setup consent).
	EndpointClassCloud EndpointClass = "cloud"
)

// EndpointPolicy is the per-scope decision for one endpoint class.
type EndpointPolicy struct {
	// Allowed reports whether this class may be dialed at all.
	Allowed bool
	// ReasonID is the stable disabled reason ID when Allowed is false.
	ReasonID string
	// Enforce reports whether Validation must be applied to the URL and to
	// every resolved IP at dial time. False in the open scope, where call
	// sites keep their pre-existing endpoint-specific validation.
	Enforce bool
	// Validation is the netsec option set to enforce when Enforce is true.
	Validation netsec.ValidationOptions
}

// EndpointPolicyFor returns the dial policy for an endpoint class under this
// scope. Unknown scopes fail closed to device_only behaviour.
func (s NetworkScope) EndpointPolicyFor(class EndpointClass) EndpointPolicy {
	if s == NetworkScopeOpen {
		return EndpointPolicy{Allowed: true}
	}
	localOnly := netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		AllowHTTP:     true,
		RequireLocal:  true,
	}
	loopbackOnly := netsec.ValidationOptions{
		AllowLoopback: true,
		AllowHTTP:     true,
		RequireLocal:  true,
	}
	if s == NetworkScopeLocalNetwork {
		switch class {
		case EndpointClassManagedLoopback:
			return EndpointPolicy{Allowed: true, Enforce: true, Validation: loopbackOnly}
		case EndpointClassLocalService:
			return EndpointPolicy{Allowed: true, Enforce: true, Validation: localOnly}
		case EndpointClassTelemetry:
			return EndpointPolicy{Allowed: true, Enforce: true, Validation: localOnly}
		default:
			return EndpointPolicy{Allowed: false, ReasonID: NetworkScopeReasonCloudProvider}
		}
	}
	// device_only (and unknown scopes, failing closed).
	switch class {
	case EndpointClassManagedLoopback:
		return EndpointPolicy{Allowed: true, Enforce: true, Validation: loopbackOnly}
	case EndpointClassLocalService:
		return EndpointPolicy{Allowed: false, ReasonID: NetworkScopeReasonLocalService}
	case EndpointClassTelemetry:
		return EndpointPolicy{Allowed: false, ReasonID: NetworkScopeReasonTelemetry}
	default:
		return EndpointPolicy{Allowed: false, ReasonID: NetworkScopeReasonCloudProvider}
	}
}
