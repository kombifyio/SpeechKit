package wakeword

import (
	"context"
	"sync"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/sdkparity"
)

type capturingHotkeySink struct {
	mu     sync.Mutex
	events []HotkeyEvent
}

func (s *capturingHotkeySink) Submit(ev HotkeyEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ev)
}

func (s *capturingHotkeySink) snapshot() []HotkeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]HotkeyEvent, len(s.events))
	copy(out, s.events)
	return out
}

func TestDispatcherParity(t *testing.T) {
	sdkparity.RunWakeDispatcherParity(t, sdkparity.WakeDispatcherHarness{
		Dispatch:           dispatchWakeEvents,
		DispatchAfterClose: dispatchWakeEventAfterClose,
	})
}

func TestAutoEndParity(t *testing.T) {
	sdkparity.RunWakeAutoEndParity(t, sdkparity.WakeAutoEndHarness{
		DefaultConfig: func() sdkparity.WakeAutoEndConfig {
			cfg := DefaultAutoEndConfig()
			return sdkparity.WakeAutoEndConfig{
				SilenceCutoff: cfg.SilenceCutoff,
				ExitPhrases:   cfg.ExitPhrases,
			}
		},
		NewPolicy: func(cfg sdkparity.WakeAutoEndConfig) sdkparity.WakeAutoEndPolicy {
			return wrapAutoEndPolicy(NewAutoEndPolicy(AutoEndConfig{
				SilenceCutoff: cfg.SilenceCutoff,
				ExitPhrases:   cfg.ExitPhrases,
			}, nil))
		},
	})
}

func dispatchWakeEvents(ctx context.Context, opts sdkparity.WakeDispatcherOptions, events []sdkparity.WakeDetection) ([]sdkparity.WakeHotkeyEvent, error) {
	sink := &capturingHotkeySink{}
	dispatcher := NewDispatcher(sink, DispatcherOptions{
		SyntheticRelease: opts.SyntheticRelease,
		ReleaseAfter:     opts.ReleaseAfter,
	})
	for _, event := range events {
		dispatcher.Emit(DetectionEvent{
			Phrase:      event.Phrase,
			Mode:        event.Mode,
			Probability: event.Probability,
		})
	}
	if err := dispatcher.Close(ctx); err != nil {
		return nil, err
	}
	return convertHotkeyEvents(sink.snapshot()), nil
}

func dispatchWakeEventAfterClose(ctx context.Context, opts sdkparity.WakeDispatcherOptions, event sdkparity.WakeDetection) ([]sdkparity.WakeHotkeyEvent, error) {
	sink := &capturingHotkeySink{}
	dispatcher := NewDispatcher(sink, DispatcherOptions{
		SyntheticRelease: opts.SyntheticRelease,
		ReleaseAfter:     opts.ReleaseAfter,
	})
	if err := dispatcher.Close(ctx); err != nil {
		return nil, err
	}
	dispatcher.Emit(DetectionEvent{
		Phrase:      event.Phrase,
		Mode:        event.Mode,
		Probability: event.Probability,
	})
	return convertHotkeyEvents(sink.snapshot()), nil
}

func convertHotkeyEvents(events []HotkeyEvent) []sdkparity.WakeHotkeyEvent {
	out := make([]sdkparity.WakeHotkeyEvent, 0, len(events))
	for _, event := range events {
		out = append(out, sdkparity.WakeHotkeyEvent{
			KeyDown: event.KeyDown,
			Binding: event.Binding,
			Source:  event.Source,
		})
	}
	return out
}

type autoEndAdapter struct {
	inner   *AutoEndPolicy
	reasons chan string
}

func wrapAutoEndPolicy(inner *AutoEndPolicy) *autoEndAdapter {
	reasons := make(chan string, 1)
	go func() {
		for reason := range inner.EndSignal() {
			reasons <- string(reason)
		}
		close(reasons)
	}()
	return &autoEndAdapter{inner: inner, reasons: reasons}
}

func (a *autoEndAdapter) Start()                    { a.inner.Start() }
func (a *autoEndAdapter) NotifyTranscript(s string) { a.inner.NotifyTranscript(s) }
func (a *autoEndAdapter) EndSignal() <-chan string  { return a.reasons }
func (a *autoEndAdapter) Close()                    { a.inner.Close() }
