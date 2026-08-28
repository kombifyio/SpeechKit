package voiceagent

// This file re-exports the realtime Voice Agent provider implementations that
// now live in pkg/speechkit/voiceagent/live, so existing non-OSS call sites in
// cmd/speechkit, internal/server, and tools keep compiling without import
// churn. New provider code goes in the live package; this file is purely a
// re-export bridge (see types.go for the protocol types and Session runtime).

import (
	liveassemblyai "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/assemblyai"
	livedeepgram "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/deepgram"
	livegemini "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/gemini"
	liveopenai "github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live/openai"
)

// Concrete realtime providers (formerly internal/voiceagent.{GeminiLive,…}).
type (
	GeminiLive     = livegemini.Provider
	OpenAILive     = liveopenai.Provider
	DeepgramLive   = livedeepgram.Provider
	AssemblyAILive = liveassemblyai.Provider

	// DeepgramAudioSettings configures the Deepgram listen/speak legs.
	DeepgramAudioSettings = livedeepgram.AudioSettings
)

// DefaultOpenAIRealtimeModel is the public runtime default for OpenAI-backed
// Voice Agent sessions.
const DefaultOpenAIRealtimeModel = liveopenai.DefaultRealtimeModel

// Provider constructors are exposed as function values so this bridge stays
// signature-agnostic: changing a constructor's parameters in the live package
// does not require editing this file.
var (
	NewGeminiLive     = livegemini.New
	NewOpenAILive     = liveopenai.New
	NewDeepgramLive   = livedeepgram.New
	NewAssemblyAILive = liveassemblyai.New
)
