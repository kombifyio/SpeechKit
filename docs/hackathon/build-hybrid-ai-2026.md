# SpeechKit native hybrid build

For the 2026-11-26 feature matrix, execution gestures, adapter responsibilities,
and current evidence limits, read [Hybrid features](hybrid-features.md).
The [Devpost packet](devpost-submission.md) supplies the English project copy,
existing-versus-new disclosure, media plan, and remaining form fields.

This build extends the existing native Android app, keyboard and system assistant
with explicit device/cloud execution and optional Google AI adapters. The work
started from source `6bb6f6365750550acefefa209291f04bf7188466` on 2026-11-24.
SpeechKit's existing dictation controllers, local recognizers, Room storage,
HeliBoard keyboard and remote voice protocol predate the hackathon.

## Build configuration

Use the repository's pinned Android toolchain and HeliBoard revision. The public
export carries the hybrid module's source and Gradle dependencies. Initialize the
keyboard as documented in [HELIBOARD.md](../../android/HELIBOARD.md).

Enable the module for the test build with Gradle property
`speechkitHybridEnabled=true` or `SPEECHKIT_HYBRID_ENABLED=true`. Ordinary builds
keep it disabled. The complete keyboard candidate also requires
`speechkitConnectionSetupEnabled=true` and `speechkitVoiceAgentOpalEnabled=true`
or their `SPEECHKIT_CONNECTION_SETUP_ENABLED` and
`SPEECHKIT_VOICE_AGENT_OPAL_ENABLED` environment equivalents.
Supply Firebase client configuration through these Gradle
properties or corresponding environment variables:

| Property | Environment |
| --- | --- |
| `speechkitFirebaseApplicationId` | `SPEECHKIT_FIREBASE_APPLICATION_ID` |
| `speechkitFirebaseProjectId` | `SPEECHKIT_FIREBASE_PROJECT_ID` |
| `speechkitFirebaseApiKey` | `SPEECHKIT_FIREBASE_API_KEY` |
| `speechkitFirebaseCloudModel` | `SPEECHKIT_FIREBASE_CLOUD_MODEL` |
| `speechkitFirebaseAllowCloud` | `SPEECHKIT_FIREBASE_ALLOW_CLOUD` |

Use a currently supported cloud model. Client configuration is separate from a
service account: never embed a service-account key, signing key or production
bearer in the build. Firebase cloud permission defaults false. The user also
controls network permission and the selected endpoint in the app.

```shell
cd android
./gradlew :hybrid:compileDebugKotlin :app:assembleOssDebug \
  -PspeechkitHybridEnabled=true \
  -PspeechkitConnectionSetupEnabled=true \
  -PspeechkitVoiceAgentOpalEnabled=true
```

This command leaves Firebase cloud access disabled. For the connected candidate,
also pass `-PspeechkitFirebaseAllowCloud=true` with the isolated Firebase client
configuration above and valid App Check setup. The app still requires an admitted
connection and a policy that permits cloud use. The flag does not grant network
permission to a Local Only invocation.

Register the exact application ID (`io.kombify.speechkit.oss` for the OSS flavor)
and build signing certificate in the isolated Firebase project. Enable Firebase
AI Logic through the official setup and configure App Check. Development/test
builds may use an explicitly registered debug token; release builds use Play
Integrity. Register judge access through the appropriate private channel, not
in this repository. Managed endpoints retain their existing admission.

## Device and capability requirements

The Firebase text path selects a Nano 4 preview category. Record the actual model
name and SDK-reported inference source on the target device. Model availability,
downloads, foreground status, language and preview enrollment affect capability;
an API level or phone brand alone does not establish readiness.

The current local note tool implementation uses AndroidX AppFunctions and is
gated to Android 16/API 36 or newer. Other local capabilities have separate
requirements. The native foreground surface handles model actions; keyboard and
assistant handoffs must preserve the input and avoid inserting into a different
editor. Missing local TTS leaves a readable result. Downloading a model is a
separate, explicit provisioning operation before an offline demonstration.

