package main

import (
	"context"
	"sync"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

type modeHotkeyReconfigurer interface {
	ReconfigureModes(dictate, agent []uint32)
}

type dualHotkeyManager struct {
	mu         sync.RWMutex
	dictate    *hotkey.Manager
	agent      *hotkey.Manager
	dictateVKs []uint32
	agentVKs   []uint32
	events     chan hotkey.Event
	activeMode func() string
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stopOnce   sync.Once
}

func newDualHotkeyManager(dictate, agent []uint32, activeMode func() string) *dualHotkeyManager {
	manager := &dualHotkeyManager{
		events:     make(chan hotkey.Event, 32),
		activeMode: activeMode,
	}
	manager.rebuildLocked(dictate, agent)
	return manager
}

func (m *dualHotkeyManager) Start(ctx context.Context) error {
	m.mu.Lock()
	inner, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	dictate := m.dictate
	agent := m.agent
	m.mu.Unlock()

	if err := dictate.Start(inner); err != nil {
		cancel()
		return err
	}
	if err := agent.Start(inner); err != nil {
		dictate.Stop()
		cancel()
		return err
	}

	m.wg.Add(2)
	go m.forwardLoop("dictate", dictate.Events())
	go m.forwardLoop("agent", agent.Events())
	return nil
}

func (m *dualHotkeyManager) Stop() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		cancel := m.cancel
		dictate := m.dictate
		agent := m.agent
		m.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		if dictate != nil {
			dictate.Stop()
		}
		if agent != nil {
			agent.Stop()
		}
		m.wg.Wait()
		close(m.events)
	})
}

func (m *dualHotkeyManager) Events() <-chan hotkey.Event {
	return m.events
}

func (m *dualHotkeyManager) Reconfigure(combo []uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dictateVKs = append([]uint32(nil), combo...)
	if m.dictate != nil {
		m.dictate.Reconfigure(combo)
	}
}

func (m *dualHotkeyManager) ReconfigureModes(dictate, agent []uint32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dictateVKs = append([]uint32(nil), dictate...)
	m.agentVKs = append([]uint32(nil), agent...)
	if m.dictate != nil {
		m.dictate.Reconfigure(dictate)
	}
	if m.agent != nil {
		m.agent.Reconfigure(agent)
	}
}

func (m *dualHotkeyManager) forwardLoop(binding string, source <-chan hotkey.Event) {
	defer m.wg.Done()
	for event := range source {
		resolved := resolveBindingForActiveMode(m.sameCombo(), m.currentActiveMode(), binding)
		if resolved == "" {
			continue
		}
		event.Binding = resolved
		select {
		case m.events <- event:
		default:
		}
	}
}

func (m *dualHotkeyManager) currentActiveMode() string {
	if m.activeMode == nil {
		return "dictate"
	}
	mode := m.activeMode()
	if mode == "" {
		return "dictate"
	}
	return mode
}

func (m *dualHotkeyManager) sameCombo() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.dictateVKs) != len(m.agentVKs) {
		return false
	}
	for index := range m.dictateVKs {
		if m.dictateVKs[index] != m.agentVKs[index] {
			return false
		}
	}
	return true
}

func (m *dualHotkeyManager) rebuildLocked(dictate, agent []uint32) {
	m.dictateVKs = append([]uint32(nil), dictate...)
	m.agentVKs = append([]uint32(nil), agent...)
	m.dictate = hotkey.NewManager(dictate)
	m.agent = hotkey.NewManager(agent)
}

func resolveBindingForActiveMode(sameCombo bool, activeMode, binding string) string {
	if !sameCombo {
		return binding
	}
	if activeMode == "" {
		activeMode = "dictate"
	}
	if binding != activeMode {
		return ""
	}
	return binding
}
