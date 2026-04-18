package voiceagent

import (
	"testing"
	"time"
)

func TestResumeHandleSetGetRoundTrip(t *testing.T) {
	h := newResumeHandle()

	h.Set("abc-123")
	got := h.Get()
	if got != "abc-123" {
		t.Fatalf("Get = %q, want %q", got, "abc-123")
	}

	// Second Get must still return the same value (non-destructive).
	if h.Get() != "abc-123" {
		t.Fatalf("Get after Get returned empty; expected stable value")
	}
}

func TestResumeHandleSetEmptyClears(t *testing.T) {
	h := newResumeHandle()
	h.Set("something")
	h.Set("")
	if got := h.Get(); got != "" {
		t.Fatalf("Get after Set(\"\") = %q, want empty", got)
	}
}

func TestResumeHandleClear(t *testing.T) {
	h := newResumeHandle()
	h.Set("something")
	h.Clear()
	if got := h.Get(); got != "" {
		t.Fatalf("Get after Clear = %q, want empty", got)
	}
}

func TestResumeHandleOverwrite(t *testing.T) {
	h := newResumeHandle()
	h.Set("first")
	h.Set("second")
	if got := h.Get(); got != "second" {
		t.Fatalf("Get after overwrite = %q, want %q", got, "second")
	}
}

func TestResumeHandleExpires(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	fake := start
	h := &resumeHandle{now: func() time.Time { return fake }}

	h.Set("handle-xyz")
	if got := h.Get(); got != "handle-xyz" {
		t.Fatalf("fresh Get = %q, want %q", got, "handle-xyz")
	}

	// Within TTL: still valid.
	fake = start.Add(resumeHandleTTL - time.Second)
	if got := h.Get(); got != "handle-xyz" {
		t.Fatalf("Get just before TTL expiry = %q, want %q", got, "handle-xyz")
	}

	// Past TTL: expired, Get returns empty and internal state is cleared.
	fake = start.Add(resumeHandleTTL + time.Second)
	if got := h.Get(); got != "" {
		t.Fatalf("Get after TTL = %q, want empty", got)
	}
	// Subsequent Get still empty (no resurrection).
	fake = start.Add(resumeHandleTTL + 2*time.Second)
	if got := h.Get(); got != "" {
		t.Fatalf("Get after expiry cleared state should remain empty, got %q", got)
	}
}

func TestResumeHandleEmptyWhenUnset(t *testing.T) {
	h := newResumeHandle()
	if got := h.Get(); got != "" {
		t.Fatalf("Get on unset handle = %q, want empty", got)
	}
}
