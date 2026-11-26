# Android hybrid adapter

This optional adapter isolates Firebase AI Logic and ML Kit. Its host must gate
the feature off by default, provide an explicitly configured Firebase project,
and enforce the destination policy before invoking either generation method.
The device method uses `ONLY_ON_DEVICE` and a preview category; the cloud method
requires `allowCloud`. Neither method silently falls back. Device and cloud use
separate named Firebase apps with default data collection disabled. Play Integrity
App Check is installed only for the cloud app with automatic token refresh disabled.
A debug build can explicitly select
`AppCheckMode.DEBUG` for a registered debug token; the debug provider is excluded
from release variants and selecting it there fails closed. Project setup, application IDs,
signing certificates, Firebase AI Logic and App Check enrollment remain required.
Production must not use a debug attestation token. Managed endpoints are
separate from the explicitly configured Firebase project and its quotas.

Generation accepts text and optionally one bounded Bitmap. The Firebase SDK
owns image processing and the selected hybrid mode; SpeechKit reports its
observed inference source. Audio is transcribed before text generation. Hosts
own content acquisition, preview, and cropping; see the
[Android context actions](../../docs/client/android-context-actions.md).
The additional Bitmap parameter changes the public JVM signatures. Recompile
consuming applications with the updated AARs for this pre-1.0 change.

SDK failures are translated from documented exception classes or ML Kit error
codes to the shared execution failures. Unknown failures and policy, safety,
permission and cancellation outcomes are not eligible for fallback. The shared
router may retry a different destination only for side-effect-free generation;
agent turns with potential note effects must instead consult their receipt.

`readiness` and `download` address the selected category. AICore chooses the
model; the preview category alone does not prove a particular Nano version ran.
ML Kit's Agent API requires a supported, enrolled device and foreground app;
the host must hand off from background, keyboard or assistant surfaces as needed.
The SDKs are experimental or preview and may change incompatibly.

`LocalNoteAgent` runs a bounded, ephemeral single turn with self-only note
AppFunctions and the supplied durable Store. Pass only an authorized bounded
summary of completed text as context. The adapter never restores or replays
tool-bearing turns. Notes belong to the `device` scope. The host supplies an
operation ID and an explicit one-write grant for the Save action; ordinary turns
can only search notes. Tool calls verify their turn nonce and active coroutine,
and cannot consume another turn's grant. Note
and receipt are stored atomically and a recorded effect prevents rerunning the
turn. Remote or otherwise uncertain outcomes still need reconciliation. The
adapter does not implement multi-turn session persistence. A note search
checks at most the latest 100 notes. Device acceptance, actual model identity,
App Check enrollment and safe foreground handoff require real-device proof.
The pinned AppFunctions service extends an Android 16 platform class, so note
tools are gated to API 36 and newer even though basic AgentRunner inference has
a lower runtime minimum. The agent and its readiness/download checks share the
explicit preview/full model configuration. `readiness` checks the ML Kit model; an
available model is not proof that tool discovery or calling works on a device.

`ImportedAudio` uses the ML Kit on-device recognition client with explicit
locale and Basic or Advanced mode. It exposes independent model readiness and
download progress and accepts a content URI, closing its source after the turn.
The host must close the recognizer. Device, language and format support require
verification on the target device.

References: [Firebase hybrid Android](https://firebase.google.com/docs/ai-logic/hybrid/android/get-started),
[model categories](https://firebase.google.com/docs/ai-logic/hybrid/android/configuration-options),
[ML Kit agents](https://developers.google.com/ml-kit/genai/agent/android/get-started).
