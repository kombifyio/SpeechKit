//go:build linux

package voiceagent

import (
	"context"
	"errors"
	"net/http"
)

// Endpoint callback authorization belongs to the application that admitted the
// selected connection. The standalone server has no application delegation
// signer; hosts can use the public responses adapter with their header provider.
func endpointTurnHeaders(_ LiveConfigFrame, _ string) (func(context.Context, []byte) (http.Header, error), error) {
	return nil, errors.New("voiceagent: endpoint callback authorization integration unavailable")
}
