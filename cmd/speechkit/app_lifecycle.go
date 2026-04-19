package main

func (s *appState) beginShutdown() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.shuttingDown = true
	s.mu.Unlock()
}

func (s *appState) isShuttingDown() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shuttingDown
}

func (s *appState) shouldHideWindowOnClose() bool {
	return !s.isShuttingDown()
}
