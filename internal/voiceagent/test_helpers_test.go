package voiceagent

import (
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/testutil"
)

func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	testutil.Eventually(t, timeout, 25*time.Millisecond, condition)
}

func containsState(states []State, want State) bool {
	for _, state := range states {
		if state == want {
			return true
		}
	}
	return false
}
