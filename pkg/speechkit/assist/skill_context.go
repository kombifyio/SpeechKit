package assist

import (
	"sync"
	"time"
)

// SkillContext is the multi-turn state container. When a skill returns a
// ToolResult with FollowupNeeded=true the Service stores its Intent + state
// under the request's SessionKey. On the user's next transcript the Service
// reuses the stored Intent rather than running the matcher again — that lets
// a Timer skill ask "for how long?" and treat the next utterance as the
// duration answer.
type SkillContext struct {
	Intent    string
	State     map[string]string
	ExpiresAt time.Time
}

// SkillContextStore is the storage contract for multi-turn follow-ups.
// InMemorySkillContextStore is the shipped implementation; satellite
// topologies can swap in a shared backing store.
//
// Scope: keyed by the host's SessionKey (a user id, or a per-device or
// per-wake-word session id). Multi-tenant installations must include the
// tenant in the key.
type SkillContextStore interface {
	// Get returns the active context for a key. The ok return is false when
	// no context exists or it has expired.
	Get(key string) (SkillContext, bool)
	// Set stores or replaces the context. ExpiresAt is set by the store
	// using its configured TTL — callers do not set it.
	Set(key, intent string, state map[string]string)
	// Clear removes any active context for the key. Idempotent.
	Clear(key string)
}

// InMemorySkillContextStore is the default SkillContextStore: a
// mutex-guarded map that prunes expired entries lazily on every Get, so no
// background goroutine has to be lifecycled by the host. Every method is safe
// for concurrent use and on a nil receiver.
type InMemorySkillContextStore struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	m   map[string]SkillContext
}

// NewInMemorySkillContextStore returns a ready store. A zero or negative ttl
// falls back to 60 seconds. The clock override is for tests; nil uses
// time.Now.
func NewInMemorySkillContextStore(ttl time.Duration, now func() time.Time) *InMemorySkillContextStore {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &InMemorySkillContextStore{ttl: ttl, now: now, m: map[string]SkillContext{}}
}

// Get returns the active context for key, after lazily pruning an expired
// entry.
func (s *InMemorySkillContextStore) Get(key string) (SkillContext, bool) {
	if s == nil || key == "" {
		return SkillContext{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, ok := s.m[key]
	if !ok {
		return SkillContext{}, false
	}
	if s.now().After(ctx.ExpiresAt) {
		delete(s.m, key)
		return SkillContext{}, false
	}
	return ctx, true
}

// Set stores a copy of state for key; ExpiresAt is now + ttl.
func (s *InMemorySkillContextStore) Set(key, intent string, state map[string]string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stateCopy := make(map[string]string, len(state))
	for k, v := range state {
		stateCopy[k] = v
	}
	s.m[key] = SkillContext{
		Intent:    intent,
		State:     stateCopy,
		ExpiresAt: s.now().Add(s.ttl),
	}
}

// Clear removes any context stored under key. Safe to call when no entry
// exists.
func (s *InMemorySkillContextStore) Clear(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// Len reports how many entries are currently tracked, expired ones
// included until their next Get. Exported for tests and diagnostics.
func (s *InMemorySkillContextStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}
