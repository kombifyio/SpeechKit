# SpeechKit Android Contract

This document maps every Kotlin interface in the Android SDK to its Go source-of-truth
in the same repository. The Go interfaces define the specification; Kotlin implements
them using idiomatic equivalents.

## Gradle module cut

SpeechKit is a developer framework: one owner per concern, reused by every
host (keyboard, system assistant, in-app test screens). Product UX stays in
`:app` (GPL glue) and Companion; these modules stay Apache-2.0.

Module cut, dependency rules, and what counts as an SDK break:
[docs/architecture/android-sdk-surface-boundary.md](../docs/architecture/android-sdk-surface-boundary.md).
This file maps Kotlin types to Go.

| Module | Owns | Must not |
|---|---|---|
| `:core` | PCM capture/playback (`AudioCapture`, `MicAudioCapture`, `PcmStreamPlayer`), engine, STT, VAD, `TurnEngine` (duplex turn-taking), store, `AudioSession` (start/stop buffer over the same capture), `VoiceLog` (`sk.voice`) | Compose UI, network transport |
| `:net` | Dictation/VA WebSocket, REST, `StoredServerProfile`, `VoiceAgentUiState` | Compose, `AudioRecord`, owning the connection algebra |
| `:voice-ui-compose` | Orb drawing (`VoiceAuraOrb`) | `:net` — session phases stay out so a protocol change does not force a visual module |
| `:ime` | Keyboard FSM/UI and the one adapter `VoiceAgentUiState.Phase.toAuraState()` | HeliBoard (`:heliboard`) |
| `:assistant` | Overlay, intent router, Assist `processAssist` via `GeneralQueryExecutor`; listen via `UtteranceTranscriber` + `DictationController` | Keyboard layout, a second Assist HTTP path, a private AudioRecord+batch STT stack |
| `:domain` | Connection contract: `ConnectionProfile`, `ConnectionMode`, resolve/infer, `ConnectionProfileSource` | Transport, Android prefs, invented placeholders |
| `:app` | GPL glue: HeliBoard, Settings, test screens | Reimplementing capture, playback, orb mapping, or Assist |

Hosts that need PCM import `io.kombify.speechkit.audio`.

On-device diagnosis uses one logcat tag. Filter:

```
adb logcat -s sk.voice
```

`:core` `io.kombify.speechkit.log.VoiceLog` owns it. Areas: `assist`, `dictation`,
`voiceagent`, `audio`, `net`. Lines carry HTTP status, WS failure, capture
codes, host URL, and `token=present|absent` — never PCM, bearer secrets, or
transcript text. Empty listen, no speech, timeout, and stream failure are
distinct overlay reasons so a device test can tell them apart.

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
| `core/audio/AudioSession.kt` :: `AudioFormat` | Audio constants in `internal/audio/` (`STREAM_CHUNK_BYTES` is Android-only: 100 ms at the pipeline rate) |
| `core/audio/AudioCapture.kt` / `MicAudioCapture.kt` / `PcmStreamPlayer.kt` | Android-only streaming adapters. Cold `frames()` vs `AudioSession` start/stop buffer; hosts must not each own an `AudioRecord` / `AudioTrack` |

### Voice Activity Detection

| Kotlin | Go Source |
|---|---|
| `core/vad/VadDetector.kt` :: `VadDetector` | `internal/vad/silero.go` :: `Detector` |
| `core/vad/VadDetector.kt` :: `VadConfig` | Threshold constants in `internal/vad/silero.go` |
| `core/vad/SileroVadDetector.kt` | `internal/vad/silero.go` |
| `core/vad/LevelVadDetector.kt` | `internal/vad/level_vad.go` :: `LevelVAD` |

`LevelVadDetector` is the **default endpointer on Android**, not a fallback.
Model weights are never bundled into a release, so `SileroVadDetector` throws
in its constructor on a fresh install — the exact state the system assistant
and the keyboard hit on first use. The level detector needs no model and
mirrors the Go thresholds (0.004 / 0.012, 400 ms hangover) so a dictation does
not endpoint differently on Android than on the Device target.

