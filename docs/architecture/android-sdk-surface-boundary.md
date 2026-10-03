# Android SDK Surface Boundary

Decision date: 2026-08-26. Twin of
[sdk-surface-boundary.md](sdk-surface-boundary.md) for the Gradle tree under
`android/`.

`pkg/speechkit` is the Go embedder boundary. This document is the Android
embedder boundary: which modules a host may depend on, which stay the
reference APK, and what a breaking change requires.

## Purpose

SpeechKit on Android is a developer framework plus one GPL reference app.
Companion, Wear, and any other host consume the Apache-2.0 modules. They must
not import `:app` or `:heliboard`.

The assembled `io.kombify.speechkit` APK is GPL-3.0 because it links the
HeliBoard fork. That license applies to the APK as a whole, not to the
framework modules. Those stay Apache-2.0 so another app can compile against
them without becoming a HeliBoard derivative. Co-installing with another app is
IPC through the `:coinstall` AIDL contract, never a Gradle dependency on
`:app`.

## Public modules

| Module | Public responsibility | May depend on |
|---|---|---|
| `:domain` | Connection algebra: `ConnectionProfile`, `ConnectionMode`, resolve/infer, `ConnectionProfileSource` | nothing else in `android/` |
| `:core` | PCM capture/playback, engine, STT, VAD, `TurnEngine`, store, `AudioSession`, `VoiceLog` (`sk.voice`) | nothing else in `android/` |
| `:net` | Dictation/VA WebSocket, REST, `StoredServerProfile`, `VoiceAgentUiState` | `:core`, `:domain` |
| `:voice-ui-compose` | Aura orb drawing only. Published as `io.kombify.speechkit:voice-ui-compose` | Compose only — never `:net` |
| `:ime` | Keyboard FSM/UI and the one adapter `VoiceAgentUiState.Phase.toAuraState()` | `:core`, `:domain`, `:net`, `:voice-ui-compose` — never `:assistant` |
| `:assistant` | Overlay, intent router, Assist `processAssist`, listen via `UtteranceTranscriber` + `DictationController` | `:core`, `:domain`, `:net`, `:voice-ui-compose` — never `:ime` |
| `:coinstall` | AIDL `speechkit.coinstall.v1` and constants. Published as `io.kombify.speechkit:coinstall-contract` | nothing else in `android/` |

Interface mapping to Go lives in [android/CONTRACT.md](../../android/CONTRACT.md).
This file owns the module cut and the dependency rules; CONTRACT maps types.

## Published artifacts

Every module in the public table is published as an Apache-2.0 AAR, versioned
from `.kombify/VERSION` like every other release artifact:

| Coordinate | Module | Why it is published |
|---|---|---|
| `io.kombify.speechkit:domain` | `:domain` | A host resolves which server to talk to with the same algebra the reference app uses |
| `io.kombify.speechkit:core` | `:core` | One microphone capture, one PCM player, one turn engine — the parts every voice surface needs |
| `io.kombify.speechkit:net` | `:net` | The dictation and Voice Agent wire clients, so a host does not re-implement the protocol |
| `io.kombify.speechkit:voice-ui-compose` | `:voice-ui-compose` | One orb for every surface, including hosts outside this repository |
| `io.kombify.speechkit:coinstall-contract` | `:coinstall` | Both apps must compile the same wire contract without linking each other across the GPL boundary |

### Where a consumer gets them

There are two registries and they are not interchangeable.

**JitPack is the public channel.** It resolves anonymously from the public
mirror, which is what an outside developer can actually use:

```kotlin
repositories { maven { url = uri("https://jitpack.io") } }

dependencies {
    implementation("com.github.kombifyio.SpeechKit:core:<version>")
}
```

**GitHub Packages is the internal lane.** Its Maven endpoint requires a token
even for public artifacts, so an outside developer hits 401 before they see a
package. Treat JitPack as the public channel.

`jitpack.yml` builds exactly the modules above. It cannot build `:app`,
because the HeliBoard fork's GPL sources are not mirrored — the mirror records
the revision in `android/heliboard.rev` — and `android/settings.gradle.kts`
drops `:app` when the submodule is absent. That guard is also what lets a
fresh clone configure the framework modules before running
`git submodule update --init`.

The artifactId `coinstall-contract` does not match its module name. That is
deliberate: what a consumer reads in a dependency block should say that this is
a contract and carries no implementation.

A published artifact is the only supported way to consume these modules from
another repository. Vendoring a snapshot or wiring an `includeBuild` against
this tree is not; a vendored copy drifts from the published contract.

Consuming the co-install contract needs both halves of the AAR — the compiled
stubs in `classes.jar` and the `.aidl` under `aidl/`. AGP ships the second only
for types named in `aidlPackagedList`, so `android/coinstall/build.gradle.kts`
lists every type in the contract. Dropping that list still produces a
building AAR; it just silently stops consumers from importing the contract
into their own `.aidl`.

## Reference-app modules (not a published SDK)

| Module | Role |
|---|---|
| `:app` | GPL glue: Settings, onboarding, test screens, `InlineVoicePanel` (the only class that imports both SpeechKit and HeliBoard) |
| `:heliboard` | HeliBoard fork. No SpeechKit module imports; the voice key calls `SpeechKitVoiceBridge` |
| `:test-shared` | Test helpers |

