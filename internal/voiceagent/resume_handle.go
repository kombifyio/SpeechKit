package voiceagent

import (
	"log/slog"
	"sync"
	"time"
)

// resumeHandleTTL bounds how long a Gemini Live session resumption handle is
// considered valid on the client. Server-side handles can expire even sooner;
// this cap limits the window of misuse if process memory is ever snapshotted.
const resumeHandleTTL = 15 * time.Minute

// resumeHandle stores a Gemini Live session resumption handle with a time-to-
// live and at-rest protection. On Windows the ciphertext is produced by DPAPI
// (CryptProtectData, user-scope) so a memory dump taken hours later cannot be
// replayed against the same session. On other platforms the handle is held in
// memory without encryption (there is no equivalent process-scoped primitive
// in the Go standard library) but the TTL still applies.
type resumeHandle struct {
	mu        sync.Mutex
	encrypted []byte
	setAt     time.Time
	now       func() time.Time // injectable for tests
}

func newResumeHandle() *resumeHandle {
	return &resumeHandle{now: time.Now}
}

// Set replaces the stored handle. An empty input clears it.
func (h *resumeHandle) Set(raw string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if raw == "" {
		h.encrypted = nil
		h.setAt = time.Time{}
		return
	}
	enc, err := protectResumeHandle([]byte(raw))
	if err != nil {
		// Protection failure should not lose the reconnect capability; fall back
		// to plain storage (TTL still applies). DPAPI failures on a healthy
		// Windows user session are rare and usually indicate a profile issue.
		slog.Warn("voiceagent: failed to protect resume handle; storing in plain memory", "err", err)
		enc = append([]byte(nil), raw...)
	}
	h.encrypted = enc
	h.setAt = h.clock()
}

// Get returns the cleartext handle if present and not expired. Expired handles
// are discarded on read. An empty return value means "no handle available".
func (h *resumeHandle) Get() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.encrypted) == 0 {
		return ""
	}
	if h.clock().Sub(h.setAt) > resumeHandleTTL {
		h.encrypted = nil
		h.setAt = time.Time{}
		return ""
	}
	plain, err := unprotectResumeHandle(h.encrypted)
	if err != nil {
		slog.Warn("voiceagent: failed to unprotect resume handle; discarding", "err", err)
		h.encrypted = nil
		h.setAt = time.Time{}
		return ""
	}
	return string(plain)
}

// Clear wipes any stored handle.
func (h *resumeHandle) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.encrypted = nil
	h.setAt = time.Time{}
}

func (h *resumeHandle) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}
