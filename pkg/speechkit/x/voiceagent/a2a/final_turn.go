package a2a

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// FinalTurnBinding assigns identities to host-observed native final transcripts.
// Vendor history is not an authority. A new identical utterance has a new
// sequence; a callback retry against the same final keeps its identity. Without
// a native event id, callbacks before a new identical final are ambiguous and
// must not be promoted to another privileged turn.
type FinalTurnBinding struct {
	mu       sync.Mutex
	prefix   string
	sequence uint64
	text     string
	eventIDs map[string]struct{}
	changed  chan struct{}
}

// NewFinalTurnBinding scopes native final transcript identities to one voice session.
func NewFinalTurnBinding(sessionID string) *FinalTurnBinding {
	return &FinalTurnBinding{prefix: sessionID, changed: make(chan struct{}), eventIDs: make(map[string]struct{})}
}

// Observe records one native final. eventID, when supplied by the provider,
// deduplicates retransmitted finals. Empty uses the native ordered event stream.
func (b *FinalTurnBinding) Observe(text, eventID string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if eventID != "" {
		if _, seen := b.eventIDs[eventID]; seen {
			return
		}
		if len(b.eventIDs) >= 256 {
			return
		}
		b.eventIDs[eventID] = struct{}{}
	}
	b.sequence++
	b.text = text
	close(b.changed)
	b.changed = make(chan struct{})
}

// Current returns the identity of the latest observed final, the one Resolve
// binds a callback for it to, or "" before the first final.
func (b *FinalTurnBinding) Current() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sequence == 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", b.prefix, b.sequence)
}

// Resolve waits briefly for native transcript delivery when callback and
// WebSocket event arrive on different transports. It never manufactures a turn.
func (b *FinalTurnBinding) Resolve(ctx context.Context, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	for {
		b.mu.Lock()
		if b.sequence > 0 && b.text == strings.TrimSpace(text) {
			id := fmt.Sprintf("%s:%d", b.prefix, b.sequence)
			b.mu.Unlock()
			return id, nil
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", errors.New("speechkit a2a: native final transcript required")
		case <-changed:
		}
	}
}
