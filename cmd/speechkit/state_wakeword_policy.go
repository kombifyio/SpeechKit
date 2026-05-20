package main

import "github.com/kombifyio/SpeechKit/internal/wakeword"

// setWakewordSessionPolicy installs a freshly-created AutoEndPolicy as the
// active wake-word watcher. The wake-word sidecar handler calls this
// immediately before emitting the synthesized hotkey press, so the
// input-controller and the Voice-Agent callbacks can both read the policy
// from this slot.
//
// If a previous policy is still in the slot (e.g. a second detection fired
// before the first session ended), the prior policy is closed so its timer
// goroutine releases. Slot capacity is 1 — wake-word-triggered sessions are
// not designed to overlap.
func (s *appState) setWakewordSessionPolicy(policy *wakeword.AutoEndPolicy) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prev := s.wakewordSessionPolicy
	s.wakewordSessionPolicy = policy
	s.mu.Unlock()

	if prev != nil && prev != policy {
		prev.Close()
	}
}

// currentWakewordSessionPolicy returns the active policy without clearing
// the slot. Callbacks that fire repeatedly during a session (PCM frames,
// transcript snippets) use this to push activity/transcript signals into
// the policy without removing it from the slot.
func (s *appState) currentWakewordSessionPolicy() *wakeword.AutoEndPolicy {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wakewordSessionPolicy
}

// clearWakewordSessionPolicy clears the slot and returns the policy that
// was there (if any). Used by the input controller when the auto-end
// fires, the user manually deactivates, or the session ends for any
// reason. The caller is responsible for Close()ing the returned policy.
func (s *appState) clearWakewordSessionPolicy() *wakeword.AutoEndPolicy {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	policy := s.wakewordSessionPolicy
	s.wakewordSessionPolicy = nil
	s.mu.Unlock()
	return policy
}