## Demonstration and acceptance

Record this journey on the actual supported phone after provisioning:

1. Show device, model readiness and the actual selected model.
2. Dictate and summarize in device-only mode with connectivity disabled.
3. Save a local note through the agent and read the stored note back.
4. Use the explicitly configured Firebase cloud route and show its observed source.
5. Interrupt a recoverable generation request and show allowed local recovery.
6. Interrupt a remote agent action. Keep an unknown outcome visible and do not
   replay it as a local tool action.
7. Import an audio file and exercise local transcription and summarization.

Repeat device-only mode with network available and observe attempted egress;
airplane mode alone cannot prove the absence of network attempts. Check cancel,
rotation, process recreation, editor/account/endpoint changes, denied microphone
permission, model download/unavailability and English/German output quality.
Record source SHA, APK checksum, OS/device/model, App Check mode and outcomes.

The device instrumentation includes `HybridStoreMigrationTest` for real disk
database upgrades and `HybridStoreAuthorizationTest` for owner-bound operations,
monotonic remote status and post-sign-out write rejection. The installed-app
journey `HybridDeviceJourney` reports foreground device/model readiness and
performs the explicit Firebase cloud acceptance request.
Pass instrumentation argument `requireNano4=true` for the strict acceptance run;
that run requires a downloaded model, an SDK name identifying Nano 4 and an
actual `DEVICE` generation. A diagnostic run without that argument is not Nano 4
acceptance. The journey does not accept preview terms or download models.

For the separate cloud acceptance case, pass `requireCloud=true` and inject a
privately registered `firebaseAppCheckDebugSecret` instrumentation argument.
The debug-variant Firebase helper scopes the provider to the named cloud app.
Its component registrar must be present in the target debug application's
manifest, not only the instrumentation APK; the helper is excluded from release.
It requires an actual nonempty response attributed to `FIREBASE_CLOUD`; it does
not weaken App Check or treat local generation as a successful cloud request.
Keep a reusable development token in approved credential custody, outside source
and distributed APKs. Revoke it when exposed or no longer needed; one completed
test does not require revocation. The foreground
journey writes `hybrid-device.png` under the installed app's external
`files/acceptance` directory for visual review.

### Recorded device probe

On 2026-11-24, an OSS debug candidate ran on a physical Pixel 9 (`tokay`, Android
16/API 36) in Firebase Test Lab, matrix `matrix-2jvsp7jrqddjp`. All three installed
app tests passed: existing-note migration, durable receipt/reopen and the
foreground readiness diagnostic. The SDK reported `UNAVAILABLE`; no Nano
generation, download or cloud generation was observed in that run. This is not
the final candidate or a hackathon acceptance result.

- App APK SHA-256: `4c6088dc01e55281f979834436d8c858a3a2915adbf972ec8ef4aff97d31d234`.
- Test APK SHA-256: `6ee873807e95e271f864e6ddf92841b31d4cfbc82d71ae4dd1d178d559dafc7e`.

The Firebase project `kombify-speechkit-hybrid` has the OSS and managed Android
package IDs and their actual debug SHA-256 certificate registered, AI Logic
enabled and App Check enforced without a billing account. Production signing
and Play Integrity acceptance remain separate. A contest Quota Account was not used.
Device availability in Test Lab does not establish the installed AICore model
or preview readiness; the strict model check above remains required.

A second probe used a physical Pixel 10 (`frankel`, Android 16/API 36), matrix
`matrix-156jduotf2x1p`, app SHA-256
`1c9cf1b38eea37406dd30c71415339187ad2891e61a4d9876e05b6a371761b7d` and test SHA-256
`b6dee0572b040d52e9b0983e78b0164ec5c3241fb42b1852d31080e9d1c58466`.
The two disk-storage tests passed; strict Nano 4 acceptance failed with model
status `UNAVAILABLE`. No inference or download was observed. Its screenshot
exposed contrast, system-inset and button-wrapping defects, corrected in the
subsequent source. A supported, provisioned device is still required.

