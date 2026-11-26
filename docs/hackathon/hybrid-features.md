# SpeechKit Hybrid features

SpeechKit Hybrid extends the native Android keyboard and the reusable Android
SDK with explicit cloud and device execution. It adds dictation, text assistance,
and voice-agent controls where people already write.

This reference describes the development source as of 2026-11-26. The source is
integrated, but the features remain disabled in ordinary builds. Implementation
does not establish device availability or a released feature. The
[build and acceptance guide](build-hybrid-ai-2026.md) identifies the remaining
checks. Web and iOS are outside this Android entry.

## New work and existing foundation

SpeechKit already had a native keyboard based on HeliBoard, dictation controllers,
downloaded local recognizers, a remote voice protocol, and Room storage. Those
components provide the input, audio, and persistence foundation.

The hackathon work started on 2026-11-24. It adds the Firebase hybrid integration,
local agent tools and receipts, connection setup, invocation gestures, keyboard
text actions, and a common adapter contract for local speech. Opal is an existing
SpeechKit voice visual. Its compact keyboard integration is part of this work.

## Feature and evidence matrix

| Capability | Implemented behavior | Observed evidence and limits |
| --- | --- | --- |
| Connection setup | Local use, own keys or endpoints, and a Kombify account are separate choices. Configured Firebase is the default Gemini method. | Installed-app setup journey passed. Real Kombify account and managed-endpoint acceptance remain open. |
| Voice selection | Provider and model dropdowns use the existing SpeechKit catalog and connection readiness. Selection belongs to the admitted connection. | Setup exercised catalog selection and key custody. Catalog presence does not prove every provider works on Android. |
| Keyboard shortcuts | Idle actions include dictation, Voice Agent, summarize, rewrite, and correct. Text actions preview before insertion and recheck the editor. | Keyboard opening, typing, and compact Opal were observed. The final full shortcut and gesture journey remains open. |
| Voice panel | Active voice sessions show compact Opal in the shortcut strip. An expanded mode replaces the keys. | Compact mode was observed. Two device journeys failed to reach expanded mode. A corrected touch-test helper awaits a run. |
| Firebase text assistance | Text requests use the official SDK's four inference modes and report the actual inference source. | An earlier physical-device candidate returned a Firebase cloud result with App Check. Current-candidate SDK acceptance and Nano 4 device generation remain open. |
| Gemini voice | Firebase Live uses the official session API. An own Gemini key uses a separate direct adapter. | An earlier direct-key candidate passed audio response, interruption, audio follow-up, and stale-profile rejection. This does not verify Firebase Live. |
| Local speech adapters | Installed compatible Phonon or Parakeet models and ready ML Kit recognition share bounded audio and typed results. | Normalization and readiness ran on a device. No native or ML Kit model was ready, so that run did not perform recognition. |
| Imported recordings | Local decoding and recognition use the same adapters as microphone segments. The user selects a language or multi-language mode. | A synthetic WAV decoded from 8 kHz stereo to 16 kHz mono. Malformed PCM and local size limits were checked. Full file transcription and summary remain open. |
| Local notes and recovery | Local tools persist notes and receipts. Remote interrupted actions retain their outcome state and are not silently replayed. | Earlier disk migration and durable-storage journeys passed. Complete local-agent and remote-reconciliation demonstrations remain open. |

## Connection and execution are different settings

The connection selects credentials and the remote service. The execution policy
selects where a task may run. Own-key Gemini access does not require a SpeechKit
gateway. A SpeechKit endpoint can expose its own compatible capabilities.

Local-only setup requires no cloud key or account. A supported local model must
still be installed and ready. Connected setup defaults to Cloud First. A gesture
changes one invocation, never the saved preference.

| Saved policy | Short tap | Long hold |
| --- | --- | --- |
| Cloud First | Cloud, then eligible local fallback | Device only for this invocation |
| Local First | Device, then permitted cloud fallback | Cloud only for this invocation |
| Local Only | Device only | Hold to talk or communicate on device |
| Cloud Only | Cloud only | Hold to talk or communicate in cloud |

