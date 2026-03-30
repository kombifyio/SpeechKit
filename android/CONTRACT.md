# SpeechKit Android Contract

This document maps every Kotlin interface in the Android SDK to its Go source-of-truth
in the same repository. The Go interfaces define the specification; Kotlin implements
them using idiomatic equivalents.

## Type Mapping Conventions

| Go | Kotlin |
|---|---|
| `<-chan T` | `SharedFlow<T>` |
| `context.Context` | Coroutine cancellation via `CoroutineScope` |
| `sync.Mutex` | `kotlinx.coroutines.sync.Mutex` |
| `struct` (mutable) | `data class` (immutable) |
| `interface` | `interface` with `suspend fun` |
| `error` return | `throws` / `Result<T>` |
| `//go:build kombify` | Gradle product flavor `kombify` |
| `time.Duration` | `kotlin.time.Duration` |
| `time.Time` | `java.time.Instant` |

## Interface Mapping

### Engine (Core Runtime)

| Kotlin | Go Source |
|---|---|
| `core/engine/Engine.kt` :: `Engine` | `pkg/speechkit/runtime.go:103-109` :: `Engine` |
| `core/engine/Engine.kt` :: `Event` | `pkg/speechkit/runtime.go:26-34` :: `Event` |
| `core/engine/Engine.kt` :: `EventType` | `pkg/speechkit/runtime.go:14-24` :: `EventType` consts |
| `core/engine/Engine.kt` :: `Command` | `pkg/speechkit/runtime.go:80-86` :: `Command` |
| `core/engine/Engine.kt` :: `CommandType` | `pkg/speechkit/runtime.go:64-78` :: `CommandType` consts |
| `core/engine/Engine.kt` :: `Snapshot` | `pkg/speechkit/runtime.go:36-48` :: `Snapshot` |
| `core/engine/Engine.kt` :: `CommandBus` | `pkg/speechkit/runtime.go:99-101` :: `CommandBus` |
| `core/engine/Runtime.kt` :: `Runtime` | `pkg/speechkit/runtime.go:117-124` :: `Runtime` |

### Audio Capture

| Kotlin | Go Source |
|---|---|
| `core/audio/AudioSession.kt` :: `AudioSession` | `internal/audio/capturer.go` :: `Session` |
| `core/audio/AudioSession.kt` :: `AudioFormat` | Audio constants in `internal/audio/` |

### Voice Activity Detection

| Kotlin | Go Source |
|---|---|
| `core/vad/VadDetector.kt` :: `VadDetector` | `internal/vad/silero.go` :: `Detector` |
| `core/vad/VadDetector.kt` :: `VadConfig` | Threshold constants in `internal/vad/silero.go` |

### Speech-to-Text

| Kotlin | Go Source |
|---|---|
| `core/stt/SttProvider.kt` :: `SttProvider` | `internal/stt/provider.go:10-19` :: `STTProvider` |
| `core/stt/SttProvider.kt` :: `TranscribeOpts` | `internal/stt/provider.go:22-25` :: `TranscribeOpts` |
| `core/stt/SttProvider.kt` :: `Result` | `internal/stt/provider.go:28-35` :: `Result` |
| `core/stt/SttRouter.kt` :: `SttRouter` | `internal/router/router.go:27-39` :: `Router` |
| `core/stt/SttRouter.kt` :: `RoutingStrategy` | `internal/router/router.go:18-22` :: `Strategy` |

### Storage

| Kotlin | Go Source |
|---|---|
| `core/store/Store.kt` :: `Store` | `internal/store/types.go:22-42` :: `Store` |
| `core/store/Store.kt` :: `Transcription` | `internal/store/types.go:75-86` :: `Transcription` |
| `core/store/Store.kt` :: `QuickNote` | `internal/store/types.go:89-101` :: `QuickNote` |
| `core/store/Store.kt` :: `Stats` | `internal/store/types.go:103-110` :: `Stats` |
| `core/store/Store.kt` :: `ListOpts` | `internal/store/types.go:53-58` :: `ListOpts` |

### Dictation

| Kotlin | Go Source |
|---|---|
| `core/dictation/DictationSession.kt` :: `DictationSession` | `pkg/speechkit/dictation_session.go` |
| `core/dictation/DictationSession.kt` :: `SegmentCollector` | `pkg/speechkit/recording_controller.go` :: `SegmentCollector` |
| `core/dictation/DictationSession.kt` :: `AudioSegment` | Segment types in `pkg/speechkit/` |

### Voice Shortcuts

| Kotlin | Go Source |
|---|---|
| `core/shortcuts/VoiceShortcuts.kt` :: `ShortcutResolver` | `internal/shortcuts/resolver.go` :: `Resolver` |
| `core/shortcuts/VoiceShortcuts.kt` :: `ShortcutAction` | `internal/shortcuts/types.go` :: action constants |

### Configuration

| Kotlin | Go Source |
|---|---|
| `core/config/SpeechKitConfig.kt` :: `SpeechKitConfig` | `internal/config/config.go` :: `Config` |

## Android-Only Extensions

These interfaces have no Go counterpart (Android-specific):

| Kotlin | Purpose |
|---|---|
| `keyboard/ime/SpeechKitIME.kt` | Android InputMethodService |
| `assistant/service/SpeechKitAssistant.kt` | Android VoiceInteractionService |
| `core/stt/SttRouter.kt` :: `connectivityCheck` | Android ConnectivityManager |
| `cloud/auth/AuthProvider.kt` | Zitadel Device Code Flow (Android-specific) |

## Drift Detection

When modifying Go interfaces, check this contract and update the Kotlin counterparts.
The CI pipeline should eventually run a drift-check comparing Go interface signatures
with Kotlin interface signatures.
