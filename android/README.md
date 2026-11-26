# SpeechKit Android

The Android half of the SpeechKit developer framework: Apache-2.0 Gradle
modules a host app depends on, plus one GPL reference APK (`:app` + the
HeliBoard fork) that proves they work. The Go `pkg/speechkit` tree is the
specification; [CONTRACT.md](CONTRACT.md) maps every Kotlin interface here to
its Go source of truth.

## Which module do I need?

| I want to… | Module | Coordinate |
|---|---|---|
| Resolve which SpeechKit Server to talk to (local, LAN, cloud) | `:domain` | `io.kombify.aiplatform.speechkit:domain` |
| Capture microphone PCM, play PCM, run VAD / turn-taking, log with `sk.voice` | `:core` | `io.kombify.aiplatform.speechkit:core` |
| Speak the Dictation and Voice Agent wire protocol (WebSocket + REST) | `:net` | `io.kombify.aiplatform.speechkit:net` |
| Draw the voice orb in Compose | `:voice-ui-compose` | `io.kombify.aiplatform.speechkit:voice-ui-compose` |
| Co-install with a companion app across the GPL boundary (AIDL contract only) | `:coinstall` | `io.kombify.aiplatform.speechkit:coinstall-contract` |
| Ship a voice-only keyboard (IME) | `:ime` | in-repo host; not published |
| Ship a system assistant (`VoiceInteractionService`, overlay, Assist) | `:assistant` | in-repo host; not published |
| Add Firebase hybrid generation and ML Kit local audio/agent capabilities | `:hybrid` | `io.kombify.aiplatform.speechkit:hybrid` (optional; experimental SDKs) |

`:ime` and `:assistant` are SpeechKit-owned hosts, not libraries: they show
how a keyboard and an assistant compose the published modules. Other apps
build their own UX on top of the same modules. `:app`, `:heliboard`
and `:test-shared` are the reference APK and test glue — start a new host
from the published modules, never by copying `:app`.

## Consume from another repository

JitPack is the public channel; it resolves anonymously from the public mirror.

```kotlin
repositories { maven { url = uri("https://jitpack.io") } }

dependencies {
    implementation("com.github.kombifyio.SpeechKit:core:<version>")
    implementation("com.github.kombifyio.SpeechKit:net:<version>")
    // Optional Google on-device / hybrid capabilities:
    implementation("com.github.kombifyio.SpeechKit:hybrid:<version>")
}
```

GitHub Packages carries the same artifacts for internal lanes but needs a
token even for public reads, so it is not a public distribution path.
Vendoring a module or wiring `includeBuild` against this tree is unsupported.
Details, artifact list and the co-install AIDL caveat:
[docs/architecture/android-sdk-surface-boundary.md](../docs/architecture/android-sdk-surface-boundary.md).

## Build

The native hybrid build and its explicit device/project prerequisites are
documented in [the hybrid build guide](../docs/hackathon/build-hybrid-ai-2026.md).
The feature defaults off in ordinary builds. A configured preview category and
a successful build alone do not prove Gemini Nano 4 execution.

Requirements: JDK 17, Android SDK (the Gradle wrapper pins the Gradle and AGP
versions).

```bash
# Framework modules only — works on a fresh clone and on the public mirror.
./gradlew -p android :core:assemble :net:assemble :domain:assemble \
    :voice-ui-compose:assemble :coinstall:assemble

# Unit tests for the framework modules.
./gradlew -p android :core:testDebugUnitTest :net:testDebugUnitTest :domain:testDebugUnitTest

# Reference APK (needs the GPL HeliBoard fork).
git submodule update --init android/heliboard
./gradlew -p android :app:assembleOssDebug
```

The reference APK also fetches the sherpa-onnx runtime for its optional on-device
neural voices (assistant replies spoken offline once a voice is downloaded in
Settings): `:app:assembleOssDebug` downloads the pinned
`sherpa-onnx-static-link-onnxruntime` AAR from the sherpa-onnx GitHub release into
`app/build/`, checks its SHA-256 and keeps only the arm64-v8a and x86_64 libraries.
Nothing of it is committed, and the framework modules do not depend on it.

