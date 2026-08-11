package live

import "testing"

// TestSendAgentProgressOnlyWhileListening pins the delivery contract the
// External Coding Agent Bridge relies on (AI-VOICE-SPEECHKIT-TARGET.md):
// progress narration from a long-running host tool must never interrupt the
// user speaking or the model answering — it is silently a no-op outside
// StateListening, and the caller is expected to re-deliver on the next
// listening transition (see cmd/speechkit desktopAgentBridge.FlushPending).
func TestSendAgentProgressOnlyWhileListening(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   State
		wantOK  bool
		wantAny bool // whether provider.SendText should have been called
	}{
		{"listening delivers", StateListening, true, true},
		{"speaking is a silent no-op", StateSpeaking, false, false},
		{"processing is a silent no-op", StateProcessing, false, false},
		{"inactive is a silent no-op", StateInactive, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := newSessionTestProvider()
			session := NewSession(provider, Callbacks{})
			session.setState(tc.state)

			ok, err := session.SendAgentProgress("GPT is done: fixed the bug")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("delivered = %v, want %v", ok, tc.wantOK)
			}
			provider.mu.Lock()
			sent := len(provider.sentText)
			provider.mu.Unlock()
			gotAny := sent > 0
			if gotAny != tc.wantAny {
				t.Fatalf("provider.SendText called = %v, want %v (sentText=%v)", gotAny, tc.wantAny, provider.sentText)
			}
		})
	}
}

// TestSendAgentProgressEmptyTextIsNoop guards against narrating blank lines.
func TestSendAgentProgressEmptyTextIsNoop(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})
	session.setState(StateListening)

	ok, err := session.SendAgentProgress("   ")
	if err != nil || ok {
		t.Fatalf("delivered=%v err=%v, want false/nil for blank text", ok, err)
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.sentText) != 0 {
		t.Fatalf("provider.SendText called for blank text: %v", provider.sentText)
	}
}

// TestSendAgentProgressRejectedHostPromptReportsNotDelivered lets a host veto
// delivery (e.g. mid-transition) without surfacing it as an error.
func TestSendAgentProgressRejectedHostPromptReportsNotDelivered(t *testing.T) {
	provider := newSessionTestProvider()
	session := NewSession(provider, Callbacks{})
	session.setState(StateListening)
	session.callbacks.OnHostPrompt = func(HostPromptEvent) bool { return false }

	ok, err := session.SendAgentProgress("GPT is done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("a rejected host prompt must report delivered=false")
	}
}