### Turn Taking (duplex dialogue)

| Kotlin | Go Source |
|---|---|
| `core/turn/TurnEngine.kt` :: `TurnEngine` | none — Android-only |
| `core/turn/TurnEvent.kt` :: `TurnEvent` | none — Android-only |

No Go counterpart. The desktop host solves the same problem with a speaker
echo guard that suppresses microphone uplink while `speaking` plus a 180 ms
tail (`docs/voiceagent-session-fsm.md`), and that document already names its
own successor: "a future AEC-backed implementation can re-enable interruption
when the input is confidently human speech rather than speaker bleed." The
Android engine is that implementation, and the desktop guard should converge
on it rather than the reverse.

`TurnEngine` is a **finished endpoint, not a part**. It owns onset
confirmation, the pre-roll ring, endpointing, and barge-in arbitration, and
exposes no threshold, window, or tail to its consumer — a surface renders
`TurnEvent`s and forwards `TurnEvent.TurnAudio`. It opens no audio device: the
host still owns `MicAudioCapture` and `PcmStreamPlayer` per the surface
boundary. Rationale, the derivation of the 600 ms pre-roll, and the echo
model: [docs/architecture/android-duplex-turn-engine.md](../docs/architecture/android-duplex-turn-engine.md).

`MicAudioCapture(duplex = true)` is the capture chain a conversation needs:
`VOICE_COMMUNICATION` plus `AcousticEchoCanceler`. The default stays
`VOICE_RECOGNITION`, which carries no AGC or noise suppression, because that
preprocessing costs transcription accuracy in every other mode.
`MicAudioCapture.echoControl` reports the result and is what `TurnEngine` is
constructed with, so a host declares a capability instead of picking a number.

`assistant/service/UtteranceTranscriber.kt` still runs its own VAD loop. It is
a one-shot dictation capture with continuous uplink, not a duplex dialogue, so
its uplink policy differs from the engine's; migrating it changes what reaches
the STT provider and needs device evidence on transcription accuracy.

### Speech-to-Text

| Kotlin | Go Source |
|---|---|
| `core/stt/SttProvider.kt` :: `SttProvider` | `internal/stt/provider.go:10-19` :: `STTProvider` |
| `core/stt/SttProvider.kt` :: `TranscribeOpts` | `internal/stt/provider.go:22-25` :: `TranscribeOpts` |
| `core/stt/SttProvider.kt` :: `LANGUAGE_MULTI` / `isMultilanguage` | `internal/config/defaults.go` `STTLanguage: "multi"`; `internal/server/assist/handler.go` `case "multi", "auto"` |

`TranscribeOpts.language` **must not default to a locale.** It defaulted to
`"de"`, so every call site that omitted the argument silently pinned German —
the root of a defect that recurred repeatedly, and a silent data loss (the same
English audio returns a zero-length transcript when pinned, HTTP 200, no
error). The value is multilanguage; an explicit user pin stays supported but is
never inferred. Translation is **per provider**, not one global string:
`multi` is Deepgram's spelling, while the Android platform recognizer expresses
multilanguage by passing no language tag at all
(`SystemSpeechRecognizerSession.toLanguageTag` returns null). Gated by
`core/src/test/.../stt/MultilanguageContractTest.kt`.
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

### Streaming Dictation

| Kotlin | Go Source |
|---|---|
| `core/stt/streaming/StreamingSttSession.kt` :: `StreamingSttSession` | `pkg/speechkit/dictation_streaming.go` :: `DictationStream` |
| `core/stt/streaming/StreamingSttSession.kt` :: `TranscriptEvent` | Stream frames in `docs/server/asyncapi.dictation-stream.v1.yaml` |