`settings.gradle.kts` includes `:heliboard` and `:app` only when the submodule
is present, so a clone without it still configures and builds every
Apache-2.0 module. `jitpack.yml` at the repository root builds exactly the
published set.

## Connection setup

The native setup behind `-PspeechkitConnectionSetupEnabled=true` has three
choices: device-only, your own connection, and a hosted account. The flag is
off by default. Voice profiles and model variants come from SpeechKit's existing
provider catalog, shown in dropdowns. Connected endpoints supply the same
`/v1/catalog/profiles?mode=voice_agent` contract and `/v1/catalog/readiness`;
catalog membership alone never means that an endpoint is ready. Before a server
is configured, Android reads a generated projection of the same Go catalog.
Regenerate it with `go run ./examples/provider-catalog --json --mode voice_agent`
and verify it with `--check-output android/net/src/main/resources/speechkit-voice-profiles.json`.

Your own connection can explicitly activate the configured Firebase AI Logic
project, use a Gemini API key for direct Live voice and text generation, or use
a SpeechKit-compatible HTTP/WebSocket endpoint.
An arbitrary OpenAI-compatible URL is not a SpeechKit voice endpoint. Gemini
requires no hosted gateway or account; other managed voice providers remain
available through a SpeechKit endpoint.

Both app distributions use this selection. Account connection first attempts the
installed Companion's public IPC provisioning, then the existing browser device
grant when a public `speechkit.cloudAuthClientId` is configured. Installing
Companion does not silently change the selected route. Own provider keys and
new endpoint bearer tokens are encrypted with Android Keystore and stored
outside Android backup. The APK contains no embedded Gemini provider key.

Selecting device-only closes active remote sessions. Switching remote accounts
also drains the previous route before committing credentials, policy and a new
revision. Captured UI profiles cannot admit work after that revision changes.
Firebase activation also requires the current build's hybrid and cloud flags;
a previously stored project cannot override a disabled build. Local recognition uses the dedicated on-device recognizer or a downloaded
Parakeet model. An unavailable local model stays unavailable; Gemini Live is a
remote voice service and does not become an offline model. The hybrid text
engine uses Firebase AI Logic's four inference modes for the selected Firebase
project. Its cloud-first text mode falls back when offline according to the SDK
contract; it does not promise fallback after every provider error. Other selected
endpoints use SpeechKit's existing routing policy and available Firebase device
inference. Authorization and safety denials do not trigger provider retries.

Firebase Live uses the official SDK's sessions, activity signals and response
stream, with SpeechKit's existing audio capture, playback and conversation state.
Firebase cloud audio transcription uses `generateContent`; local file transcription
uses ML Kit Speech Recognition. Live audio and audio-input transcription are not
part of Firebase's automatic on-device text fallback. SpeechKit coordinates those
capabilities explicitly and does not replay dispatched agent actions.

### Reusable local speech adapters

`:core` owns `LocalSpeechAdapter`, bounded audio normalization and the existing
Phonon/Parakeet model-pool adapter. `:net` owns `LocalSpeechRouter` and destination
policy. The optional, separately published `:hybrid` AAR supplies
`MlKitLocalSpeechAdapter` and Firebase hybrid text; `:net` never depends on it.
A host composes the installed SDKs once during `Application.onCreate`:

```kotlin
LocalSpeechAdapters.install { app -> listOf(MlKitLocalSpeechAdapter(app)) }
// Pass the same selection before opening and when starting the segment.
val options = DictationSegmentOptions(language = "de-DE")
val session = DictationController(profile, context = context).openSession(options)
session.startSegment(options)
// Collect events once; send 16 kHz mono S16 PCM only if !session.capturesOwnAudio.
// Finish the segment, then close the session when leaving the host.
```

