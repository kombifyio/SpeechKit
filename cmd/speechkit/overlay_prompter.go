package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
)

// This file groups the prompter (voice agent chat window) and the live
// voice-agent audio stream player. Both concerns are tightly coupled to the
// voice agent session; keeping them adjacent makes that grouping explicit.

func (s *appState) showPrompterWindowForMode(mode string, reset bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	locator := s.screenLocator
	previousMode := s.prompterMode
	wasVisible := prompter != nil && prompter.IsVisible()
	feedbackMode := s.feedbackModeForRuntimeModeLocked(mode)
	s.prompterMode = mode
	s.mu.Unlock()

	if prompter == nil {
		return
	}
	if feedbackMode == config.OverlayFeedbackModeSmallFeedback {
		prompter.Hide()
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

func (s *appState) feedbackModeForRuntimeModeLocked(mode string) string {
	switch mode {
	case modeAssist:
		return normalizeRuntimeOverlayFeedbackMode(s.assistOverlayMode)
	case modeVoiceAgent:
		return normalizeRuntimeOverlayFeedbackMode(s.voiceAgentOverlayMode)
	default:
		return config.OverlayFeedbackModeBigProductivity
	}
}

func normalizeRuntimeOverlayFeedbackMode(value string) string {
	if strings.TrimSpace(value) == "" {
		return config.OverlayFeedbackModeBigProductivity
	}
	return config.NormalizeOverlayFeedbackMode(value, config.OverlayFeedbackModeSmallFeedback)
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

func (s *appState) sendPrompterMessage(role, text string, done bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	prompter := s.prompterWindow
	s.mu.Unlock()

	if prompter == nil {
		if role == "user" || role == "assistant" || role == "system" {
			s.setOverlayFeedbackMessage(role, text, done)
		}
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
	if role == "user" || role == "assistant" || role == "system" {
		s.setOverlayFeedbackMessage(role, text, done)
	}
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

func (s *appState) updateOverlayForVoiceAgentState(state string) {
	if s == nil {
		return
	}

	overlayState := "idle"
	overlayText := ""
	overlayPhase := "idle"
	switch strings.TrimSpace(state) {
	case "connecting":
		overlayState = "processing"
		overlayText = "Connecting"
		overlayPhase = "thinking"
	case "listening":
		overlayState = "recording"
		overlayText = "Listening"
		overlayPhase = "listening"
	case "processing":
		overlayState = "processing"
		overlayText = "Thinking"
		overlayPhase = "thinking"
	case "speaking":
		overlayState = "recording"
		overlayText = "Speaking"
		overlayPhase = "speaking"
	case "deactivating":
		overlayState = "processing"
		overlayText = "Finishing"
		overlayPhase = "thinking"
	}

	s.mu.Lock()
	s.activeMode = modeVoiceAgent
	s.hotkey = s.activeHotkeyLocked()
	if overlayState == "idle" {
		if s.currentState != "done" {
			s.currentState = "idle"
			s.overlayText = ""
			s.overlayFeedbackRole = ""
			s.overlayFeedbackText = ""
			s.overlayFeedbackDone = true
			s.overlayLevel = 0
			s.overlayPhase = "idle"
		}
	} else {
		s.currentState = overlayState
		if strings.TrimSpace(s.overlayFeedbackText) == "" {
			s.overlayText = overlayText
		}
		if overlayPhase == "speaking" {
			s.overlayPhase = "speaking"
		} else {
			s.overlayPhase = overlayPhase
		}
		if overlayState != "recording" {
			s.overlayLevel = 0
		}
	}
	s.syncSpeechKitSnapshotLocked()
	s.mu.Unlock()

	s.showActiveOverlayWindow()
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
