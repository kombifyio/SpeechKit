package assist

import (
	"sync"
	"time"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// SkillContext is the v0.38.0 multi-turn state container. When a
// skill returns a ToolResult with FollowupNeeded=true the pipeline
// stores its Intent + state under a per-user key. On the user's
// next transcript the pipeline reuses the stored Intent rather than
// running fresh keyword routing — that lets a Timer skill ask "for
// how long?" and treat the next utterance as the duration answer.
//
// Threading: every method is safe for concurrent use. The TTL prune
// runs inline on every Get to avoid a background goroutine the
// caller would otherwise have to lifecycle.
//
// Scope: keyed by UserID (anonymous "session" for un-auth flows).
// Multi-tenant installations must include OrgID in the key. The
// constructor lets callers pick the key shape.
type SkillContext struct {
	Intent    shortcuts.Intent
	State     map[string]string
	ExpiresAt time.Time
}

// SkillContextStore is the storage contract. v0.38.0 ships only the
// in-memory implementation but the interface lets future deployments
// swap in a Redis or shared-store backing for satellite topologies.
type SkillContextStore interface {
	// Get returns the active context for a key. The ok return is
	// false when no context exists or it has expired.
	Get(key string) (SkillContext, bool)

	// Set stores or replaces the context. ExpiresAt is set by the
	// store using its configured TTL — callers do not set it.
	Set(key string, intent shortcuts.Intent, state map[string]string)

	// Clear removes any active context for the key. Idempotent.
	Clear(key string)
}

// InMemorySkillContextStore is the default implementation. It uses a
// mutex-guarded map and prunes expired entries lazily on every Get.
type InMemorySkillContextStore struct {
	mu  sync.Mutex
	ttl time.Duration
	now func() time.Time
	m   map[string]SkillContext
}

// NewInMemorySkillContextStore returns a ready store. A zero or
// negative ttl falls back to 60 seconds. The clock override is for
// tests; nil uses time.Now.
func NewInMemorySkillContextStore(ttl time.Duration, now func() time.Time) *InMemorySkillContextStore {
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	return &InMemorySkillContextStore{ttl: ttl, now: now, m: map[string]SkillContext{}}
}

// Get returns the active context for key, after lazily pruning any
// expired entry.
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

// Set stores a context for key. ExpiresAt is `now + ttl`.
func (s *InMemorySkillContextStore) Set(key string, intent shortcuts.Intent, state map[string]string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stateCopy := map[string]string{}
	for k, v := range state {
		stateCopy[k] = v
	}
	s.m[key] = SkillContext{
		Intent:    intent,
		State:     stateCopy,
		ExpiresAt: s.now().Add(s.ttl),
	}
}

// Clear removes any context stored under key. Safe to call when no
// entry exists.
func (s *InMemorySkillContextStore) Clear(key string) {
	if s == nil || key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// Len reports how many entries are currently tracked. Exported for
// tests + diagnostics; production code should not depend on it.
func (s *InMemorySkillContextStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.m)
}
