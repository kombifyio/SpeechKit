// Package speechkit provides the public SDK for embedding SpeechKit voice
// capture, transcription, and assist/voice-agent pipelines into host
// applications.
//
// # Surface
//
// The kernel exposes three strict interaction modes:
//
//   - Dictation — speech to text only, no AI rewriting.
//   - Assist — speech (or text) to a one-shot result, with optional TTS.
//   - Voice Agent — realtime audio-to-audio dialogue.
//
// The [Mode] enum carries two further constants that are capability
// surfaces, not interaction modes: [ModeTTS] exposes Text-to-Speech as a
// model-selection axis (its [IntelligenceVoiceOutput] contract is strictly
// text in, audio out), and [ModeNone] means no mode is selected.
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
// The root package is deliberately limited to contracts and value types: the
// [Mode], [Capability], [ProviderKind] and [ExecutionMode] enums, the
// [ProviderProfile] and [ModeSettings] descriptors, [RuntimePolicy], the
// pipeline contracts ([AudioRecorder], [Transcriber], [SegmentCollector],
// [TranscriptOutput], [TranscriptionObserver], [JobSubmitter]) and the value
// types that flow between them ([Submission], [Transcript], [TranscriptionJob],
// [Completion]). [Runtime] owns shared state and the event channel that host
// apps read from. Everything in this package can be imported by any other
// SpeechKit package without creating a cycle, so custom providers, collectors
// and outputs only need this one import.
//
// Implementations live in two sibling packages:
//
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/pipeline] — the
//     composable capture/transcription engine: RecordingController,
//     TranscriptionWorker, TranscriptionRunner, DictationSegmenter,
//     TranscriptSessionLedger and the live-commit policy.
//   - [github.com/kombifyio/SpeechKit/pkg/speechkit/catalog] — the
//     built-in provider/model catalog: Catalog, DefaultCatalog,
//     DefaultProviderProfiles, the provider defaults matrix and the model
//     registry with its freshness metadata.
//
// # Stability
//
// pkg/speechkit is the OSS public surface. Symbols here follow semver from
// v1.0 onward. Before v1.0 the surface may still evolve — see
// [CHANGELOG.md] and the release notes for breaking-change calls.
//
// [CHANGELOG.md]: https://github.com/kombifyio/SpeechKit/blob/main/CHANGELOG.md
package speechkit
