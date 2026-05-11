//go:build linux

package voiceagent

import (
	"context"
	"fmt"
	"strings"
)

// MediaBridgeFactory creates the media transport sidecar for a session. The
// WebSocket stays the control channel; a bridge only carries PCM audio.
type MediaBridgeFactory interface {
	Start(ctx context.Context, req MediaBridgeRequest) (MediaBridge, error)
}

// MediaBridgeRequest is the minimum session/provider state needed to attach
// LiveKit media to an already accepted Voice Agent session.
type MediaBridgeRequest struct {
	SessionID string
	Owner     Identity
	Provider  LiveProviderAdapter
}

// MediaBridge moves provider output audio to the selected media transport and
// delivers inbound transport audio to the provider.
type MediaBridge interface {
	SendAudio([]byte) error
	Close() error
}

// LiveKitTransportSupport marks providers whose realtime APIs consume and
// emit raw PCM audio without SpeechKit-side transcoding.
type LiveKitTransportSupport interface {
	SupportsLiveKitTransport() bool
}

func normalizeMediaTransport(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", MediaTransportWebSocket:
		return MediaTransportWebSocket, nil
	case MediaTransportLiveKit:
		return MediaTransportLiveKit, nil
	default:
		return "", fmt.Errorf("unsupported media_transport %q", value)
	}
}

func providerSupportsLiveKitTransport(provider LiveProviderAdapter) bool {
	if p, ok := provider.(LiveKitTransportSupport); ok {
		return p.SupportsLiveKitTransport()
	}
	return false
}
