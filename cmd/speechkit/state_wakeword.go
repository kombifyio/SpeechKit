package main

// State accessors for the wake-word runtime. Lifted out of state_types.go
// so the desktop_wakeword.go consumer sees a small, focused surface.
//
// Wake-word lifecycle in appState:
//
//   - setWakewordRuntime: install a freshly-built runtime; if a previous
//     instance was active, close it first to avoid leaking the audio
//     session + ONNX detector + dispatcher goroutines.
//   - takeWakewordRuntime: read the current runtime AND clear the slot
//     atomically. Used by the cleanup-stack hook (shutdown) and by the
//     restart path so the caller "owns" the close.
//   - setWakewordStatus / WakewordStatus: human-readable last-known
//     status string surfaced in /settings/state for the React panel.

func (s *appState) setWakewordRuntime(rt *desktopWakeRuntime) {
	if s == nil {
		return
	}
	s.mu.Lock()
	old := s.wakewordRuntime
	s.wakewordRuntime = rt
	tray := s.appTray
	s.mu.Unlock()

	// Close the previous runtime outside the lock — Close() can block on
	// goroutine joins (dispatcher in-flight events, audio session stop).
	if old != nil && old != rt {
		old.Close()
	}
	// Sync the tray privacy indicator. Wake-on iff we have an active runtime.
	if tray != nil {
		tray.SetWakeListening(rt != nil)
	}
}

func (s *appState) takeWakewordRuntime() *desktopWakeRuntime {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	rt := s.wakewordRuntime
	s.wakewordRuntime = nil
	tray := s.appTray
	s.mu.Unlock()
	if rt != nil && tray != nil {
		tray.SetWakeListening(false)
	}
	return rt
}

func (s *appState) currentWakewordRuntime() *desktopWakeRuntime {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wakewordRuntime
}

func (s *appState) setWakewordStatus(status string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.wakewordStatus = status
	s.mu.Unlock()
}

func (s *appState) wakewordStatusValue() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wakewordStatus
}
