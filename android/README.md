# SpeechKit Android

The Android half of the SpeechKit developer framework: Apache-2.0 Gradle
modules a host app depends on, plus one GPL reference APK (`:app` + the
HeliBoard fork) that proves they work. The Go `pkg/speechkit` tree is the
specification; [CONTRACT.md](CONTRACT.md) maps every Kotlin interface here to
its Go source of truth.

## Which module do I need?

| I want to… | Module | Coordinate |
|---|---|---|
| Resolve which SpeechKit Server to talk to (local, LAN, cloud) | `:domain` | `io.kombify.speechkit:domain` |
| Capture microphone PCM, play PCM, run VAD / turn-taking, log with `sk.voice` | `:core` | `io.kombify.speechkit:core` |
| Speak the Dictation and Voice Agent wire protocol (WebSocket + REST) | `:net` | `io.kombify.speechkit:net` |
| Draw the voice orb in Compose | `:voice-ui-compose` | `io.kombify.speechkit:voice-ui-compose` |
| Co-install with the Companion app across the GPL boundary (AIDL contract only) | `:coinstall` | `io.kombify.speechkit:coinstall-contract` |
| Ship a voice-only keyboard (IME) | `:ime` | in-repo host; not published |
| Ship a system assistant (`VoiceInteractionService`, overlay, Assist) | `:assistant` | in-repo host; not published |

`:ime` and `:assistant` are SpeechKit-owned hosts, not libraries: they show
how a keyboard and an assistant compose the published modules. Companion owns
its own app, Wear and Car UX on top of the same modules. `:app`, `:heliboard`
and `:test-shared` are the reference APK and test glue — start a new host
from the published modules, never by copying `:app`.

## Consume from another repository

JitPack is the public channel; it resolves anonymously from the public mirror.

```kotlin
repositories { maven { url = uri("https://jitpack.io") } }

dependencies {
    implementation("com.github.kombifyio.SpeechKit:core:<version>")
    implementation("com.github.kombifyio.SpeechKit:net:<version>")
}
```

GitHub Packages carries the same artifacts for internal lanes but needs a
token even for public reads, so it is not a public distribution path.
Vendoring a module or wiring `includeBuild` against this tree is unsupported.
Details, artifact list and the co-install AIDL caveat:
[docs/architecture/android-sdk-surface-boundary.md](../docs/architecture/android-sdk-surface-boundary.md).

## Build

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

`settings.gradle.kts` includes `:heliboard` and `:app` only when the submodule
is present, so a clone without it still configures and builds every
Apache-2.0 module. `jitpack.yml` at the repository root builds exactly the
published set.

## Licensing boundary

Everything outside `:app` and `:heliboard` is Apache-2.0. `:heliboard` is a
GPL-3.0 fork ([HELIBOARD.md](HELIBOARD.md)); an APK that links it is GPL as a
whole, so no proprietary code may enter `:app`. `:coinstall` exists so the
Companion app and the keyboard can share one AIDL contract without linking
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
