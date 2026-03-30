package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

type audioSessionOpener func(audio.Config) (audio.Session, error)

type reconfigurableAudioSession struct {
	mu            sync.Mutex
	cfg           audio.Config
	opener        audioSessionOpener
	session       audio.Session
	events        chan audio.Event
	levelHandler  func(float64)
	pcmHandler    func([]byte)
	pendingReopen bool
	closed        bool
	closeOnce     sync.Once
}

var _ audio.Session = (*reconfigurableAudioSession)(nil)

func newReconfigurableAudioSession(cfg audio.Config, opener audioSessionOpener) (*reconfigurableAudioSession, error) {
	if opener == nil {
		opener = audio.Open
	}

	wrapper := &reconfigurableAudioSession{
		cfg:    cfg,
		opener: opener,
		events: make(chan audio.Event, 16),
	}
	if err := wrapper.reopenLocked(nil); err != nil {
		return nil, err
	}
	return wrapper, nil
}

func (s *reconfigurableAudioSession) Start() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("audio session closed")
	}
	if s.session == nil || s.pendingReopen {
		if err := s.reopenLocked(s.session); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	session := s.session
	s.mu.Unlock()
	return session.Start()
}

func (s *reconfigurableAudioSession) Stop() ([]byte, error) {
	s.mu.Lock()
	if s.session == nil {
		s.mu.Unlock()
		return nil, nil
	}
	session := s.session
	s.mu.Unlock()

	pcm, stopErr := session.Stop()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.pendingReopen || s.session == nil || s.session.IsRunning() {
		return pcm, stopErr
	}
	reopenErr := s.reopenLocked(s.session)
	if stopErr != nil {
		return pcm, stopErr
	}
	return pcm, reopenErr
}

func (s *reconfigurableAudioSession) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session != nil && s.session.IsRunning()
}

func (s *reconfigurableAudioSession) Events() <-chan audio.Event {
	return s.events
}

func (s *reconfigurableAudioSession) SetLevelHandler(handler func(float64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.levelHandler = handler
	if s.session != nil {
		s.session.SetLevelHandler(handler)
	}
}

func (s *reconfigurableAudioSession) SetPCMHandler(handler func([]byte)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pcmHandler = handler
	if s.session != nil {
		s.session.SetPCMHandler(handler)
	}
}

func (s *reconfigurableAudioSession) ReconfigureDevice(deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("audio session closed")
	}

	nextDeviceID := strings.TrimSpace(deviceID)
	if strings.EqualFold(strings.TrimSpace(s.cfg.DeviceID), nextDeviceID) {
		return nil
	}

	s.cfg.DeviceID = nextDeviceID
	s.pendingReopen = true
	if s.session != nil && s.session.IsRunning() {
		return nil
	}
	return s.reopenLocked(s.session)
}

func (s *reconfigurableAudioSession) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		session := s.session
		s.session = nil
		s.mu.Unlock()

		if session != nil {
			closeErr = session.Close()
		}
		close(s.events)
	})
	return closeErr
}

func (s *reconfigurableAudioSession) reopenLocked(previous audio.Session) error {
	session, err := s.opener(s.cfg)
	if err != nil {
		return err
	}
	session.SetLevelHandler(s.levelHandler)
	session.SetPCMHandler(s.pcmHandler)
	s.forwardEvents(session)
	s.session = session
	s.pendingReopen = false
	if previous != nil {
		_ = previous.Close()
	}
	return nil
}

func (s *reconfigurableAudioSession) forwardEvents(session audio.Session) {
	if session == nil {
		return
	}
	events := session.Events()
	go func() {
		for event := range events {
			s.mu.Lock()
			closed := s.closed
			same := s.session == session
			s.mu.Unlock()
			if closed || !same {
				return
			}
			select {
			case s.events <- event:
			default:
			}
		}
	}()
}