Hosts may instead construct `LocalSpeechRouter` with their own adapters and inject
it into `DictationController`. Microphone segments and imported recordings share
the same local routing. Automatic mode tries an installed compatible native model
and then ready Google ML Kit recognition. Only unavailable/transient failures may
retry the same bounded recording, before one final result is committed. Local first
exhausts the local chain before a permitted cloud attempt; cloud first can fall back
to that chain. Exclusive modes never cross destinations. No retry downloads models.

An explicit system-recognizer preference retains Android's microphone-owning live
recognizer. It cannot replay captured audio after failure: retry starts a new
recording. `capturesOwnAudio` stays fixed throughout each session. Google ML Kit
requires a supported explicit locale; multi-language mode never silently becomes
the device language. English-only Phonon requires an English selection. File import
defaults to multi and offers an explicit language choice before starting.

Local decoded recordings are limited to two minutes at 16 kHz mono S16, with a
14 MiB encoded-input bound. Exceeding local capacity permits cloud fallback only
when the invocation already allows it; corrupt input and permission denials do not.
Receipts identify the actual model or Google mode used. Cancellation and connection
revision changes discard unfinished results.

This pre-1.0 SDK change requires recompiling consuming applications together with
the versioned AAR update: `DictationController.openSession` gains segment options
and `ExecutionResult` gains adapter provenance. Existing compiled JVM clients must
not be mixed with the new artifacts.

### Keyboard shortcuts and gestures

The idle shortcut panel offers dictation, Voice Agent, summarize, rewrite and
correct. Opal appears during a voice session. Text actions use selected text, or
the current field when nothing is selected, and present a foreground preview
before insertion. The editor contents, selection, field identity and captured
connection revision must still match before insertion. Password fields and
editors without a verifiable complete snapshot are refused.

| Preference | Tap | Hold |
|---|---|---|
| Cloud first (default after connecting) | Prefer cloud, with the supported local fallback | This invocation is local only |
| Local first | Prefer device, with permitted cloud fallback | This invocation is cloud only |
| Local only | Device only | Hold to talk or communicate on device |
| Cloud only | Cloud only | Hold to talk or communicate in cloud |

A 650 ms ring, destination symbol and haptic confirmation indicate an armed hold;
dragging away cancels it. Gestures never persist a different preference. Exclusive
modes never fall back to the other destination. Missing local capability remains
an explicit failure. Local voice captures in the IME and passes completed text to
the existing foreground local agent after release, without starting another mic.

The direct adapter defaults to `gemini-3.8-live` with the `Kore` voice and uses
16 kHz PCM input, 24 kHz PCM output and manual activity signals for interruption.
Its connection test performs a real setup handshake without microphone capture.
It does not expose managed sequences or tools. Provider eligibility, quota,
device model readiness and a real voice turn must be verified separately from
the setup UI journey. Protocol references: [Live API](https://ai.google.dev/api/live)
and [WebSocket setup](https://ai.google.dev/gemini-api/docs/live-api/get-started-websocket).

## Licensing boundary

Everything outside `:app` and `:heliboard` is Apache-2.0. `:heliboard` is a
GPL-3.0 fork ([HELIBOARD.md](HELIBOARD.md)); an APK that links it is GPL as a
whole, so no proprietary code may enter `:app`. `:coinstall` exists so a
companion app and the keyboard can share one AIDL contract without linking
each other across that line. See [GPL-NOTICE.md](GPL-NOTICE.md).

## Diagnose on device

```
adb logcat -s sk.voice
```

One tag, owned by `:core`'s `VoiceLog`. Lines carry HTTP status, WebSocket
failure, capture codes and `token=present|absent` — never PCM, bearer secrets
or transcript text.

## Read next

- [CONTRACT.md](CONTRACT.md) — Kotlin ↔ Go interface map and module ownership table.
- [docs/architecture/android-sdk-surface-boundary.md](../docs/architecture/android-sdk-surface-boundary.md) — dependency rules, published artifacts, what counts as an SDK break.
- [HELIBOARD.md](HELIBOARD.md) — how the fork is tracked and rebased.
- [docs/server/README.md](../docs/server/README.md) — the server these clients talk to.
