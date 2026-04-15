package main

import (
	"context"
	"sync"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

type modeHotkeyReconfigurer interface {
	ReconfigureModes(bindings map[string][]uint32)
}

type managedHotkeyManager interface {
	Start(context.Context) error
	Stop()
	Events() <-chan hotkey.Event
	Reconfigure([]uint32)
}

type modeHotkeyManager struct {
	mu         sync.RWMutex
	managers   map[string]managedHotkeyManager
	events     chan hotkey.Event
	ctx        context.Context
	newManager func([]uint32) managedHotkeyManager
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stopOnce   sync.Once
}

func newModeHotkeyManager(bindings map[string][]uint32) *modeHotkeyManager {
	manager := &modeHotkeyManager{
		events:     make(chan hotkey.Event, 32),
		newManager: func(combo []uint32) managedHotkeyManager { return hotkey.NewManager(combo) },
	}
	manager.rebuildLocked(bindings)
	return manager
}

func (m *modeHotkeyManager) Start(ctx context.Context) error {
	m.mu.Lock()
	inner, cancel := context.WithCancel(ctx)
	m.ctx = inner
	m.cancel = cancel
	managers := cloneHotkeyManagers(m.managers)
	m.mu.Unlock()

	started := make([]managedHotkeyManager, 0, len(managers))
	for _, mode := range orderedRuntimeModes() {
		manager := managers[mode]
		if manager == nil {
			continue
		}
		if err := manager.Start(inner); err != nil {
			cancel()
			m.mu.Lock()
			m.ctx = nil
			m.cancel = nil
			m.mu.Unlock()
			for _, startedManager := range started {
				startedManager.Stop()
			}
			return err
		}
		started = append(started, manager)
		m.wg.Add(1)
		go m.forwardLoop(mode, manager.Events())
	}

	return nil
}

func (m *modeHotkeyManager) Stop() {
	m.stopOnce.Do(func() {
		m.mu.Lock()
		cancel := m.cancel
		managers := cloneHotkeyManagers(m.managers)
		m.ctx = nil
		m.cancel = nil
		m.mu.Unlock()

		if cancel != nil {
			cancel()
		}
		for _, mode := range orderedRuntimeModes() {
			if manager := managers[mode]; manager != nil {
				manager.Stop()
			}
		}
		m.wg.Wait()
		close(m.events)
	})
}

func (m *modeHotkeyManager) Events() <-chan hotkey.Event {
	return m.events
}

func (m *modeHotkeyManager) Reconfigure(combo []uint32) {
	m.ReconfigureModes(map[string][]uint32{
		modeDictate: combo,
	})
}

func (m *modeHotkeyManager) ReconfigureModes(bindings map[string][]uint32) {
	m.mu.Lock()
	if m.managers == nil {
		m.managers = make(map[string]managedHotkeyManager, len(bindings))
	}

	runtimeCtx := m.ctx
	type pendingManagerStart struct {
		mode    string
		manager managedHotkeyManager
	}
	pendingStarts := make([]pendingManagerStart, 0, len(bindings))

	for _, mode := range orderedRuntimeModes() {
		combo, ok := bindings[mode]
		manager := m.managers[mode]
		switch {
		case !ok && manager != nil:
			manager.Reconfigure(nil)
		case ok && manager != nil:
			manager.Reconfigure(combo)
		case ok && manager == nil:
			manager = m.newManager(combo)
			m.managers[mode] = manager
			if runtimeCtx != nil {
				pendingStarts = append(pendingStarts, pendingManagerStart{
					mode:    mode,
					manager: manager,
				})
			}
		}
	}
	m.mu.Unlock()

	for _, pending := range pendingStarts {
		if err := pending.manager.Start(runtimeCtx); err != nil {
			m.mu.Lock()
			if m.managers[pending.mode] == pending.manager {
				delete(m.managers, pending.mode)
			}
			m.mu.Unlock()
			continue
		}
		m.wg.Add(1)
		go m.forwardLoop(pending.mode, pending.manager.Events())
	}
}

func (m *modeHotkeyManager) rebuildLocked(bindings map[string][]uint32) {
	m.managers = make(map[string]managedHotkeyManager, len(bindings))
	for _, mode := range orderedRuntimeModes() {
		if combo, ok := bindings[mode]; ok {
			m.managers[mode] = m.newManager(combo)
		}
	}
}

func (m *modeHotkeyManager) forwardLoop(binding string, source <-chan hotkey.Event) {
	defer m.wg.Done()
	for event := range source {
		event.Binding = binding
		select {
		case m.events <- event:
		default:
		}
	}
}

func cloneHotkeyManagers(input map[string]managedHotkeyManager) map[string]managedHotkeyManager {
	if input == nil {
		return nil
	}
	cloned := make(map[string]managedHotkeyManager, len(input))
	for key, value := range input {
		cloned[key] = value
	}
	return cloned
}
