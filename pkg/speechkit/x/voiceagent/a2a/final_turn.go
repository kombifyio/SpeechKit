package a2a

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// finalTurnWindow bounds how many of the latest native finals a callback
// utterance may span. A vendor joins the finals of one unanswered turn (a
// caller who pauses mid-sentence); eight covers that with room to spare.
const finalTurnWindow = 8

// finalTurnWait bounds how long a callback waits for the native final it
// names. Measured on Deepgram Voice Agent: its think request
// can leave 0.6 to 1.3 s before the final transcript reaches the host on the
// media socket, so the earlier 500 ms bound refused real turns.
const finalTurnWait = 3 * time.Second

// FinalTurnBinding assigns identities to host-observed native final transcripts.
// Vendor history is not an authority: a callback binds only to text this
// session's own media stream delivered as finals. A new identical utterance has
// a new sequence; a callback retry against the same final keeps its identity.
// Without a native event id, callbacks before a new identical final are
// ambiguous and must not be promoted to another privileged turn.
type FinalTurnBinding struct {
	mu       sync.Mutex
	prefix   string
	sequence uint64
	// finals holds the latest finals (normalized text), oldest first; the last
	// one has identity sequence.
	finals   []string
	eventIDs map[string]struct{}
	changed  chan struct{}
}

// NewFinalTurnBinding scopes native final transcript identities to one voice session.
func NewFinalTurnBinding(sessionID string) *FinalTurnBinding {
	return &FinalTurnBinding{prefix: sessionID, changed: make(chan struct{}), eventIDs: make(map[string]struct{})}
}

// normalizeUtterance collapses whitespace so a vendor that joins finals with a
// single space, or trims them, still names the same observed speech.
func normalizeUtterance(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// Observe records one native final. eventID, when supplied by the provider,
// deduplicates retransmitted finals. Empty uses the native ordered event stream.
func (b *FinalTurnBinding) Observe(text, eventID string) {
	text = normalizeUtterance(text)
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
	b.finals = append(b.finals, text)
	if len(b.finals) > finalTurnWindow {
		b.finals = b.finals[len(b.finals)-finalTurnWindow:]
	}
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

// matchLocked reports whether text is the latest final or the latest finals
// joined in order (a turn the caller spoke in pieces). Caller holds mu.
func (b *FinalTurnBinding) matchLocked(text string) bool {
	if b.sequence == 0 || text == "" {
		return false
	}
	joined := ""
	for i := len(b.finals) - 1; i >= 0; i-- {
		if joined == "" {
			joined = b.finals[i]
		} else {
			joined = b.finals[i] + " " + joined
		}
		if joined == text {
			return true
		}
		if len(joined) >= len(text) {
			return false
		}
	}
	return false
}

// Resolve binds a callback utterance to the latest host-observed native final.
// It matches the latest final or the latest finals joined in order, never any
// other text, and never manufactures a turn. When the callback arrives before
// the final it names (a separate transport), it waits for that final, at most
// finalTurnWait. A final that arrives and still does not match ends the wait at
// once: the callback named a draft the vendor has already superseded (Deepgram
// sends a speculative request on its draft transcript and re-requests on the
// final), so it is refused without holding up the request that does match.
func (b *FinalTurnBinding) Resolve(ctx context.Context, text string) (string, error) {
	text = normalizeUtterance(text)
	ctx, cancel := context.WithTimeout(ctx, finalTurnWait)
	defer cancel()
	b.mu.Lock()
	arrivedAt := b.sequence
	b.mu.Unlock()
	for {
		b.mu.Lock()
		if b.matchLocked(text) {
			id := fmt.Sprintf("%s:%d", b.prefix, b.sequence)
			b.mu.Unlock()
			return id, nil
		}
		superseded := b.sequence > arrivedAt
		changed := b.changed
		b.mu.Unlock()
		if superseded {
			return "", errFinalSuperseded
		}
		select {
		case <-ctx.Done():
			return "", errFinalMissing
		case <-changed:
		}
	}
}

var (
	errFinalMissing    = errors.New("speechkit a2a: native final transcript required")
	errFinalSuperseded = errors.New("speechkit a2a: callback names no native final")
)