**Android-only contract amendment — `capturesOwnAudio` (B-M2b).**
`StreamingSttSession.capturesOwnAudio` (default `false`) has no Go
counterpart: the Go kernel always receives pushed PCM. A session that records
microphone audio itself — the system `SpeechRecognizer` tier
(`core/stt/system/SystemSpeechRecognizerSession.kt`) — returns `true`.
Callers MUST skip their own AudioRecord capture for such sessions and treat
`sendAudio` as a no-op; decorators (`KeepAliveSession`,
`RetryingDictationSession`) forward the delegate's value instead of
inheriting the default.

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

| Kotlin | Status | Purpose |
|---|---|---|
| `assistant/service/SpeechKitAssistant.kt` | present | Android VoiceInteractionService |
| `assistant/service/SpeechKitAssistantSession.kt` | present | The activation session. Hilt-injected through the session **service** (`@AndroidEntryPoint`) and handed to the session by constructor. Listen uses `UtteranceTranscriber` + `DictationController` (same streaming session as the keyboard); on-device skips `AudioCapture` because `capturesOwnAudio`. Overlay mounts via `ServiceWindowOwner`. |
| `voice-ui-compose/.../VoiceAuraOrb.kt` | present | Compose implementation of the canonical Voice Assistant orb. Not a Go mapping: its specification is `clients/typescript/packages/voice-ui/src/tokens/tokens.json` → `assistant` (layer stack, per-state colour pairs, animation periods, level formulas), shared with the web element `speechkit-voice-assistant`. Changing one without the other is visual drift. Session phases map onto `VoiceAuraState` only in `:ime` `toAuraState()` so `:voice-ui-compose` never depends on `:net`. |
| `core/stt/SttRouter.kt` :: `connectivityCheck` | present | Android ConnectivityManager |
| `keyboard/voicepanel` + HeliBoard fork integration | present | Inline dictation and Voice Agent via `SpeechKitVoiceBridge` in the HeliBoard fork; `:app` `InlineVoicePanel` hosts the IME panel. The fork is `:heliboard` in this Gradle build. The voice-only IME is removed from the system picker so onboarding selects the typing keyboard. |
| `net/DictationWsClient.kt` | present | Client of the server's streaming Dictation WS (`docs/server/asyncapi.dictation-stream.v1.yaml`); implements `StreamingSttSession`, mirrors Go `speechkit.DictationStream` |
| `core/stt/system/SystemSpeechRecognizerSession.kt` | present | System on-device STT tier (B-M2b): platform `SpeechRecognizer` behind `StreamingSttSession` (`capturesOwnAudio = true`); API 31+ on-device recognizer when available, `EXTRA_PREFER_OFFLINE` fallback below; unit-testable via the `SpeechRecognizerHandle` seam |
| `core/stt/system/SystemSttSupport.kt` | present | API 33+ `checkRecognitionSupport` / `triggerModelDownload` surface for the B-M4 onboarding language downloads (null / no-op below 33) |
| `domain/ConnectionProfile.kt` + `ConnectionMode.kt` | present | Shared connection contract. Flavor-bound resolution lives in `:app` DI; `:net` `StoredServerProfile` is the prefs adapter. `oss` = on-device; `kombify` uses persisted `connection_mode`. Companion is applied only after explicit Connect. See `docs/android-connect-distribution-standard.md`. |
| Pairing/auth provisioning | present | kombify tester APKs dial `https://speechkit.kombify.io` with a shared, revocable bearer baked at Firebase / `assemble-tester.ps1`. Companion `provision()` supplies the user JWT only when the user Connects Cloud or Companion launches `speechkit://connect/kombify`. Homelab: Settings can still browse `_speechkit._tcp` and override. OSS stays local-only. |

## Drift Detection

When modifying Go interfaces, check this contract and update the Kotlin counterparts.
The CI pipeline should eventually run a drift-check comparing Go interface signatures
with Kotlin interface signatures.