`:app` may depend on every public module plus `:heliboard`. A new host that is
not this APK starts from the public modules, not by copying `:app`.

## Boundary rules

1. Dictation, Assist, and Voice Agent stay separate modes. Sharing capture,
   playback, connection algebra, and the orb is reuse; sharing a session type
   across modes is not.
2. One PCM capture (`MicAudioCapture`) and one PCM player (`PcmStreamPlayer`)
   live in `:core`. Hosts import `io.kombify.speechkit.audio`. Do not add an
   IME- or assistant-shaped wrapper around those types.
   Capture defaults its rate and playback does not, because only one of them
   is fixed: every microphone path records at 16 kHz, while the Voice Agent
   downlink arrives at 24 kHz (`VoiceAgentAudio.SERVER_SAMPLE_RATE` in `:net`,
   mirroring `SERVER_SAMPLE_RATE` in the TypeScript client's `protocol.ts`).
   A host constructs `PcmStreamPlayer` with the rate of the stream it holds.
   There is no default to inherit, because an `AudioTrack` opened at the wrong
   rate raises nothing and fails no write — it just plays the agent slow and
   flat, and no test in this repo can hear that. The rate the track opened at
   is logged next to the capture rate, so `adb logcat -s sk.voice` is the
   diagnosis when a surface sounds wrong.
   Turn taking for a duplex conversation is `io.kombify.speechkit.turn`
   `TurnEngine`, not a per-host state machine either. A surface that mutes the
   microphone during playback, adds a tail after it, or decides for itself
   when a turn started or ended is reimplementing the engine and will get
   duplex wrong the way the Companion did. The engine opens no audio device;
   the host still owns capture and playback. See
   [android-duplex-turn-engine.md](android-duplex-turn-engine.md).
   Hosts that receive audio and control events on one collector use
   `PcmPlaybackQueue` for a separate playback coroutine. Its default backlog
   is two seconds of S16 mono, including the chunk currently being written;
   `offer` returns false on overflow so the host can terminate with a stable
   failure code. Clear the queue and flush the player together on interruption,
   and close the queue, cancel its consumer, and release the player on teardown.
   The player retains its constructor and `play`/`flush`/`release` signatures.
   Playback errors now throw `PcmPlaybackException` with a safe local code;
   hosts must handle it. Flush and release stop and release the held track
   before returning, and invalidate earlier waiting writes. Queue clearing also
   cancels an already handed-off chunk before it can start another player cycle.
   Voice Agent error frames carry an optional `fatal` flag (absent means false).
   Fatal errors stop playback and end the session; `authorization_expired`
   requires a fresh authenticated session, without renewal or replay.
   `VoiceAgentSession.start` waits up to 20 seconds for a listening state with
   `event_type=session_ready`; failure throws `VoiceAgentSetupException` before
   the host opens capture. Microphone sends before that event fail closed.
   Both exceptions retain only stable diagnostic codes.
3. `:voice-ui-compose` never depends on `:net`. Session phases map onto
   `VoiceAuraState` only in `:ime` (`toAuraState()`).
4. `:ime` and `:assistant` do not depend on each other. Keyboard dictation and
   the system overlay are different hosts of the same `:net` / `:core` path.
5. `:domain` does not import Android prefs, OkHttp, or placeholders. Flavor
   wiring and `StoredServerProfile` stay in `:app` / `:net`.
6. Public modules must not import `:heliboard` or `helium314.keyboard.*`.
7. Public binds and cloud paths fail closed in the host; OSS flavor stays
   local-only.
8. Do not log PCM, bearer tokens, or transcript text. Diagnosis is
   `adb logcat -s sk.voice`.

## Breaking changes

Removing or renaming an exported type, method, or AIDL member in a public
module requires a release-plan callout, the same way a `pkg/speechkit`
signature change does. Additive APIs are allowed in pre-1.0 patches.

`:app` and `:heliboard` are not a published contract. Changing them is not an
SDK break.

The voice reliability slice removes the public-source
`ImeVoiceAgentController.audio: Flow<ByteArray>` property and replaces it with
`consumeAudio(flush, play)`. Both repository hosts and controller tests migrate
to the cancelable, bounded `PcmPlaybackQueue` consumer; emitting from that
queue's per-frame child coroutine into a Kotlin `flow` would violate the flow
context invariant. `:ime` currently has no Maven publishing configuration and
is absent from the published artifact list, but it remains a public source
module: this removal is a source compatibility change and needs a release-plan
callout. It does not establish binary compatibility for the published `:core`
and `:net` artifacts; those retained signatures require their separate gate.

`:voice-ui-compose` visual tokens are specified in
`clients/typescript/packages/voice-ui/src/tokens/tokens.json`. Changing the
Compose orb without that file is visual drift, not a Gradle API break.

## Current verification

- Gradle module dependencies match the table above
  (`android/*/build.gradle.kts`).
- `:domain` has no SpeechKit module dependencies.
- `:voice-ui-compose` depends only on Compose.
- `:ime` does not depend on `:assistant`; `:assistant` does not depend on
  `:ime`.
- Hosts construct `MicAudioCapture` / `PcmStreamPlayer` from `:core`.
- Affected gate for this boundary: `:ime:testDebugUnitTest`,
  `:assistant:testDebugUnitTest`, `:app:compileKombifyDebugKotlin`.
