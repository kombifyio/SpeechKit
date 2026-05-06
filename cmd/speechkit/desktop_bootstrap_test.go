package main

import (
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

func TestDesktopAudioLevelHandlerQuickCaptureIgnoresInitialSilence(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(12)
	silenceAutoStop := make(chan struct{}, 1)
	handler := desktopAudioLevelHandler(&config.Config{
		General: config.GeneralConfig{FastModeSilenceMs: 5},
	}, state, silenceAutoStop)

	handler(0)
	time.Sleep(10 * time.Millisecond)
	handler(0)

	select {
	case <-silenceAutoStop:
		t.Fatal("silence auto-stop fired before speech was detected")
	default:
	}
}

func TestDesktopAudioLevelHandlerQuickCaptureStopsAfterSpeechThenSilence(t *testing.T) {
	state := &appState{}
	state.armQuickCapture(12)
	silenceAutoStop := make(chan struct{}, 1)
	handler := desktopAudioLevelHandler(&config.Config{
		General: config.GeneralConfig{FastModeSilenceMs: 5},
	}, state, silenceAutoStop)

	handler(0.04)
	handler(0)
	time.Sleep(10 * time.Millisecond)
	handler(0)

	select {
	case <-silenceAutoStop:
	case <-time.After(50 * time.Millisecond):
		t.Fatal("silence auto-stop did not fire after speech followed by silence")
	}
}
