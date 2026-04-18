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
	bindings := configuredModeBindings(true, true, true, "win+alt", "", "ctrl+shift")

	if got, want := bindings[modeDictate], "win+alt"; got != want {
		t.Fatalf("bindings[%q] = %q, want %q", modeDictate, got, want)
	}
	if _, ok := bindings[modeAssist]; ok {
		t.Fatalf("bindings unexpectedly contains %q", modeAssist)
	}
	if got, want := bindings[modeVoiceAgent], "ctrl+shift"; got != want {
		t.Fatalf("bindings[%q] = %q, want %q", modeVoiceAgent, got, want)
	}
}

func TestConfiguredModeBindingsSkipsExplicitlyDisabledModes(t *testing.T) {
	bindings := configuredModeBindings(true, false, true, "win+alt", "ctrl+win", "ctrl+shift")

	if _, ok := bindings[modeAssist]; ok {
		t.Fatalf("bindings unexpectedly contains %q", modeAssist)
	}
	if got, want := bindings[modeDictate], "win+alt"; got != want {
		t.Fatalf("bindings[%q] = %q, want %q", modeDictate, got, want)
	}
}

func TestSanitizeActiveModeForBindingsDisablesUnavailableModes(t *testing.T) {
	if got := sanitizeActiveModeForBindings(modeAssist, modeAssist, true, false, true, "win+alt", "", "ctrl+shift"); got != modeNone {
		t.Fatalf("sanitizeActiveModeForBindings returned %q, want %q", got, modeNone)
	}
	if got := sanitizeActiveModeForBindings(modeVoiceAgent, modeAssist, true, true, true, "win+alt", "ctrl+win", "ctrl+shift"); got != modeVoiceAgent {
		t.Fatalf("sanitizeActiveModeForBindings returned %q, want %q", got, modeVoiceAgent)
	}
}

func TestDeriveLegacyAgentModeFromBindingsPrefersActiveMode(t *testing.T) {
	if got := deriveLegacyAgentModeFromBindings("ctrl+win", "ctrl+shift", modeVoiceAgent, modeAssist); got != modeVoiceAgent {
		t.Fatalf("deriveLegacyAgentModeFromBindings returned %q, want %q", got, modeVoiceAgent)
	}
	if got := deriveLegacyAgentModeFromBindings("ctrl+win", "", modeNone, modeVoiceAgent); got != modeAssist {
		t.Fatalf("deriveLegacyAgentModeFromBindings returned %q, want %q", got, modeAssist)
	}
}

func TestValidateDistinctModeHotkeysRejectsDuplicateTwoKeyBases(t *testing.T) {
	if validateDistinctModeHotkeys(true, true, true, "win+alt", "win+alt+j", "ctrl+shift") {
		t.Fatal("expected duplicate two-key base to be rejected")
	}
	if !validateDistinctModeHotkeys(true, true, true, "win+alt", "ctrl+win+j", "ctrl+shift") {
		t.Fatal("expected distinct two-key bases to pass validation")
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
