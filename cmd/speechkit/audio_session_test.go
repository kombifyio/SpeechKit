package main

import (
	"testing"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

type testAudioSession struct {
	cfg              audio.Config
	events           chan audio.Event
	levelHandler     func(float64)
	pcmHandler       func([]byte)
	pooledPCMHandler audio.PooledPCMHandler
	closed           bool
}

func (s *testAudioSession) Start() error                                  { return nil }
func (s *testAudioSession) Stop() ([]byte, error)                         { return nil, nil }
func (s *testAudioSession) IsRunning() bool                               { return false }
func (s *testAudioSession) Events() <-chan audio.Event                    { return s.events }
func (s *testAudioSession) SetLevelHandler(fn func(float64))              { s.levelHandler = fn }
func (s *testAudioSession) SetPCMHandler(fn func([]byte))                 { s.pcmHandler = fn }
func (s *testAudioSession) SetPooledPCMHandler(fn audio.PooledPCMHandler) { s.pooledPCMHandler = fn }
func (s *testAudioSession) Close() error {
	s.closed = true
	close(s.events)
	return nil
}

func TestReconfigurableAudioSessionReopensWithNewDevice(t *testing.T) {
	var created []*testAudioSession
	opener := func(cfg audio.Config) (audio.Session, error) {
		session := &testAudioSession{cfg: cfg, events: make(chan audio.Event, 1)}
		created = append(created, session)
		return session, nil
	}

	session, err := newReconfigurableAudioSession(audio.Config{DeviceID: "one"}, opener)
	if err != nil {
		t.Fatalf("newReconfigurableAudioSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	levelCalls := 0
	session.SetLevelHandler(func(float64) { levelCalls++ })

	if err := session.ReconfigureDevice("two"); err != nil {
		t.Fatalf("ReconfigureDevice() error = %v", err)
	}

	if got, want := len(created), 2; got != want {
		t.Fatalf("created sessions = %d, want %d", got, want)
	}
	if !created[0].closed {
		t.Fatal("original session should be closed on reconfigure")
	}
	if got, want := created[1].cfg.DeviceID, "two"; got != want {
		t.Fatalf("reopened DeviceID = %q, want %q", got, want)
	}
	if created[1].levelHandler == nil {
		t.Fatal("level handler should be rebound to reopened session")
	}
}

func TestReconfigurableAudioSessionForwardsPooledPCMHandler(t *testing.T) {
	var created []*testAudioSession
	opener := func(cfg audio.Config) (audio.Session, error) {
		session := &testAudioSession{cfg: cfg, events: make(chan audio.Event, 1)}
		created = append(created, session)
		return session, nil
	}

	session, err := newReconfigurableAudioSession(audio.Config{DeviceID: "one"}, opener)
	if err != nil {
		t.Fatalf("newReconfigurableAudioSession() error = %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	called := 0
	session.SetPooledPCMHandler(func(buf []byte, release func()) {
		called++
		release()
	})

	if created[0].pooledPCMHandler == nil {
		t.Fatal("pooled handler must be installed on the underlying session")
	}

	// Simulate the underlying session invoking the handler.
	created[0].pooledPCMHandler([]byte{1, 2}, func() {})
	if called != 1 {
		t.Errorf("called = %d after one invocation; want 1", called)
	}

	// Reconfigure to a new device — the pooled handler must rebind
	// to the freshly-opened session.
	if err := session.ReconfigureDevice("two"); err != nil {
		t.Fatalf("ReconfigureDevice() error = %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created sessions = %d; want 2", len(created))
	}
	if created[1].pooledPCMHandler == nil {
		t.Fatal("pooled handler must be re-installed on the reopened session")
	}
}