The corrected foreground screen passed a physical Pixel 10 diagnostic in
matrix `matrix-1j1hgj1l0p1kk`. Its captured screen verified readable foreground
colors, system-bar insets and wrapping action buttons. This visual check does
not establish every interaction or accessibility behavior.

The final physical Pixel 10 probe on 2026-11-24, matrix
`matrix-264ks81b1p9wi`, passed all four installed-app tests. Firebase AI Logic
returned a nonempty 83-character result with SDK-reported source
`FIREBASE_CLOUD` and model `gemini-3.5-flash-lite`, while App Check remained
enforced. The disk migration and durable-store checks passed. Local model
readiness still reported `UNAVAILABLE`; this diagnostic did not require or
establish Nano 4 inference, local fallback or the complete demo journey.

- App APK SHA-256: `421c8e026f6e28b384f54bdc461dd7812abdbc9ad0935ee44d7e6a246c564527`.
- Test APK SHA-256: `f09f318936bb31e73f258444e48dc4f1a4a5d543da7e1f656b6783760d091b51`.
- [Firebase Test Lab result](https://console.firebase.google.com/project/kombify-speechkit-hybrid/testlab/histories/bh.b78782aeafc514af/matrices/6537191198753675988).

These APK hashes identify the tested worktree build. A later publication or
integration commit is not automatically an identical built artifact. The
four test methods were subsequently split into the journey, migration and
authorization classes above without changing their assertions. The
private reusable debug token remains in encrypted Windows credential custody;
the central 1Password intake is pending. Temporary plaintext test flags were
removed. No production release or contest submission was performed.

Remote sessions retain their server-issued session ID before opening media.
Recovery lists the owner's server receipts even if a socket event was missed.
`unknown` and `dispatched` remain unsafe to replay. `completed` means the agent
returned an explicit terminal completion status; it does not independently
verify each downstream tool effect. Missing receipt support is an unavailable
capability and must not silently weaken the action contract.

## Current keyboard candidate

The 2026-11-26 candidate builds both app variants and the hybrid library. Its
setup and bounded-audio diagnostic passed on a virtual API 36 device. Compact
Opal and typing were observed, but expanded-panel acceptance failed. The latest
combined rerun did not execute because the test quota was exhausted. Current
Firebase SDK Live/audio acceptance, supported-device Nano 4 inference, real
account/endpoint acceptance, and the final offline flow remain open. The earlier
physical cloud result above belongs to its recorded candidate and is not a pass
for these new adapters. No final judge build or video is available yet.

## Three-minute video outline

Record only after the corresponding physical-device checks pass. Use English
narration or English subtitles and keep the phone screen visible.

| Time | Demonstration |
| --- | --- |
| 0:00–0:20 | A short dictation becomes a useful note while the user controls its destination. |
| 0:20–0:45 | Show the actual device, Nano model identity and local readiness. |
| 0:45–1:20 | Disable connectivity, dictate, summarize through Firebase on-device inference, and explicitly save a note. Show its stored ID. |
| 1:20–1:55 | Enable the permitted cloud route, show the observed execution source, and demonstrate a recoverable generation failure switching locally. |
| 1:55–2:25 | Run the selected remote agent, interrupt a turn and retrieve its server receipt without replay. |
| 2:25–3:00 | Show native keyboard handoff, imported audio and the source/build instructions. Identify existing SpeechKit components and the new hybrid work. |

The English demonstration video must reflect observed behavior. Runtime
acceptance and a working test artifact remain prerequisites for a readiness
claim; this guide does not certify them. Keep the judging build available free
through 2026-12-16 14:00 Pacific Time. A four-day quota account does not cover
the judging period.

Sources: [Firebase hybrid Android](https://firebase.google.com/docs/ai-logic/hybrid/android/get-started),
[ML Kit agents](https://developers.google.com/ml-kit/genai/agent/android),
[hackathon rules](https://firebasehybridaihackathon.devpost.com/rules).
