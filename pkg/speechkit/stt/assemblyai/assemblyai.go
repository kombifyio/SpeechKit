// Package assemblyai is the AssemblyAI provider for SpeechKit: sync
// transcription, speaker streaming with attribution, and live dictation with
// optional LLM Gateway turn cleanup.
//
// In v0.64.0 the identifiers below alias the implementation that still lives
// in pkg/speechkit/stt. v0.65.0 moves the implementation here and deletes the
// old names, so code written against this package needs no further change.
package assemblyai

//lint:file-ignore SA1019 This package exists to alias the names deprecated in
// pkg/speechkit/stt; flagging its own aliases would defeat the migration window.

import "github.com/kombifyio/SpeechKit/pkg/speechkit/stt"

// Provider transcribes through AssemblyAI.
type Provider = stt.AssemblyAIProvider

// StreamingLLM configures LLM Gateway cleanup on realtime dictation turns.
type StreamingLLM = stt.AssemblyAIStreamingLLM

// DefaultTurnCleanupPrompt is the prompt used when StreamingLLM is enabled
// without one.
const DefaultTurnCleanupPrompt = stt.DefaultAssemblyAITurnCleanupPrompt

// New returns an AssemblyAI provider for apiKey. models is the
// comma-separated model list; empty uses the provider default.
func New(apiKey, models string) *Provider { return stt.NewAssemblyAIProvider(apiKey, models) }
