package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
)

type fakeManagedHotkey struct {
	mu           sync.Mutex
	combo        []uint32
	reconfigured [][]uint32
	startCalls   int
	events       chan hotkey.Event
	stopOnce     sync.Once
}

func newFakeManagedHotkey(combo []uint32) *fakeManagedHotkey {
	return &fakeManagedHotkey{
		combo:  append([]uint32(nil), combo...),
		events: make(chan hotkey.Event, 4),
	}
}

func (f *fakeManagedHotkey) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalls++
	return nil
}

func (f *fakeManagedHotkey) Stop() {
	f.stopOnce.Do(func() {
		close(f.events)
	})
}

func (f *fakeManagedHotkey) Events() <-chan hotkey.Event {
	return f.events
}

func (f *fakeManagedHotkey) Reconfigure(combo []uint32) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cloned := append([]uint32(nil), combo...)
	f.combo = cloned
	f.reconfigured = append(f.reconfigured, cloned)
}

func TestConfiguredModeBindingsSkipsDisabledModes(t *testing.T) {
	bindings := configuredModeBindings("win+alt", "", "ctrl+shift+v")

	if got, want := bindings[modeDictate], "win+alt"; got != want {
		t.Fatalf("bindings[%q] = %q, want %q", modeDictate, got, want)
	}
	if _, ok := bindings[modeAssist]; ok {
		t.Fatalf("bindings unexpectedly contains %q", modeAssist)
	}
	if got, want := bindings[modeVoiceAgent], "ctrl+shift+v"; got != want {
		t.Fatalf("bindings[%q] = %q, want %q", modeVoiceAgent, got, want)
	}
}

func TestSanitizeActiveModeForBindingsDisablesUnavailableModes(t *testing.T) {
	if got := sanitizeActiveModeForBindings(modeAssist, modeAssist, "win+alt", "", "ctrl+shift+v"); got != modeNone {
		t.Fatalf("sanitizeActiveModeForBindings returned %q, want %q", got, modeNone)
	}
	if got := sanitizeActiveModeForBindings(modeVoiceAgent, modeAssist, "win+alt", "ctrl+shift+j", "ctrl+shift+v"); got != modeVoiceAgent {
		t.Fatalf("sanitizeActiveModeForBindings returned %q, want %q", got, modeVoiceAgent)
	}
}

func TestDeriveLegacyAgentModeFromBindingsPrefersActiveMode(t *testing.T) {
	if got := deriveLegacyAgentModeFromBindings("ctrl+shift+j", "ctrl+shift+v", modeVoiceAgent, modeAssist); got != modeVoiceAgent {
		t.Fatalf("deriveLegacyAgentModeFromBindings returned %q, want %q", got, modeVoiceAgent)
	}
	if got := deriveLegacyAgentModeFromBindings("ctrl+shift+j", "", modeNone, modeVoiceAgent); got != modeAssist {
		t.Fatalf("deriveLegacyAgentModeFromBindings returned %q, want %q", got, modeAssist)
	}
}

func TestModeHotkeyManagerReconfigureModesStartsNewManagersWhileRunning(t *testing.T) {
	manager := newModeHotkeyManager(nil)

	var created *fakeManagedHotkey
	manager.newManager = func(combo []uint32) managedHotkeyManager {
		created = newFakeManagedHotkey(combo)
		return created
	}

	if err := manager.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer manager.Stop()

	manager.ReconfigureModes(map[string][]uint32{
		modeAssist: {hotkey.VkControl, hotkey.VkShift, 'J'},
	})

	if created == nil {
		t.Fatal("expected a new hotkey manager to be created")
	}
	if created.startCalls != 1 {
		t.Fatalf("created.startCalls = %d, want 1", created.startCalls)
	}

	created.events <- hotkey.Event{Type: hotkey.EventKeyDown}

	select {
	case event := <-manager.Events():
		if event.Binding != modeAssist {
			t.Fatalf("event.Binding = %q, want %q", event.Binding, modeAssist)
		}
		if event.Type != hotkey.EventKeyDown {
			t.Fatalf("event.Type = %v, want %v", event.Type, hotkey.EventKeyDown)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected forwarded event from newly enabled manager")
	}
}
