package gemini

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/genai"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// overlapSession records whether two writes were ever in flight at once; the
// real genai session writes to a gorilla WebSocket, which panics on that.
type overlapSession struct {
	inFlight atomic.Int32
	overlap  atomic.Bool
}

func (s *overlapSession) write() error {
	if s.inFlight.Add(1) > 1 {
		s.overlap.Store(true)
	}
	time.Sleep(time.Millisecond)
	s.inFlight.Add(-1)
	return nil
}
func (s *overlapSession) SendRealtimeInput(genai.LiveRealtimeInput) error    { return s.write() }
func (s *overlapSession) SendToolResponse(genai.LiveToolResponseInput) error { return s.write() }
func (s *overlapSession) Receive() (*genai.LiveServerMessage, error)         { select {} }
func (s *overlapSession) Close() error                                       { return nil }

// Regression: a tool response sent from its own goroutine (registered-agent
// delegation, tool bridge) must not write concurrently with streamed audio.
func TestSessionWritesAreSerialised(t *testing.T) {
	session := &overlapSession{}
	p := &Provider{session: session}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				_ = p.SendAudio([]byte{0, 0})
			} else {
				_ = p.SendToolResponse(live.ToolResponse{ID: "call", Name: "tool"})
			}
		}()
	}
	wg.Wait()
	if session.overlap.Load() {
		t.Fatal("concurrent session writes reached the WebSocket")
	}
}
