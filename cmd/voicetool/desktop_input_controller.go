package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kombifyio/SpeechKit/internal/hotkey"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type recordingStatusReader interface {
	IsRecording() bool
}

type desktopInputController struct {
	commands          speechkit.CommandBus
	recording         recordingStatusReader
	state             *appState
	hotkeyEvents      <-chan hotkey.Event
	silenceAutoStop   <-chan struct{}
	autoStartInterval time.Duration
}

func (c desktopInputController) Run(ctx context.Context) {
	interval := c.autoStartInterval
	if interval <= 0 {
		interval = 200 * time.Millisecond
	}
	autoStartTicker := time.NewTicker(interval)
	defer autoStartTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.silenceAutoStop:
			c.handleSilenceAutoStop(ctx)
		case <-autoStartTicker.C:
			c.handleAutoStartTick(ctx)
		case evt, ok := <-c.hotkeyEvents:
			if !ok {
				return
			}
			c.handleHotkey(ctx, evt)
		}
	}
}

func (c desktopInputController) handleSilenceAutoStop(ctx context.Context) {
	if c.recording == nil || !c.recording.IsRecording() {
		return
	}
	c.log("Quick Capture: silence detected, auto-stopping", "info")
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStopDictation,
		Metadata: map[string]string{
			"label": "Quick Capture",
		},
	}, "Stop")
}

func (c desktopInputController) handleAutoStartTick(ctx context.Context) {
	if c.recording != nil && c.recording.IsRecording() {
		return
	}
	if c.state == nil || !c.state.consumeQuickCaptureAutoStart() {
		return
	}
	c.dispatch(ctx, speechkit.Command{
		Type: speechkit.CommandStartDictation,
		Metadata: map[string]string{
			"label": "Quick Capture: auto-recording started (speak now, auto-stops on silence)",
		},
	}, "Start")
}

func (c desktopInputController) handleHotkey(ctx context.Context, evt hotkey.Event) {
	if evt.Binding == "agent" {
		if evt.Type == hotkey.EventKeyDown {
			c.dispatch(ctx, speechkit.Command{
				Type: speechkit.CommandSetActiveMode,
				Metadata: map[string]string{
					"mode": "agent",
				},
			}, "Set mode")
		}
		return
	}
	if evt.Binding == "dictate" && evt.Type == hotkey.EventKeyDown {
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandSetActiveMode,
			Metadata: map[string]string{
				"mode": "dictate",
			},
		}, "Set mode")
	}
	switch evt.Type {
	case hotkey.EventKeyDown:
		if c.recording != nil && c.recording.IsRecording() {
			c.dispatch(ctx, speechkit.Command{
				Type: speechkit.CommandStopDictation,
				Metadata: map[string]string{
					"label": "Captured",
				},
			}, "Stop")
			return
		}
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandStartDictation,
			Metadata: map[string]string{
				"label": "Recording started",
			},
		}, "Start")
	case hotkey.EventKeyUp:
		if c.recording == nil || !c.recording.IsRecording() {
			return
		}
		if c.state != nil && c.state.quickCaptureModeActive() {
			return
		}
		c.dispatch(ctx, speechkit.Command{
			Type: speechkit.CommandStopDictation,
			Metadata: map[string]string{
				"label": "Captured",
			},
		}, "Stop")
	}
}

func (c desktopInputController) dispatch(ctx context.Context, command speechkit.Command, action string) {
	if c.commands == nil {
		return
	}
	if err := c.commands.Dispatch(ctx, command); err != nil {
		c.log(fmt.Sprintf("%s error: %v", action, err), "error")
	}
}

func (c desktopInputController) log(message, kind string) {
	if c.state == nil || message == "" {
		return
	}
	c.state.addLog(message, kind)
}
