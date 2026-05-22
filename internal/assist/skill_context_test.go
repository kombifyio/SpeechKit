package assist

import (
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

func TestSkillContext_SetGetClear(t *testing.T) {
	store := NewInMemorySkillContextStore(time.Minute, nil)
	store.Set("alice", shortcuts.IntentTimer, map[string]string{"awaiting": "duration"})

	got, ok := store.Get("alice")
	if !ok {
		t.Fatal("expected context to exist after Set")
	}
	if got.Intent != shortcuts.IntentTimer {
		t.Errorf("Intent = %v, want IntentTimer", got.Intent)
	}
	if got.State["awaiting"] != "duration" {
		t.Errorf("State = %v, want awaiting=duration", got.State)
	}

	store.Clear("alice")
	if _, ok := store.Get("alice"); ok {
		t.Error("expected Clear to remove context")
	}
}

func TestSkillContext_TTLExpiry(t *testing.T) {
	now := time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC)
	store := NewInMemorySkillContextStore(30*time.Second, func() time.Time { return now })
	store.Set("bob", shortcuts.IntentReminder, nil)

	// 29s later — still alive.
	now = now.Add(29 * time.Second)
	if _, ok := store.Get("bob"); !ok {
		t.Error("expected entry to still be alive at 29s")
	}

	// 31s later — expired.
	now = now.Add(2 * time.Second)
	if _, ok := store.Get("bob"); ok {
		t.Error("expected entry to expire after TTL")
	}
}

func TestSkillContext_DefaultsAreSafe(t *testing.T) {
	// Zero TTL → defaults to 60s. Nil clock → defaults to time.Now.
	store := NewInMemorySkillContextStore(0, nil)
	store.Set("c", shortcuts.IntentTimer, map[string]string{"k": "v"})
	if _, ok := store.Get("c"); !ok {
		t.Error("default config should still store + retrieve")
	}
}

func TestSkillContext_StateIsCopied(t *testing.T) {
	store := NewInMemorySkillContextStore(time.Minute, nil)
	s := map[string]string{"a": "1"}
	store.Set("user", shortcuts.IntentMath, s)

	// Mutate the caller's map; the store must not see it.
	s["a"] = "2"

	got, _ := store.Get("user")
	if got.State["a"] != "1" {
		t.Errorf("State was not defensively copied: %v", got.State)
	}
}

func TestSkillContext_EmptyKeyIsIgnored(t *testing.T) {
	store := NewInMemorySkillContextStore(time.Minute, nil)
	store.Set("", shortcuts.IntentTimer, nil)
	if store.Len() != 0 {
		t.Error("empty key should not store anything")
	}
	if _, ok := store.Get(""); ok {
		t.Error("Get of empty key should return ok=false")
	}
}

func TestSkillContext_NilReceiverIsSafe(t *testing.T) {
	var store *InMemorySkillContextStore
	store.Set("x", shortcuts.IntentTimer, nil)
	if _, ok := store.Get("x"); ok {
		t.Error("nil receiver Get must return ok=false, not panic")
	}
	store.Clear("x")
	if got := store.Len(); got != 0 {
		t.Errorf("nil receiver Len = %d, want 0", got)
	}
}
