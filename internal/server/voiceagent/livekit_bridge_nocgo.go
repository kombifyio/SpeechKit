//go:build linux && !cgo

package voiceagent

import (
	"context"
	"errors"
	"time"
)

// LiveKitMediaBridgeFactory is present in non-CGO Linux builds so the server
// package still compiles. The real LiveKit media bridge uses the LiveKit PCM
// helpers, which depend on libopus through CGO.
type LiveKitMediaBridgeFactory struct {
	Issuer         *LiveKitTokenIssuer
	ConnectTimeout time.Duration
}

func NewLiveKitMediaBridgeFactory(issuer *LiveKitTokenIssuer) *LiveKitMediaBridgeFactory {
	return &LiveKitMediaBridgeFactory{Issuer: issuer}
}

func (f *LiveKitMediaBridgeFactory) Start(_ context.Context, _ MediaBridgeRequest) (MediaBridge, error) {
	return nil, errors.New("voiceagent: LiveKit media bridge requires a Linux CGO build with libopus")
}
