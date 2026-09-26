package deviceagent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

const (
	ClaimDispatchNew     = "dispatch_new"
	ClaimReplayCompleted = "replay_completed"
	ClaimIndeterminate   = "indeterminate"
	ClaimDigestConflict  = "digest_conflict"
	ClaimNotFound        = "not_found"

	maxJSONBodyBytes   = 64 << 10
	maxAssistTextBytes = 4096
	maxTTSAudioBytes   = 8 << 20
	minPairingTokenLen = 32
)

type HomeAssistant interface {
	Probe(context.Context) error
	Converse(context.Context, string, string) (*HomeAssistantResult, error)
	VerifyState(context.Context, string, string) error
}

type TTSSynthesizer interface {
	Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error)
	ReadyHealthCheck(context.Context) map[string]error
}

type ClaimKey struct {
	PairingID string
	RequestID string
}

type StoredResult struct {
	Status         string
	ConversationID string
	ResponseType   string
	Speech         string
	Language       string
	ErrorCode      string
	ReasonCode     string
	Retryable      bool
	ActionExecuted string
}

type ClaimDecision struct {
	Disposition string
	Result      *StoredResult
}

// ClaimLedger is a durable at-most-once ledger. Claim must commit a new row
// before returning ClaimDispatchNew. Implementations must never turn an
// existing nonterminal claim back into a dispatch decision.
type ClaimLedger interface {
	Claim(context.Context, ClaimKey, [32]byte, time.Time) (ClaimDecision, error)
	Lookup(context.Context, ClaimKey, time.Time) (ClaimDecision, error)
	Complete(context.Context, ClaimKey, [32]byte, StoredResult, time.Time) error
	MarkIndeterminate(context.Context, ClaimKey, [32]byte, string, time.Time) error
}

type DeviceBindingOptions struct {
	PairingID          string
	DeviceID           string
	RoomID             string
	Token              string
	AllowedClientCIDRs []string
}

type BridgeOptions struct {
	ServerInstanceID   string
	Bindings           []DeviceBindingOptions
	HomeAssistant      HomeAssistant
	TTS                TTSSynthesizer
	TTSReady           bool
	Claims             ClaimLedger
	Policy             *Policy
	MaxRequestAge      time.Duration
	FutureSkew         time.Duration
	ProbeTimeout       time.Duration
	StateVerifyTimeout time.Duration
	Now                func() time.Time
}

type deviceBinding struct {
	pairingID string
	deviceID  string
	roomID    string
	token     string
	allowed   []*net.IPNet
}

type Bridge struct {
	serverInstanceID   string
	bindings           map[string]deviceBinding
	ha                 HomeAssistant
	tts                TTSSynthesizer
	ttsReady           bool
	claims             ClaimLedger
	policy             *Policy
	maxRequestAge      time.Duration
	futureSkew         time.Duration
	probeTimeout       time.Duration
	stateVerifyTimeout time.Duration
	now                func() time.Time
}

func NewBridge(opts BridgeOptions) (*Bridge, error) {
	if !validBridgeID(opts.ServerInstanceID) {
		return nil, errors.New("device-agent bridge: server instance id must be a bounded stable identifier")
	}
	if opts.HomeAssistant == nil {
		return nil, errors.New("device-agent bridge: Home Assistant client is required")
	}
	if opts.TTS == nil || !opts.TTSReady {
		return nil, errors.New("device-agent bridge: ready TTS synthesizer is required")
	}
	if opts.Claims == nil {
		return nil, errors.New("device-agent bridge: durable claim ledger is required")
	}
	if opts.Policy == nil {
		return nil, errors.New("device-agent bridge: local command policy is required")
	}
	if opts.MaxRequestAge <= 0 {
		return nil, errors.New("device-agent bridge: max request age must be positive")
	}
	if opts.FutureSkew < 0 {
		return nil, errors.New("device-agent bridge: future skew must not be negative")
	}
	bindings := make(map[string]deviceBinding, len(opts.Bindings))
	pairingIDs := make(map[string]struct{}, len(opts.Bindings))
	tokens := make(map[string]struct{}, len(opts.Bindings))
	for _, raw := range opts.Bindings {
		binding, err := newDeviceBinding(raw)
		if err != nil {
			return nil, err
		}
		if _, exists := bindings[binding.deviceID]; exists {
			return nil, fmt.Errorf("device-agent bridge: duplicate device id %q", binding.deviceID)
		}
		if _, exists := pairingIDs[binding.pairingID]; exists {
			return nil, fmt.Errorf("device-agent bridge: duplicate pairing id %q", binding.pairingID)
		}
		if _, exists := tokens[binding.token]; exists {
			return nil, errors.New("device-agent bridge: pairing tokens must be unique per device")
		}
		bindings[binding.deviceID] = binding
		pairingIDs[binding.pairingID] = struct{}{}
		tokens[binding.token] = struct{}{}
	}
	if len(bindings) == 0 {
		return nil, errors.New("device-agent bridge: at least one device binding is required")
	}
	probeTimeout := opts.ProbeTimeout
	if probeTimeout <= 0 {
		probeTimeout = 2 * time.Second
	}
	if opts.StateVerifyTimeout < 0 || opts.StateVerifyTimeout > 10*time.Second {
		return nil, errors.New("device-agent bridge: state verification timeout must be between zero and ten seconds")
	}
	stateVerifyTimeout := opts.StateVerifyTimeout
	if stateVerifyTimeout == 0 {
		stateVerifyTimeout = 3 * time.Second
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Bridge{
		serverInstanceID:   strings.TrimSpace(opts.ServerInstanceID),
		bindings:           bindings,
		ha:                 opts.HomeAssistant,
		tts:                opts.TTS,
		ttsReady:           opts.TTSReady,
		claims:             opts.Claims,
		policy:             opts.Policy,
		maxRequestAge:      opts.MaxRequestAge,
		futureSkew:         opts.FutureSkew,
		probeTimeout:       probeTimeout,
		stateVerifyTimeout: stateVerifyTimeout,
		now:                now,
	}, nil
}

func (b *Bridge) Mount(mux *http.ServeMux) {
	if b == nil || mux == nil {
		return
	}
	mux.HandleFunc("/v1/device-agent/register", b.wrap(b.register))
	mux.HandleFunc("/v1/device-agent/events", b.wrap(b.events))
	mux.HandleFunc("/v1/device-agent/assist", b.wrap(b.assist))
	mux.HandleFunc("/v1/device-agent/tts", b.wrap(b.synthesize))
}
