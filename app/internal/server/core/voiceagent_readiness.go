//go:build linux

package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// The bridge owns Receive exclusively during Connect; the adapter pumps start
// afterward. Preserve unsolicited normalized control/media until the provider
// acknowledges configuration, then deliver it through the ordinary receive path.
func waitVoiceProviderReady(ctx context.Context, receive func(context.Context) (*live.LiveMessage, error)) ([]*live.LiveMessage, error) {
	var pending []*live.LiveMessage
	bufferedBytes := 0
	for {
		msg, err := receive(ctx)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			continue
		}
		if msg.EventType == live.LiveEventSessionReady {
			return pending, nil
		}
		if msg.GoAway || len(pending) >= 64 {
			return nil, fmt.Errorf("voiceagent: %w", live.ErrSessionNotReady)
		}
		encoded, err := json.Marshal(msg)
		if err != nil || len(encoded) > (4<<20)-bufferedBytes {
			return nil, fmt.Errorf("voiceagent: %w", live.ErrSessionNotReady)
		}
		bufferedBytes += len(encoded)
		pending = append(pending, msg)
	}
}
