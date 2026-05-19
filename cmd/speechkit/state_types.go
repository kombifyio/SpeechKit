package main

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/firebase/genkit/go/core"

	appai "github.com/kombifyio/SpeechKit/internal/ai"
	"github.com/kombifyio/SpeechKit/internal/ai/flows"
	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/audio"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/downloads"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/textactions"
	"github.com/kombifyio/SpeechKit/internal/tts"
	"github.com/kombifyio/SpeechKit/internal/voiceagent"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
)

type logEntry struct {
	Message   string `json:"message"`
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
}

// appState holds shared state for UI updates.
type appState struct {
	mu                       sync.Mutex
	controlPlaneToken        string
	overlay                  overlayWindow
	pillAnchor               overlayWindow
	pillPanel                overlayWindow
	dotAnchor                overlayWindow
	radialMenu               overlayWindow
	dashboard                settingsWindow
	settings                 settingsWindow
	appTray                  trayStateSetter
	screenLocator            overlayScreenLocator
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
	overlayFeedbackRole      string
	overlayFeedbackText      string
	overlayFeedbackDone      bool
	overlayLevel             float64
	overlayPhase             string
	overlayVisualizer        string
	overlayDesign            string
	assistOverlayMode        string
	voiceAgentOverlayMode    string
	overlayEnabled           bool
	overlayPosition          string
	overlayMovable           bool
	overlayFreeX             int
	overlayFreeY             int
	overlayMonitorKey        string
	overlayMonitorCenters    map[string]config.OverlayFreePosition
	quickNoteMode            bool
	quickCaptureMode         bool
	quickCaptureAutoStart    bool  // when true, next PTT event auto-starts + auto-stops recording
	quickNoteAutoStop        bool  // enables silence auto-stop for editor-armed quick-note recording
	quickCaptureNoteID       int64 // the specific note ID this capture session writes to
	lastTranscriptionText    string
	vocabularyDictionary     string
	activeMode               string
	prompterMode             string
	audioDeviceID            string
	audioOutputDeviceID      string
	activeProfiles           map[string]string
	hkManager                hotkeyReconfigurer
	audioSession             audioDeviceReconfigurer
	wakewordRuntime          *desktopWakeRuntime
	wakewordStatus           string
	engine                   *speechkit.Runtime
	sttRouter                *router.Router
	genkitRT                 *appai.Runtime
	summarizeFlow            *core.Flow[flows.SummarizeInput, string, struct{}]
	agentFlow                *core.Flow[flows.AgentInput, flows.AgentOutput, struct{}]
	assistFlow               *core.Flow[flows.AssistInput, flows.AssistOutput, struct{}]
	assistExecutor           assist.ToolExecutor
	assistPipeline           *assist.Pipeline
	assistBubble             overlayWindow
	prompterWindow           overlayWindow
	ttsRouter                *tts.Router
	audioPlayer              *audio.Player
	localLLMRuntime          localLLMRuntimeStarter
	voiceAgentSession        *voiceagent.Session
	voiceAgentDialogTurns    []voiceAgentDialogTurn
	voiceAgentSessionStarted time.Time
	voiceAgentSessionID      string    // audit: opaque session identifier for the current VA session
	voiceAgentSessionStart   time.Time // audit: wall-clock start of the current VA session
	voiceAgentTerminatedBy   string    // audit: termination reason preset before session.Stop() or OnError fires
	voiceAgentSummaryDone    bool
	voiceAgentStore          store.VoiceAgentSessionStore
	voiceAgentSummaryTool    textactions.SummaryTool
	streamPlayer             *audio.StreamPlayer
	voiceAgentAudioSender    *voiceAgentAudioSender
	// voiceAgentActivationCancel cancels the activation goroutine's context
	// so a hold-to-talk release can abort an in-flight session.Start (most
	// notably while Gemini Live is still establishing its WebSocket). The
	// field is set synchronously by activateVoiceAgent before the goroutine
	// runs and cleared by startVoiceAgentSession once activation either
	// succeeds or fails.
	voiceAgentActivationCancel context.CancelFunc
	voiceAgentEchoGuard        *voiceAgentEchoGuard
	wailsApp                   *application.App
	captureWin                 *application.WebviewWindow
	doneResetDelay             time.Duration
	downloads                  *downloads.Manager
	appUpdates                 *appUpdateManager
	shuttingDown               bool
	appStarted                 bool

	// serverDelegates holds optional per-mode adapters that delegate to a
	// remote SpeechKit Server-Target. Nil when every mode runs locally
	// (the pre-0.26 default). See server_delegates.go.
	serverDelegates *serverDelegates
}

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
	overlayFeedbackRole      string
	overlayFeedbackText      string
	overlayFeedbackDone      bool
	overlayLevel             float64
	overlayPhase             string
	overlayVisualizer        string
	overlayDesign            string
	assistOverlayMode        string
	voiceAgentOverlayMode    string
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
		overlayFeedbackRole:      s.overlayFeedbackRole,
		overlayFeedbackText:      s.overlayFeedbackText,
		overlayFeedbackDone:      s.overlayFeedbackDone,
		overlayLevel:             s.overlayLevel,
		overlayPhase:             s.overlayPhase,
		overlayVisualizer:        s.overlayVisualizer,
		overlayDesign:            s.overlayDesign,
		assistOverlayMode:        s.assistOverlayMode,
		voiceAgentOverlayMode:    s.voiceAgentOverlayMode,
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
