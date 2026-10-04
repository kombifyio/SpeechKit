//go:build linux

package middleware

import (
	"sync"
	"time"
)

// edgeReplayGuardCapacity bounds the nonces held inside one freshness window.
// When it is full after dropping expired entries, new envelopes are refused
// rather than admitted unchecked.
const edgeReplayGuardCapacity = 1 << 17

// edgeReplayGuard remembers verified envelope nonces until their timestamp
// leaves the freshness window. It is process-local: a deployment with more
// than one server instance needs a shared store or sticky routing for the
// guarantee to hold across instances.
type edgeReplayGuard struct {
	mu       sync.Mutex
	seen     map[string]time.Time
	capacity int
}

func newEdgeReplayGuard(capacity int) *edgeReplayGuard {
	return &edgeReplayGuard{seen: make(map[string]time.Time), capacity: capacity}
}

// claim records key until expires and reports whether it was unused.
func (g *edgeReplayGuard) claim(key string, expires, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until, ok := g.seen[key]; ok && until.After(now) {
		return false
	}
	if len(g.seen) >= g.capacity {
		for k, until := range g.seen {
			if !until.After(now) {
				delete(g.seen, k)
			}
		}
		if len(g.seen) >= g.capacity {
			return false
		}
	}
	g.seen[key] = expires
	return true
}
