package main

import (
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
)

type hotkeyReconfigurer interface {
	Reconfigure([]uint32)
}

type audioDeviceReconfigurer interface {
	ReconfigureDevice(string) error
}

type runtimeState struct {
	logEntries               []logEntry
	transcriptions           int
	providers                []string
	hotkey                   string
	dictateHotkey            string
	assistHotkey             string
	voiceAgentHotkey         string
	dictateHotkeyBehavior    string
	assistHotkeyBehavior     string
	voiceAgentHotkeyBehavior string
	dictateEnabled           bool
	assistEnabled            bool
	voiceAgentEnabled        bool
	agentHotkey              string
	currentState             string
	overlayText              string
	overlayLevel             float64
	overlayPhase             string
	overlayVisualizer        string
	overlayDesign            string
	overlayEnabled           bool
	overlayPosition          string
	overlayMovable           bool
	overlayFreeX             int
	overlayFreeY             int
	overlayMonitorKey        string
	overlayMonitorCenters    map[string]config.OverlayFreePosition
	quickNoteMode            bool
	quickCaptureMode         bool
	quickCaptureAutoStart    bool
	quickCaptureNoteID       int64
	lastTranscriptionText    string
	vocabularyDictionary     string
	activeMode               string
	audioDeviceID            string
	activeProfiles           map[string]string
}

type desktopHostState struct {
	overlay        overlayWindow
	pillAnchor     overlayWindow
	pillPanel      overlayWindow
	dotAnchor      overlayWindow
	radialMenu     overlayWindow
	dashboard      settingsWindow
	settings       settingsWindow
	appTray        trayStateSetter
	screenLocator  overlayScreenLocator
	doneResetDelay time.Duration
}

func (s *appState) runtimeStateLocked() runtimeState {
	state := runtimeState{
		logEntries:               append([]logEntry(nil), s.logEntries...),
		transcriptions:           s.transcriptions,
		providers:                append([]string(nil), s.providers...),
		hotkey:                   s.hotkey,
		dictateHotkey:            s.dictateHotkey,
		assistHotkey:             s.assistHotkey,
		voiceAgentHotkey:         s.voiceAgentHotkey,
		dictateHotkeyBehavior:    s.dictateHotkeyBehavior,
		assistHotkeyBehavior:     s.assistHotkeyBehavior,
		voiceAgentHotkeyBehavior: s.voiceAgentHotkeyBehavior,
		dictateEnabled:           s.dictateEnabled,
		assistEnabled:            s.assistEnabled,
		voiceAgentEnabled:        s.voiceAgentEnabled,
		agentHotkey:              s.agentHotkey,
		currentState:             s.currentState,
		overlayText:              s.overlayText,
		overlayLevel:             s.overlayLevel,
		overlayPhase:             s.overlayPhase,
		overlayVisualizer:        s.overlayVisualizer,
		overlayDesign:            s.overlayDesign,
		overlayEnabled:           s.overlayEnabled,
		overlayPosition:          s.overlayPosition,
		overlayMovable:           s.overlayMovable,
		overlayFreeX:             s.overlayFreeX,
		overlayFreeY:             s.overlayFreeY,
		overlayMonitorKey:        s.overlayMonitorKey,
		quickNoteMode:            s.quickNoteMode,
		quickCaptureMode:         s.quickCaptureMode,
		quickCaptureAutoStart:    s.quickCaptureAutoStart,
		quickCaptureNoteID:       s.quickCaptureNoteID,
		lastTranscriptionText:    s.lastTranscriptionText,
		vocabularyDictionary:     s.vocabularyDictionary,
		activeMode:               s.activeMode,
		audioDeviceID:            s.audioDeviceID,
	}
	if s.activeProfiles != nil {
		state.activeProfiles = make(map[string]string, len(s.activeProfiles))
		for key, value := range s.activeProfiles {
			state.activeProfiles[key] = value
		}
	}
	if s.overlayMonitorCenters != nil {
		state.overlayMonitorCenters = make(map[string]config.OverlayFreePosition, len(s.overlayMonitorCenters))
		for key, value := range s.overlayMonitorCenters {
			state.overlayMonitorCenters[key] = value
		}
	}
	return state
}

func (s *appState) desktopHostStateLocked() desktopHostState {
	state := desktopHostState{
		overlay:        s.overlay,
		pillAnchor:     s.pillAnchor,
		pillPanel:      s.pillPanel,
		dotAnchor:      s.dotAnchor,
		radialMenu:     s.radialMenu,
		dashboard:      s.dashboard,
		settings:       s.settings,
		appTray:        s.appTray,
		screenLocator:  s.screenLocator,
		doneResetDelay: s.doneResetDelay,
	}
	return state
}
