package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kombifyio/SpeechKit/internal/audio"
)

// This file groups the prompter (voice agent chat window) and the live
// voice-agent audio stream player. Both concerns are tightly coupled to the
// voice agent session; keeping them adjacent makes that grouping explicit.

func (s *appState) showPrompterWindow() {
	s.showPrompterWindowForMode("voice_agent", false)
}

func (s *appState) showPrompterWindowForMode(mode string, reset bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	locator := s.screenLocator
	previousMode := s.prompterMode
	wasVisible := prompter != nil && prompter.IsVisible()
	s.prompterMode = mode
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	if locator != nil {
		if bounds, ok := locator.OverlayScreenBounds(); ok {
			x, y := prompterPosition(bounds)
			prompter.SetPosition(x, y)
		}
	}

	s.setPrompterMode(mode)
	if reset || !wasVisible || previousMode != mode {
		s.clearPrompterMessages()
		s.updatePrompterState("inactive")
	}
	if !prompter.IsVisible() {
		prompter.Show()
	}
}

func (s *appState) hidePrompterWindow() {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter != nil {
		prompter.Hide()
	}
}

func (s *appState) minimisePrompterWindow() {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter != nil {
		prompter.Minimise()
	}
}

func (s *appState) sendPrompterMessage(role, text string, done bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	escapedText := escapeJS(text)
	doneStr := "false"
	if done {
		doneStr = "true"
	}
	prompter.ExecJS(fmt.Sprintf(
		`if(window.__prompter){window.__prompter.addMessage({role:%q,text:%q,done:%s})}`,
		role, escapedText, doneStr,
	))
}

func (s *appState) clearPrompterMessages() {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	prompter.ExecJS(`if(window.__prompter){window.__prompter.clear()}`)
}

func (s *appState) setPrompterMode(mode string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	prompter.ExecJS(fmt.Sprintf(
		`if(window.__prompter){window.__prompter.setMode(%q)}`,
		escapeJS(mode),
	))
}

func (s *appState) updatePrompterState(state string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	prompter.ExecJS(fmt.Sprintf(
		`if(window.__prompter){window.__prompter.updateState(%q)}`,
		escapeJS(state),
	))
}

func (s *appState) updatePrompterActivity(role string, level float64) {
	if s == nil {
		return
	}
	if role != "user" && role != "assistant" {
		return
	}
	if level != level || level < 0 {
		level = 0
	}
	if level > 1 {
		level = 1
	}

	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		return
	}

	prompter.ExecJS(fmt.Sprintf(
		`if(window.__prompter){window.__prompter.setActivity(%q,%.4f)}`,
		role, level,
	))
}

func (s *appState) startVoiceAgentStream(ctx context.Context) {
	s.mu.Lock()
	outputDeviceID := s.audioOutputDeviceID
	s.mu.Unlock()

	sp, err := audio.NewStreamPlayerWithOutputDevice(outputDeviceID)
	if err != nil {
		slog.Error("voice agent stream player init", "err", err)
		return
	}
	s.mu.Lock()
	old := s.streamPlayer
	s.streamPlayer = sp
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}
	sp.Start(ctx)
}

func (s *appState) stopVoiceAgentStream() {
	s.mu.Lock()
	sp := s.streamPlayer
	s.streamPlayer = nil
	s.mu.Unlock()
	if sp != nil {
		sp.Close()
	}
}

func (s *appState) interruptVoiceAgentStream(ctx context.Context) {
	s.mu.Lock()
	sp := s.streamPlayer
	s.mu.Unlock()
	if sp != nil {
		sp.StopAndDrain()
		sp.Start(ctx)
	}
}

func (s *appState) writeVoiceAgentAudio(chunk []byte) {
	s.mu.Lock()
	sp := s.streamPlayer
	s.mu.Unlock()
	if sp != nil {
		sp.WriteChunk(chunk)
	}
}
