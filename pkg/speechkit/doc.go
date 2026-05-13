// Package speechkit provides the public SDK for embedding SpeechKit voice
// capture, transcription, and assist/voice-agent pipelines into host
// applications.
//
// # Surface
//
// The kernel exposes three strict modes:
//
//   - Dictation — speech to text only, no AI rewriting.
//   - Assist — speech (or text) to a one-shot result, with optional TTS.
//   - Voice Agent — realtime audio-to-audio dialogue.
//
// Each mode is constructed via a small subpackage so host apps depend only
// on what they use:
//
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/dictation]
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/assist]
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent]
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit] (tool
//     registry, session memory, lifecycle hooks for Voice Agent hosts)
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/client] (HTTP
//     client for talking to a remote SpeechKit Server)
//
// # Central types in this package
//
// [Runtime] owns shared state and the event channel that host apps read
// from. [Engine] is the full voice pipeline; [RecordingController] and
// [TranscriptionWorker] can be composed independently for custom pipelines.
// [Catalog] exposes the provider/model/mode metadata for setup UIs and
// readiness checks; [RuntimePolicy] lets the host pin profiles and gate
// fallbacks.
//
// # Stability
//
// pkg/speechkit is the OSS public surface. Symbols here follow semver from
// v1.0 onward. Before v1.0 the surface may still evolve — see
// [CHANGELOG.md] and the release notes for breaking-change calls.
//
// [CHANGELOG.md]: https://github.com/kombifyio/SpeechKit/blob/main/CHANGELOG.md
package speechkit