The hold control arms after 650 ms with a progress ring, a destination symbol,
and haptic feedback. In a preferred mode, release dispatches the alternative
destination action. In an exclusive mode, voice and dictation capture starts
when the hold arms; release completes that capture or turn. Text actions
dispatch on release. Dragging away cancels an unfinished gesture or capture.
The lock symbol identifies device-only execution. An unavailable local model
produces an availability result, not permission to send the request elsewhere.
For text actions, the hold communicates the selected text rather than starting
a microphone recording.

## Firebase owns model inference

Text assistance maps the policy to `PREFER_IN_CLOUD`, `PREFER_ON_DEVICE`,
`ONLY_ON_DEVICE`, or `ONLY_IN_CLOUD`. SpeechKit checks the SDK-reported inference
source and retains its own connection and editor admission rules.

Firebase's cloud-preferred mode falls back when the device is offline. It is not
a general retry rule for every server error. Authentication, safety, and quota
denials do not trigger automatic provider replay. SpeechKit does not promise a
fixed fallback latency.

Firebase Live and cloud audio transcription use separate official SDK adapters.
The hybrid text modes do not turn a Live session into an offline voice agent.
Local transcription uses native recognition or ML Kit; local text generation
uses the Firebase on-device path. Local agent execution has its own readiness
requirements. Each capability must be admitted separately.

See [Firebase inference modes](https://firebase.google.com/docs/ai-logic/hybrid/android/configuration-options),
[Firebase Live](https://firebase.google.com/docs/ai-logic/live-api), and
[ML Kit speech recognition](https://developers.google.com/ml-kit/genai/speech-recognition/android).

## Local redundancy stays local

Destination routing and local-engine selection are separate. In automatic local
selection, the router tries compatible installed native models before ready
Google recognition. Local First exhausts eligible local candidates before an
allowed cloud attempt. Local Only never crosses that boundary.

Buffered adapters reuse the same recording before any final transcript is
committed. They retry only an unavailable or transient failure. Cancellation,
permission denial, invalid input, and silence do not cause blind retries.
Adapters do not download models during inference.

The classic Android system recognizer owns its microphone. Its explicit
selection remains available, but a failed session needs a new recording. It
does not promise replay of the failed audio into another recognizer.

Language selection is part of admission. English-only Phonon requires explicit
English. ML Kit requires an explicitly supported locale. Multi-language input
never silently becomes the device language. Local input is bounded to 120 seconds
of 16 kHz mono PCM and 14 MiB of encoded data. These are SDK limits, not measured
performance claims.

## Reusable Android modules

| Module | Responsibility |
| --- | --- |
| `domain` | Connection profiles and execution policy. |
| `core` | Local speech contract, bounded decoding, installed native recognition, and typed availability. |
| `net` | Shared controllers, local routing, credential custody, and remote/Firebase adapters. |
| `hybrid` | Optional Firebase on-device text, ML Kit speech and agent integration, and local tools. |
| App, IME, and assistant hosts | Compose the modules, request permissions, present results, and protect editor ownership. |

`LocalSpeechAdapter` accepts bounded audio, performs one local attempt, and
returns the actual adapter identity. `LocalSpeechRouter` chooses eligible
adapters; it does not grant network permission. A host can install the optional
ML Kit adapter or inject its own router without changing keyboard UI code.

The hybrid AAR and its source, POM, and Gradle metadata build successfully. Its
publication configuration is present; no new artifact release is claimed here.
Consumers must recompile for this pre-1.0 API change. Framework modules retain
Apache-2.0 licensing. The reference APK links GPL-3.0 HeliBoard; Companion remains
across the existing IPC boundary. See the [module contract](../../android/CONTRACT.md)
and [Android SDK guide](../../android/README.md).

## Submission limits

The Android hackathon demonstration still requires actual Nano 4 inference
through Firebase on a supported, provisioned device. A successful build, an
unavailable-model diagnostic, or a direct Gemini request cannot replace that
proof. The final SDK journey, expanded-panel interaction, and complete offline
flow remain open. No latency, battery, accuracy, or production-readiness claim
has been measured for this candidate.
