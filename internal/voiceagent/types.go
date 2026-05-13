package voiceagent

// This file re-binds the public Voice Agent realtime-protocol types and the
// Session runtime from pkg/speechkit/voiceagent/live so internal/voiceagent
// (provider implementations + resume-handle storage) and the existing
// non-OSS call sites in cmd/speechkit and internal/server keep compiling
// without touching every file.
//
// Adding a new public realtime type or Session method? Make the change in
// pkg/speechkit/voiceagent/live; this file is purely a re-export bridge.

import (
	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

type (
	Session                  = live.Session
	IdleTimer                = live.IdleTimer
	State                    = live.State
	ThinkingLevel            = live.ThinkingLevel
	StartSensitivity         = live.StartSensitivity
	EndSensitivity           = live.EndSensitivity
	ActivityHandling         = live.ActivityHandling
	TurnCoverage             = live.TurnCoverage
	ThinkingPolicy           = live.ThinkingPolicy
	ContextCompressionPolicy = live.ContextCompressionPolicy
	ActivityDetectionPolicy  = live.ActivityDetectionPolicy
	LivePolicies             = live.LivePolicies
	ToolBehavior             = live.ToolBehavior
	ToolResponseScheduling   = live.ToolResponseScheduling
	ToolDefinition           = live.ToolDefinition
	ToolCall                 = live.ToolCall
	ToolResponse             = live.ToolResponse
	LiveProvider             = live.LiveProvider
	LiveInstructionUpdater   = live.LiveInstructionUpdater
	LiveReconnector          = live.LiveReconnector
	LiveConfig               = live.LiveConfig
	LiveMessage              = live.LiveMessage
	Callbacks                = live.Callbacks
	IdleConfig               = live.IdleConfig
	WorkflowConfig           = live.WorkflowConfig
	WorkflowStep             = live.WorkflowStep
)

const (
	StateInactive     = live.StateInactive
	StateConnecting   = live.StateConnecting
	StateListening    = live.StateListening
	StateProcessing   = live.StateProcessing
	StateSpeaking     = live.StateSpeaking
	StateRecovering   = live.StateRecovering
	StateDeactivating = live.StateDeactivating

	ThinkingLevelOff    = live.ThinkingLevelOff
	ThinkingLevelLow    = live.ThinkingLevelLow
	ThinkingLevelMedium = live.ThinkingLevelMedium
	ThinkingLevelHigh   = live.ThinkingLevelHigh

	StartSensitivityLow    = live.StartSensitivityLow
	StartSensitivityMedium = live.StartSensitivityMedium
	StartSensitivityHigh   = live.StartSensitivityHigh

	EndSensitivityLow    = live.EndSensitivityLow
	EndSensitivityMedium = live.EndSensitivityMedium
	EndSensitivityHigh   = live.EndSensitivityHigh

	ActivityHandlingUnspecified               = live.ActivityHandlingUnspecified
	ActivityHandlingNoInterrupt               = live.ActivityHandlingNoInterrupt
	ActivityHandlingStartOfActivityInterrupts = live.ActivityHandlingStartOfActivityInterrupts

	TurnCoverageUnspecified               = live.TurnCoverageUnspecified
	TurnCoverageTurnIncludesOnlyActivity  = live.TurnCoverageTurnIncludesOnlyActivity
	TurnCoverageTurnIncludesAllInput      = live.TurnCoverageTurnIncludesAllInput
	TurnCoverageTurnIncludesAudioActivity = live.TurnCoverageTurnIncludesAudioActivity

	ToolBehaviorUnspecified = live.ToolBehaviorUnspecified
	ToolBehaviorBlocking    = live.ToolBehaviorBlocking
	ToolBehaviorNonBlocking = live.ToolBehaviorNonBlocking

	ToolResponseSchedulingUnspecified = live.ToolResponseSchedulingUnspecified
	ToolResponseSchedulingSilent      = live.ToolResponseSchedulingSilent
	ToolResponseSchedulingWhenIdle    = live.ToolResponseSchedulingWhenIdle
	ToolResponseSchedulingInterrupt   = live.ToolResponseSchedulingInterrupt
)

// NewSession is a thin wrapper around [live.NewSession] so existing
// non-OSS call sites can continue to construct sessions through this
// package.
func NewSession(provider LiveProvider, callbacks Callbacks) *Session {
	return live.NewSession(provider, callbacks)
}

// NewIdleTimer is a thin wrapper around [live.NewIdleTimer].
func NewIdleTimer(cfg IdleConfig, session *Session) *IdleTimer {
	return live.NewIdleTimer(cfg, session)
}

// DefaultIdleConfig is a thin wrapper around [live.DefaultIdleConfig].
func DefaultIdleConfig() IdleConfig {
	return live.DefaultIdleConfig()
}

// RenderHostInstructionUpdate is a thin wrapper around
// [live.RenderHostInstructionUpdate].
func RenderHostInstructionUpdate(cfg LiveConfig) string {
	return live.RenderHostInstructionUpdate(cfg)
}
