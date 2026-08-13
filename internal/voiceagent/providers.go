package voiceagent

// This file re-exports the realtime Voice Agent provider implementations that
// now live in pkg/speechkit/voiceagent/live, so existing non-OSS call sites in
// cmd/speechkit, internal/server, and tools keep compiling without import
// churn. New provider code goes in the live package; this file is purely a
// re-export bridge (see types.go for the protocol types and Session runtime).

import (
	live "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// Concrete realtime providers (formerly internal/voiceagent.{GeminiLive,…}).
type (
	GeminiLive     = live.GeminiLive
	OpenAILive     = live.OpenAILive
	DeepgramLive   = live.DeepgramLive
	AssemblyAILive = live.AssemblyAILive

	// DeepgramAudioSettings configures the Deepgram listen/speak legs.
	DeepgramAudioSettings = live.DeepgramAudioSettings
)

// DefaultOpenAIRealtimeModel is the public runtime default for OpenAI-backed
// Voice Agent sessions.
const DefaultOpenAIRealtimeModel = live.DefaultOpenAIRealtimeModel

// Provider constructors are exposed as function values so this bridge stays
// signature-agnostic: changing a constructor's parameters in the live package
// does not require editing this file.
var (
	NewGeminiLive     = live.NewGeminiLive
	NewOpenAILive     = live.NewOpenAILive
	NewDeepgramLive   = live.NewDeepgramLive
	NewAssemblyAILive = live.NewAssemblyAILive
)
