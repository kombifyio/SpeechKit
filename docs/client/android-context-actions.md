# Android screen and shared-content analysis

SpeechKit can analyze deliberately shared text, one image, or one audio file.
The keyboard's **Analyze screen** action requests a single screenshot through
Android's system consent dialog. Both paths show a preview before inference.
They do not read a messenger's private database or claim to identify its latest
incoming message.

## Use

- In another application, share a text, image, or audio attachment and choose
  **Analyze with SpeechKit**. The source application must offer the attachment
  and grant access to it.
- In the keyboard's shortcuts panel, choose **Analyze screen**, approve the
  system capture, and review the visible content. The capture ends after one
  frame. Protected surfaces remain excluded by Android.
- Edit shared text or adjust the image's left/right and top/bottom edges. Only
  the highlighted region reaches the model. Choose **Summarize**, or
  **Transcribe** for audio. Copy the result explicitly when ready.

An image of a voice-note bubble does not contain the spoken words. Share the
audio attachment to transcribe it. A screenshot covers only visible content;
scrolling to older messages does not turn them into the latest received message.

## Execution and data handling

Actions reuse SpeechKit's selected connection, execution policy, and existing
hold gesture. Cloud First and Local First retain their permitted fallback;
holding selects the alternative destination for that invocation. Exclusive
modes retain their destination. A keyboard gesture's chosen policy survives the
capture handoff; it does not change the saved preference.

Firebase AI Logic receives a text prompt and at most one Bitmap. Gemini BYOK
uses the selected credential and an inline image. A SpeechKit endpoint without
image support reports unavailable; it can use an eligible local fallback under
the existing policy. It never receives a text-only request that silently omits
the image. Audio uses the existing transcription adapters before summarization.
Local Only never grants cloud inference. Local model readiness is still required.

Android's Basic speech recognizer requires the audio permission even for a saved
file. If that adapter needs the permission, the preview requests it through the
system dialog and retries the same action after approval. This does not start a
microphone recording. A denied permission, a backgrounded preview, or a changed
connection does not authorize processing. Native file adapters and Advanced
recognition do not trigger this Basic-specific permission request.

Text is limited to 12,000 characters. Shared images are bounded to 16 MiB encoded
input and decoded to at most 1600 pixels per edge, with image orientation applied.
Audio retains the runtime's 25 MiB bound. Unreadable, mismatched, private-app, or
unsupported inputs are rejected. Incoming shared content cannot select credentials
or override execution policy.

Screenshots, decoded images, transcripts, and summaries are not written to the
operation store by this feature. In-process capture handoffs expire after one
minute and cannot be replayed. Preview requests expire after five minutes.
Leaving the analysis activity cancels processing; connection changes invalidate
the invocation. Only an explicit Copy action writes to the clipboard. No result
is inserted or sent into a conversation automatically.

## Build and implementation

The feature is in development and defaults off. Enable a named tester build with
`-PspeechkitHybridEnabled=true -PspeechkitContextActionsEnabled=true`.
`speechkitConnectionSetupEnabled=true` enables the existing connection setup.
The equivalent context environment variable is `SPEECHKIT_CONTEXT_ACTIONS_ENABLED`.
Disabled builds hide the keyboard action and disable the share/capture components.

The official Android release enables hybrid AI, connection setup, context actions,
and the Opal voice panel in both APK and AAB builds. Fresh installations retain a
local-only connection until the user explicitly selects a cloud connection.
Release builds use Play Integrity and public Firebase client configuration from
the release repository's `SPEECHKIT_FIREBASE_PROJECT_ID`,
`SPEECHKIT_FIREBASE_APP_ID`, `SPEECHKIT_FIREBASE_OSS_APP_ID`, and
`SPEECHKIT_FIREBASE_API_KEY` variables. The two app identifiers correspond to the
managed and OSS package names. The key must be restricted to Firebase services;
it must never permit the Gemini Developer API. Development App Check tokens,
private Gemini keys, service accounts, and server bearer tokens are not included.

For APK distribution outside Google Play, register the release certificate in
Firebase App Check and allow off-Play app versions while requiring device
integrity. A successful debug-provider test does not verify this release path.
Local model readiness remains device-dependent; releasing the feature does not
make unsupported local generation available.

Host acquisition and preview live under `android/app/.../app/context/`.
`HybridRuntime` remains the shared routing entry point; `:hybrid` and `:net` own
reusable Firebase and Gemini image generation. No accessibility or notification
listener permission is added. The screen action requests the current display on
Android 14+ to retain a reliable foreground preview handoff. If a manufacturer
overrides that selection, delivery waits for the capture activity to resume and
expires without retaining the image indefinitely.

Android SDK change: `HybridAi` generation methods and `GeminiTextClient.generate`
now accept an optional Bitmap. Kotlin text-only source calls remain valid, but
their JVM signatures change. Recompile AAR consumers together with this pre-1.0
update; do not replace only one binary in an already compiled application.

## Verification scope

`ContextShareJourney` drives installed share targets with synthetic text, a chat
image, and a silent WAV. It covers preview, text editing, cancellation, unreadable
and oversized inputs, and an unavailable local model without a fabricated result.
Capture journeys separately exercise the real system consent dialog. These
fixtures do not prove a particular WhatsApp or Telegram version exports its files.
Provider output and supported-device inference require their own acceptance.

`contextProcessingJourney` is opt-in through `requireContextProcessing=true` and
`contextProcessingDestination=local|selected`. It writes per-action receipts and
screenshots rather than treating unavailable local generation as successful
inference. The German speech fixture asks for a meeting in the library tomorrow
at three and asks the listener to bring the blue book. It was generated with the
local Microsoft Hedda voice and contains no personal recording. Successful
transcription must preserve the library and book facts.

For the explicit cloud test, the host exchanges the registered development
secret for a short-lived App Check token. Only that exchanged token enters
`files/context-test-input.json` in the isolated preview app. The journey deletes
the input before initialization and uses a non-logging, in-memory provider.
Do not use the Firebase debug-provider test helper here: it writes the durable
debug secret to device logcat. No credential belongs in runner arguments,
screenshots, reports, or source control.

Platform references: [Android sharing](https://developer.android.com/develop/ui/compose/sharing/receive),
[screen capture](https://developer.android.com/media/grow/media-projection), and
[Firebase hybrid input capabilities](https://firebase.google.com/docs/ai-logic/hybrid/android/get-started).
