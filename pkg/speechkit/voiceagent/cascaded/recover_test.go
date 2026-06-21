package cascaded

import (
	"strings"
	"testing"
	"time"
)

// TestRecoverGoroutine_SurfacesPanicAsMessage proves that a panic in a spawned
// provider goroutine is recovered (not crashing the process) and surfaced to
// the client as an error message instead of silently dying.
func TestRecoverGoroutine_SurfacesPanicAsMessage(t *testing.T) {
	p := newTestProvider(t, Deps{
		STT:    &fakeSTT{},
		Agent:  &fakeAgent{},
		Config: Config{SilenceTurnMs: 50, MinTurnMs: 50},
	})

	func() {
		defer p.recoverGoroutine("test")
		panic("simulated processor panic")
	}()

	msgs := collectMessages(t, p, 1*time.Second)
	var saw bool
	for _, m := range msgs {
		if strings.Contains(m.OutputTranscript, "internal_panic") {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("expected internal_panic message after recover; got %+v", msgs)
	}
}
