# Changelog

All notable changes to SpeechKit should be documented in this file.

The format is based on Keep a Changelog and this project is intended
to ship under Apache-2.0. Entries are the public-facing summary that
lands on the GitHub Release page — write them for end users, not for
maintainers. The release linter
(`npm run release:lint -- --version vX.Y.Z`) refuses internal tracker
IDs, source paths, and other maintainer-only vocabulary.

## [Unreleased]

### Added

* **meetings:** notes read as formatted Markdown. The Meeting Review shows a
  write-up either as citable bullets or as Markdown, your own notes render
  formatted with a switch back to the source, and the compact notepad gets a
  Preview button while you type.
* **meetings:** "Copy Markdown" puts the complete write-up — title, date,
  language, executive brief and every section — on the clipboard, and
  "Save .md" writes the same file where you choose.

### Fixed

* **local model:** the bundled model server no longer spends its answer on a
  hidden thinking trace. With thinking allowed, summary answers were cut off
  before the JSON was complete, so the same summary step was retried with the
  same result and Meeting Reviews stayed empty. Meeting digests, Assist and
  Voice Agent answers now come back whole.
* **meetings:** a transcript piece longer than the local model can take is
  summarised in parts instead of being left out: the summary steps, the
  meeting write-up and the model-facing transcript all split such a piece at
  sentence boundaries, so a long uninterrupted stretch of a call still reaches
  your notes.
* **meetings:** a stretch of speech too long for the local model no longer
  stalls the whole meeting summary. Such a piece is marked as not summarised
  once and the rest of the meeting continues, instead of the same request being
  sent again thousands of times; the write-up keeps that passage as plain
  transcript. The model's "input too long" answer is now recognised as such
  rather than reported as a sign-in problem.
* **meetings:** meeting audio is transcribed at least every 90 seconds even
  when nobody pauses, so the notepad follows a continuous call and each
  summary step stays small enough for the local model. Previously a call
  without a 1.5-second pause produced one 13-minute transcript piece.
* **meetings:** summary batches are sized for the context window the bundled
  local model server actually runs with.
* **meetings:** summaries no longer fail for good when the local model server
  is not reachable for a moment. After a start or an update SpeechKit waits for
  the local model before it catches up on pending summaries, a refused
  connection counts as temporary, and a summary step that hit such a failure is
  retried on its own with a growing delay instead of waiting for the next
  restart.
* **meetings:** clicking the compact meeting pill no longer freezes mouse input
  for the rest of the desktop until a key is pressed. The pill moves from
  pointer movement instead of handing a quick click to the system's window-move
  mode; a click without movement stays a click.
* **meetings:** while the meeting pill is on screen the idle dictation pill
  hides instead of sitting next to it as a second overlay. A running dictation
  keeps its own overlay, and the idle pill returns when the meeting window
  closes.
* **server:** the release pipeline's post-deploy check reads the detailed
  readiness endpoint with the server credential, matching the hosted
  deployment that now keeps that endpoint authenticated; a failed check names
  its reason again instead of ending silently.

## [0.68.0](https://github.com/kombifyio/SpeechKit/compare/v0.67.0...v0.68.0) (2026-09-03)

### Highlights

- **Nothing fails quietly anymore**: dropped Voice Agent speech, overlay placement problems and meeting summaries that stall now show up in the activity log with a reason instead of leaving you guessing.
- **See what you captured**: after a meeting screenshot the compact pill briefly unfolds a preview of the saved image, so you know the right window was captured.
- **Meeting Reviews finish on their own**: interrupted write-ups resume after a restart, stuck summary steps are retried, and model answers are cleaned before they reach your notes.
- **Integrations show their maturity**: Settings marks each integration as Ready, Partial, Planned or Unavailable and greys out what this build cannot run.

### Added

* **voice-agent:** a framework-neutral turn adapter lets a registered voice
  agent receive each finalized text turn over the A2A protocol while SpeechKit
  keeps speech recognition and synthesis in its own pipeline. Hosted
  connectors can attach short-lived delegation headers per turn; self-hosted
  endpoints use the same API, and an external endpoint without TLS is refused.
* **server:** hosted deployments can route a voice session to a registered
  Companion agent. The session's target and lease come from the gateway, are
  verified independently, are kept only in memory, and are bound to the
  authenticated session; the first synthesized answer carries the AI
  disclosure. The Go SDK and the OpenAPI document expose the provider and
  target session options.
* **sdk:** `speechkit.OutputTarget` is the typed contract for the `Target`
  values carried from `RecordingStartOptions`, `dictation.RuntimeOptions`,
  `DictationStreamSinkOptions`, `TranscriptionJob` and `assist.ToolCall` to
  `TranscriptOutput.Deliver` / `TranscriptInterceptor.Intercept`. Hosts
  implement `TargetKind()` on their own target types or use
  `speechkit.TargetRef{Kind, ID}`; `speechkit.TargetKind(any)` classifies any
  value (`"opaque"` for legacy untyped targets). Well-known kinds are
  `window`, `editor` and `clipboard`; accepted kinds are documented in
  `docs/speechkit-framework-api.md`. The `Target any` fields keep accepting
  untyped values for one minor and become `OutputTarget` in v0.69.0.
* **desktop:** Settings → Integrations now shows how far each integration is
  implemented. Every card carries a maturity pill (Ready / Partial / Planned /
  Unavailable) and per-mode tags derived from the provider catalog matrix;
  modes without a runtime profile render as dashed "planned" tags, and
  integrations without any implemented mode (or unavailable in this build,
  e.g. managed Hugging Face) are greyed out with their toggle, setup and
  credential controls disabled. Cloudflare appears as a planned tile.
* **sdk:** STT hooks are now per instance instead of process-wide.
  `stt.Router.OnProviderSelected` replaces the global
  `stt.SetProviderSelectedObserver`, `stt.SecretResolver` (set via
  `allproviders.EnabledProviders.Secrets` / `google.Provider.SecretResolver`)
  replaces `stt.SetSecretResolver`, and `local.Provider.LowerSubprocessPriority`
  overrides `local.SetSubprocessPriorityLowered`. Two routers or providers in
  one process no longer share hidden state; the old STT globals were removed
  in the same release (see Removed).
* **sdk:** a Native Requirements table in `docs/architecture/sdk-surface-boundary.md`
  (linked from the README) lists the few packages that need cgo or an external
  binary and how each fails closed. `wakeword.NewPipeline` and
  `wakeword.ErrCgoRequired` now exist in `CGO_ENABLED=0` builds too, so a host
  compiles against the full wake-word surface everywhere and learns about the
  missing native engine only at runtime.
* **sdk:** shared sentinel errors for the voice-agent and assist layers. All
  `voiceagent/live` providers now wrap `live.ErrNotConnected`,
  `live.ErrSessionNotReady`, `live.ErrMissingAPIKey`, `live.ErrMissingEndpoint`
  and `live.ErrNoResumableSession`; `cascaded` exposes `ErrNotConfigured` and
  `ErrClosed`; `assist` exposes `ErrMissingExecutor`. Hosts can branch with
  `errors.Is` instead of matching provider-specific message text.
* **sdk:** runnable `Example*` functions for every entry-point package
  (`dictation`, `stt`, `assist`, `tts`, `client`; `hostconfig` already had
  them). Each has a `// Output:` block, so pkg.go.dev shows a verified first
  call and a drifted public signature fails `go test`.
* **sdk:** one vocabulary across the mode packages. `dictation.Service` /
  `dictation.NewService` join `assist`, `tts` and `voiceagent` (the existing
  `Runtime` / `NewRuntime` names stay valid), `stt.WordConfidence` is now an
  alias of `speechkit.WordConfidence` so provider results and transcripts share
  one word type, and `docs/architecture/sdk-surface-boundary.md` documents the
  naming and contract roles (`Service`/`Options`, provider SPI vs. host
  contract, `Router`/`Strategy`, `Err*` sentinels).
* **sdk:** hosts can ship their own providers without forking the catalog.
  `catalog.DefaultCatalog().With(profile...)` returns an immutable `Catalog`
  that validates every profile against its mode contract (`catalog.ErrInvalidProfile`),
  rejects duplicate ids (`catalog.ErrDuplicateProfileID`), derives the provider id from
  the `<mode>.<provider>.<model>` id shape, and exposes the same
  `ProfilesForMode` / `ProviderMatrix` / `ProviderDefaults` / `Filter` views
  the built-ins use. `docs/sdk/custom-provider.md` walks through SPI,
  conformance suite (`sttcontract`, `ttscontract`, `livecontract`), catalog
  profile and routing for a new STT backend.
* **sdk:** `pkg/speechkit/hostconfig` no longer imports any `internal/*`
  package. It now owns the loader core — `Defaults`, `Normalize` (legacy
  `[general]` hotkey backfills, mode-source and session-summary defaults,
  custom-URL auth-mode migration), `LoadConfig`, and the hotkey /
  close-behavior / auth-mode normalisers with their constants — and the
  desktop app's own configuration loader delegates to it, so a Companion host and the
  reference app read one `config.toml` identically. `hostconfig.Load` /
  `LoadConfig` return an error wrapping `hostconfig.ErrMalformedConfig` on
  broken TOML instead of silently falling back to defaults; a missing file
  still yields the defaults. Voice-agent `AgentProfileID` is carried through
  as configured (trimmed) rather than collapsed to `"default"` — which ids
  exist is the host's behaviour catalog, not the SDK's.
* **sdk:** a shorter way in. `docs/sdk/README.md` takes a Go developer from
  `go get` to a first transcript in one page and maps the next steps
  (providers, policy, custom provider, Assist / Voice Agent / TTS / Companion);
  `android/README.md` does the same for the Android modules (which module for
  which job, JitPack coordinates, build without the GPL fork); `CONTRIBUTING.md`
  spells out the three repository identities (working repository, public
  mirror, Go module path) and the rules that follow; and every
  `dictation.Options` field — including the previously opaque `Target` — is
  documented where pkg.go.dev shows it.
* **meetings:** after a screenshot is saved, the compact meeting pill unfolds
  a small preview of the captured image for a few seconds and folds it away
  again, so you can see at a glance whether the right window was captured.
  The picture is read from the meeting's local snapshot store; nothing leaves
  the device.
* **meetings:** capture ad-hoc screen snapshots during a live meeting by
  selecting the highlighted window, or press the configurable `Ctrl+Alt+S`
  shortcut to capture the monitor under the cursor immediately. Snapshots are
  stored locally with a meeting-timeline timestamp, appear as markers in the
  transcript timeline and the meeting detail gallery, and are referenced with
  their timestamps in generated Meeting Reviews. Everything stays on this
  device — no image ever leaves the machine.
* **meetings:** recordings now finish on their own when the call ends. The same
  microphone signal that detects a starting call notices when the calling
  application has left the microphone, and stops the recording after a short
  grace period. Hand-started recordings without a call are never auto-stopped.
* **meetings:** a notification appears when the Meeting Review is ready — or
  when it failed — so the result no longer sits unnoticed until the meeting
  library is opened by chance.

### Changed

* **desktop:** a locally built SpeechKit now reports the version the next
  release would carry (for example 0.67.18) instead of the line baseline
  0.67.0, so a local install compares correctly against published releases.
  Builds from a feature branch or with uncommitted changes append
  `-dev.<commit>` so they cannot be mistaken for a release, and the in-app
  update check offers such a build the matching published release.
* **localization:** the six message catalogs ship with a review record
  (`docs/localization/review-evidence.md`). Non-English catalogs are marked as
  translation proposals until a named reviewer signs them off, and a catalog
  cannot change without its record changing with it.
* **catalog:** The provider runtime registry now matches the catalog matrix:
  OpenAI advertises Voice Agent (realtime), Deepgram no longer advertises
  Assist (it has no LLM profile), and `tts.local.piper` is a first-class,
  non-experimental profile since the Piper runtime ships in the framework.
* **sdk:** BREAKING — the root package `speechkit` now holds contracts and
  value types only; implementations moved one level down. Importing the root
  no longer pulls in the provider catalog or the capture pipeline. Migration
  is a mechanical import rewrite:
  * `speechkit/pipeline` now owns the capture-to-transcript engine:
    `RecordingController`, `TranscriptionWorker` (+ `TranscriptionWorkerConfig`),
    `TranscriptionRunner`, `DictationSegmenter` / `NewDictationSegmenter`,
    `TranscriptSessionLedger`, the `LiveCommit*` policy helpers,
    `JoinTranscriptFragments`, `CountTerminalSentences`, `LiveInjectFragment`,
    `NeedsLiveInjectSpace`, `FallbackDictationSegments`, the `DefaultDictation*`
    tunables, `PooledPCMRecorder`, `ReadySegmentCollector` and the
    `ErrMissing*` / `ErrWorker*` sentinels.
  * `speechkit/catalog` now owns the shipped provider and model data:
    `Catalog`, `DefaultCatalog`, `NewCatalog`, `DefaultProviderProfiles`,
    `DefaultProviderMatrix`, `DefaultProviderDefaults`, `ProfilesForMode`,
    `ProviderKindsForMode`, the `Find*` lookups, the `Provider*` and `Model*`
    id constants, `DefaultModelRegistry`, `FindModelDescriptor`,
    `ModelFreshnessSLA` and the freshness reports, `ValidateDefaultCatalog`,
    `DefaultLocalBuiltInLLMModel`, `ErrInvalidProfile` and
    `ErrDuplicateProfileID`.
  * Unchanged in the root: every interface (`AudioRecorder`, `Transcriber`,
    `SegmentCollector`, `TranscriptOutput`, `JobSubmitter`, `Persistence`,
    observers), every value type (`Submission`, `Transcript`,
    `TranscriptionJob`, `ProviderProfile`, `ProviderKind`, `ModelLifecycle*`,
    `CaptureChannel*`), `Runtime`, the event bus and the policy helpers.
  * New: `TranscriptionJob.EffectiveSegments()` exposes the segments a worker
    should transcribe (previously unexported).
  Replace `speechkit.X` with `pipeline.X` or `catalog.X` per the lists above;
  the compiler flags every remaining call site. The change ships under the
  `breaking-api-approved` label; `dictation`, `assist`, `tts`, `voiceagent`,
  `companion` and `client` are unaffected.
* **meetings:** the compact meeting overlay now has an always-visible expand
  button next to its controls, so switching to the full notes window no longer
  depends on discovering the hover-revealed window chrome.
* **meetings:** the meeting note window closes when the meeting finishes,
  instead of staying open and looking like the recording is still running.

### Deprecated

* **api:** `POST /api/v1/meeting/snapshot` is deprecated and will be removed in
  v0.69.0. Every response now carries `Deprecation: true`, a `Sunset` date and
  a `Link` header with `rel="successor-version"`. Capture screenshots with
  `POST /api/v1/recording-sessions/{sessionId}/snapshots` (resolve the live
  meeting via `GET /api/v1/recording-sessions?kind=meeting&limit=1`). The
  desktop frontend already uses the session-scoped route.

### Removed

* **sdk (breaking, `breaking-api-approved`):** the process-wide STT hooks
  deprecated for the v0.65 window and the deviceagent v0 compatibility
  surface are gone (decision `dcda`: the `speechkit.device_agent.v1`
  protocol is final). Migration table (apidiff `v0.67.0..HEAD`,
  `pkg/speechkit/stt` and `pkg/speechkit/deviceagent`):

  | Removed symbol | Replacement |
  | --- | --- |
  | `stt.SetSecretResolver(fn)` | per-provider `SecretResolver` field (e.g. `google.Provider.SecretResolver`) or `allproviders.EnabledProviders.Secrets` |
  | `stt.ResolveSecret(name)` | `stt.SecretResolver(nil).Resolve(name)` / `stt.EnvSecretResolver(name)`; hosts keep their own resolver |
  | `stt.SetProviderSelectedObserver(fn)` | `stt.Router.OnProviderSelected` or `allproviders.RouterConfig.OnProviderSelected` |
  | `deviceagent.ProtocolVersion` (`…v0`) | `deviceagent.CurrentProtocolVersion` (`speechkit.device_agent.v1`) |
  | `deviceagent.Config.ServerToken` | `Config.PairingToken` |
  | `deviceagent.Config.HomeAssistantURL/Token/Agent` | none — Home Assistant authority is server-owned (`[assist.home_assistant]` in the server config) |
  | `deviceagent.Config.HTTPClient` | none — the agent owns its loopback-only, no-redirect transport |
  | `deviceagent.Capabilities.LocalPairing` | `RegistrationAck.Pairing` (server-attested) |
  | `deviceagent.Registration.Pairing`, `type deviceagent.Pairing` | `RegistrationAck` pairing and capability state |
  | `deviceagent.CycleResult.HomeAssistantRaw` | none — raw HA responses never cross the v1 boundary |
  | `deviceagent.ErrLegacyClientConfig`, `ErrMissingHomeAssistantURL`, `ErrMissingHomeAssistantToken` | no longer reachable; `New` validates v1 fields only |

  The matching desktop-side alias of the secret-resolver setter was removed
  with it. Nothing in this repository, the examples or the public export used
  the removed symbols; `Router` without `OnProviderSelected` now simply emits
  nothing.

### Fixed

* **meetings:** rolling summaries keep only the JSON the model was asked for.
  Answers wrapped in a code fence or padded with a sentence no longer pollute
  the rollups and the Meeting Review, and an empty answer is retried instead
  of being recorded as a finished summary. Summaries stored earlier are
  cleaned on the way out.
* **meetings:** a summary step interrupted by a restart no longer shows
  "Summarizing" forever. Leftover steps are retried on the next pass, and a
  rollup whose previous attempt never finished is redone.
* **meetings:** a Meeting Review that a restart interrupted resumes on its own
  once the local model is up, instead of waiting for a manual retry; the
  activity log names the meeting being resumed.
* **voice-agent:** speech spoken while the agent is still talking is no longer
  lost without a trace. When the microphone mute swallows more than half a
  second of your reply, the activity log says how much was dropped and why,
  and every session records whether it runs half-duplex (speakers) or with
  barge-in (headset).
* **voice-agent:** the activity log names the provider, transport and model
  that serve a session as soon as audio streams, not only when a fallback
  occurs.
* **voice-agent:** a Voice Agent running on AssemblyAI gets the AssemblyAI
  language-model gateway for its session summary even when AssemblyAI is not
  enabled as a speech provider. A session that ends without a summary now says
  why: summaries are turned off, or no finished dialog turn existed yet.
* **voice-agent:** the conversation window no longer shows a revised
  transcript twice when the speech recognizer rewrites an earlier word; it now
  applies the same merge rule as the compact overlay.
* **overlay:** a late answer or system notice arriving on an idle Assist or
  Voice Agent overlay no longer pins the overlay on "done"; it returns to idle
  after the usual delay.
* **overlay:** placement failures are no longer silent. When the active screen
  cannot be located, its work area is empty, or the resolved position lands on
  no monitor, a rate-limited warning appears in the activity log (and in the
  log file when logging is enabled) instead of nothing.
* **meetings:** a failed save of rolling summary progress is reported in the
  activity log once per meeting instead of being dropped, so a Meeting Review
  that lags behind the transcript has a visible cause.
* **server:** the customization endpoints reject an empty `scope_key` and a
  `scope_key` without `scope` with a clear error instead of silently ignoring
  them, as the API description always stated.
* **local-stt:** the bundled whisper-server no longer fail-fasts with
  `0xc0000409` on a fresh install when the model lives under a non-ASCII path
  (for example a Windows profile with an umlaut). SpeechKit now hands the child
  an ASCII-only `--model` argument and runs it inside the model directory. If
  local STT still cannot start, the activity log names the exit code with a
  readable cause and announces whether dictation is offline or falling back to
  a named cloud provider instead of switching silently.
* **voice-agent:** activation audit events no longer record provider `unknown`
  when the live session does not report a name; the identity falls back to the
  configured `[voice_agent].provider` backend, and server delegation is
  recorded with the `server` transport.
* **meeting:** the screenshot hotkey no longer fails silently. Capture errors
  (protected window, missing capture backend, store failures) now reach the
  activity log exactly like the notepad camera button, and pressing the hotkey
  outside a live meeting logs why nothing was captured.
* **meeting:** the screenshot window picker now gives clear hover feedback.
  The window under the cursor is framed with a DPI-scaled highlight border
  and its content is dimmed with a translucent wash, so it is unmistakable
  which window a click would capture. Previously the frame was a 3 px
  hairline that was easy to miss on scaled displays.
* **meeting:** the picked-window screenshot picker never appeared and every
  camera-button click ended in "Screenshot cancelled." — the overlay border
  windows were created with the module handle in `CreateWindowExW`'s `hMenu`
  slot, so Windows rejected them with `ERROR_INVALID_MENU_HANDLE`. Picker setup
  failures (window class, overlays, input hooks) are now reported as errors
  instead of cancellations, and the log records the path-free failure reason.
* **meetings:** meeting transcription now runs on its own pipeline, fully
  separated from dictation. What is said in a call no longer flashes the
  dictation overlay, no longer fills the dictation transcript panel or history,
  and can no longer become the "last transcription" that the copy/insert voice
  shortcuts reuse. Meeting text lands only in the meeting record.
* **meetings:** multi-language write-up progress now follows the language that
  is actually running instead of a later queued job stuck at 0%. The dashboard
  shows the real preparation, generation and persistence phases rather than a
  misleading percentage.
* **meetings:** Meeting Reviews in several languages are now written one after
  another instead of in parallel. Parallel runs against the bundled local model
  each ran at half speed and could all miss their deadline together, which made
  the automatic Meeting Review fail after longer meetings.
* **catalog:** `NormalizeProviderID` now canonicalises the provider segment of
  every `<mode>.<provider>.<model>` profile id, so `utility.builtin.*` maps to
  `local` and `utility.routed.*` / `tts.routed.*` map to `huggingface` like
  their `stt.`/`assist.` counterparts. Third-party segments still pass through.
* **catalog:** the model-registry freshness SLA no longer fails the default
  `go test ./...` run once the calendar moves past seven days. The age check
  is enforced by the new scheduled `model-freshness-gate` workflow via
  `SPEECHKIT_MODEL_FRESHNESS_GATE=1`; a missing `LastVerifiedAt` still fails
  everywhere. Registry rows re-verified against vendor docs on 2026-09-02; the
  Gemini Live Translate source URL now points at the model page (the old
  guide URL is gone).
* **build:** the whole module compiles again without a C toolchain: the
  openWakeWord helper program gained a fallback entry point that exits with a
  clear message instead of leaving the build without one.


## [0.67.0](https://github.com/kombifyio/SpeechKit/compare/v0.66.0...v0.67.0) (2026-09-01)

### Highlights

- **Meetings stay within reach while recording**: a compact transparent overlay shows capture health, elapsed time and controls, then expands into the full transcript and notes workspace.
- **Privacy now has boundaries you can select**: choose all networks, the local network only or this device only; incompatible providers and integrations remain visible but cannot be enabled.
- **Long meetings produce useful reviews**: bounded rolling summaries feed durable Meeting Reviews, five-sentence Executive Briefs and action items in English, German, Spanish, Chinese, Hindi and Arabic.
- **Cloud assistance stays optional**: GitHub Copilot can help generate Meeting Reviews only after sign-in and a separate transcript-processing grant, while the local path remains available without cloud credentials.

### Added

* **desktop:** Meeting Mode is available beside Dictation, Assist and Voice Agent
  in General Settings, with its own settings page for capture, review generation,
  languages, retention and compact-window behavior.
* **meetings:** active recordings continuously summarize bounded transcript
  batches without blocking capture. Finishing a meeting produces a durable
  Meeting Review, Executive Brief and action-item candidates, with retry,
  cancellation, progress and provider attribution.
* **meetings:** reviews support English, German, Spanish, Simplified Chinese,
  Hindi and Arabic. Follow-up tasks remain local and link back to their source
  meeting.
* **privacy:** the new Network Scope setting offers All networks, Local network
  only and This device only. The backend enforces the selected boundary for
  speech providers, model downloads, updates, telemetry, server connections and
  integrations, including redirect and DNS-rebinding checks.
* **privacy:** switching to a restrictive scope suspends incompatible settings
  without deleting them. Settings explains why affected choices are disabled
  and restores them when the scope permits them again.
* **integrations:** GitHub Copilot is available as an optional desktop generation
  provider with isolated sessions, no tools or repository context, and a
  separate revocable transcript-processing grant.
* **desktop:** signing in to kombify Cloud now works from the Connect button in
  Server settings. It opens the kombify sign-in page in your browser, shows the
  code to confirm there, and once you are signed in it points this device at
  the hosted service for you. Your password never enters the app, and the
  session is kept in the operating system's credential store.
* **desktop:** a signed-in device shows which account it is using and can sign
  out again, which also releases the hosted connection. A server you host
  yourself keeps its own credentials either way.

### Fixed

* **meetings:** pause, resume and finish commands now verify the requested
  recording session, so one meeting cannot change or persist another meeting's
  state.
* **meetings:** finishing a meeting no longer replaces an existing review with
  an empty result, and filtered meeting lists paginate consistently.
* **desktop:** the installed-version footer no longer shows a hard-coded,
  outdated changelog description beneath the actual version.
* **desktop:** the Connect button no longer reports a ready connection it never
  established. Previously it recorded the hosted address and claimed a
  credential was in place without ever obtaining one, so every request was
  refused while the settings card showed "ready".
* **android:** when the Companion app turns down the connection because the two
  installed apps do not match, SpeechKit now says so instead of showing a
  general failure. Connecting could not succeed in any released pairing before
  this, which looked like the button doing nothing.


## [0.66.0](https://github.com/kombifyio/SpeechKit/compare/v0.65.0...v0.66.0) (2026-08-28)

### Highlights

- **The Android modules you are told to use can now be resolved**: all five Apache-2.0 modules publish, and JitPack is the public channel because a token-gated registry was never one.
- **A fresh clone builds the framework without the keyboard fork**: the Android build no longer stops at settings evaluation when the GPL submodule is absent.
- **The voice contract is where both clients can check it**: the canonical file left a directory the public mirror never sees, so the drift gate now runs there instead of skipping.
- **A release no longer dies on an empty deploy response**: a null answer from the hosting API reports what came back instead of a type error.

### Added

* **android:** `core`, `net` and `domain` publish as Apache-2.0 AARs alongside `voice-ui-compose` and `coinstall-contract`. The public channel is JitPack, which resolves anonymously; GitHub Packages stays the internal lane because its Maven endpoint needs a token even for public artifacts.
* **sdk:** a gate compares the documented public package list against the real one and fails on drift in either direction. It found microphone capture undocumented since it was promoted.
* **server:** the four local device-agent routes are documented in the API description with the request and response shapes they actually carry.
* **clients:** publishing the TypeScript clients now verifies that an outside developer can install them anonymously, from a clean directory with no credentials.

### Fixed

* **android:** a checkout without the keyboard submodule configures and builds the framework modules instead of failing while Gradle evaluates settings.
* **release:** an empty deploy response from the hosting API no longer fails a release with a type error; the deploy is resolved from the service, and an unresolvable one reports both API answers.
* **server:** the API description says which scope values need a scope key, and rejects an empty one rather than accepting it and answering 400.

### Changed

* **docs:** the canonical voice-surface contract moved out of the desktop frontend into the exported server contracts directory, so the drift check runs in the public mirror instead of skipping there.


## [0.65.0](https://github.com/kombifyio/SpeechKit/compare/v0.64.0...v0.65.0) (2026-08-28)

### Highlights

- **A speech app carries the backend it uses, not all of them**: an application that talks only to Deepgram now compiles 49 external packages instead of 286, and never pulls the Google Cloud stack it does not use.
- **The names deprecated one release ago are gone**: everything landed exactly where its deprecation note said, so code already on the new import paths needs no change.
- **Reporting a failure no longer drags in a tracing vendor**: the framework's own attribute type replaces OpenTelemetry's on the public signature.

### Removed

* **sdk:** the provider names deprecated in v0.64.0 are gone, and the
  implementations now live in the provider packages. A Deepgram-only
  application compiles 49 external packages instead of 286, and never sees the
  Google Cloud or gRPC stack. The realtime side drops the same way: 35 instead
  of 236.

  Every name moved to the home its v0.64.0 deprecation note gave it, so code
  already using the new import paths needs no change. `Build`, `Register`,
  `BuildRouter`, `EnabledProviders` and the per-provider option structs live in
  `pkg/speechkit/stt/allproviders`; the realtime provider factory lives in
  `pkg/speechkit/voiceagent/live/allproviders`.

  The contracts did not move: `STTProvider`, `TranscribeOpts`, `Result`,
  `Router` and `AsTranscriber` are where they were, and so are the session
  runtime and the `LiveProvider` contract.

### Added

* **sdk:** the helpers a provider needs are part of the public surface now that
  providers live outside the root package: WAV framing and PCM extraction,
  the shared secret resolver, the speech capability baseline, speaker-count
  bounds, the WebSocket close predicate, and the microphone upsampler.
* **sdk:** recording a framework outcome no longer asks the caller to speak
  OpenTelemetry. `RecordOutcome` takes SpeechKit's own attribute type, so
  whether a tracing backend is installed stays SpeechKit's business instead of
  appearing in an embedder's signatures.


## [0.64.0](https://github.com/kombifyio/SpeechKit/compare/v0.62.0...v0.64.0) (2026-08-28)

### Highlights

- **Depend on the one speech backend you actually use**: every speech provider and every realtime voice provider now has its own package, so an app that talks to one of them stops compiling the rest.
- **One model catalog, not two**: the desktop app's private copy of the catalog is gone, and catalog entries now say what they are instead of leaving a host to infer it.
- **A full release to migrate**: the old names still work through v0.64 and disappear in v0.65, with a migration table for every one of them.

### Added

* **sdk:** catalog entries say what they are with a `modality` field, so a host
  can tell a speech model from a text or embedding model without inferring it
  from the mode. The desktop app's own catalog copy is gone; there is one
  catalog now.

### Deprecated

* **sdk:** every speech provider now has its own Go package, so an app that
  talks to one backend stops compiling the others' dependencies. The old names
  in `pkg/speechkit/stt` still work and are marked deprecated; they are removed
  in v0.65.0. A Deepgram-only app compiles 286 external packages today and
  drops the Google Cloud and gRPC stack once the old names go away.

  | Old name in `pkg/speechkit/stt` | New home |
  |---|---|
  | `GoogleSTTProvider`, `NewGoogleSTTProvider` | `pkg/speechkit/stt/google` — `Provider`, `New` |
  | `DeepgramProvider`, `DeepgramOptions`, `NewDeepgramProvider`, `Flux*` | `pkg/speechkit/stt/deepgram` — `Provider`, `Options`, `New`, `Flux*` |
  | `AssemblyAIProvider`, `AssemblyAIStreamingLLM`, `NewAssemblyAIProvider` | `pkg/speechkit/stt/assemblyai` — `Provider`, `StreamingLLM`, `New` |
  | `HuggingFaceProvider`, `NewHuggingFaceProvider` | `pkg/speechkit/stt/huggingface` — `Provider`, `New` |
  | `OpenRouterSTTProvider`, `NewOpenRouterSTTProvider` | `pkg/speechkit/stt/openrouter` — `Provider`, `New` |
  | `OpenAICompatibleProvider`, `NewOpenAICompatibleProvider`, `NewOpenAISTTProvider`, `NewGroqSTTProvider`, `NewOllamaSTTProvider` | `pkg/speechkit/stt/openaicompat` — `Provider`, `New`, `NewOpenAI`, `NewGroq`, `NewOllama` |
  | `VPSProvider`, `NewVPSProvider`, `NewVPSProviderWithModel` | `pkg/speechkit/stt/vps` — `Provider`, `New`, `NewWithModel` |
  | `LocalProvider`, `NewLocalProvider`, `InstallStatus`, `MinWhisperModelBytes`, `ValidateModelPath`, `FindWhisperBinary`, `SetSubprocessPriorityLowered` | `pkg/speechkit/stt/local` — same names, `New` for the constructor |
  | `EnabledProviders`, `RouterConfig`, `BuildRouter`, `BuildSpec`, `Build`, `Register`, `*Opts` | `pkg/speechkit/stt/allproviders` — same names |

  The provider contracts stay where they are: `STTProvider`, `TranscribeOpts`,
  `Result`, `Router`, and `AsTranscriber` are not moving.

* **sdk:** the realtime Voice Agent providers split the same way. The old
  names in `pkg/speechkit/voiceagent/live` are deprecated and removed in
  v0.65.0.

  | Old name in `pkg/speechkit/voiceagent/live` | New home |
  |---|---|
  | `GeminiLive`, `NewGeminiLive` | `pkg/speechkit/voiceagent/live/gemini` — `Provider`, `New` |
  | `OpenAILive`, `NewOpenAILive` | `pkg/speechkit/voiceagent/live/openai` — `Provider`, `New` |
  | `DeepgramLive`, `DeepgramAudioSettings`, `NewDeepgramLive` | `pkg/speechkit/voiceagent/live/deepgram` — `Provider`, `AudioSettings`, `New` |
  | `AssemblyAILive`, `NewAssemblyAILive` | `pkg/speechkit/voiceagent/live/assemblyai` — `Provider`, `New` |

  The session runtime, the `LiveProvider` contract, and the protocol types
  stay in the `live` package.

## [0.62.0](https://github.com/kombifyio/SpeechKit/compare/v0.61.0...v0.62.0) (2026-08-28)

### Highlights

- **Interrupt the assistant from any client**: tapping to stop a spoken answer now works in the web and Android clients, not only in the desktop app.
- **A refused voice session explains itself**: the error names the one thing to change and carries the id support needs to find the request.
- **Build on SpeechKit without writing glue**: any speech provider now plugs straight into the runtime, the microphone comes from the framework, and the embeddable settings loader takes a public config.
- **Words and Replacements are manageable**: Settings searches, pages and deletes terms, and several spoken forms can rewrite to one word.

### Added

* **sdk:** any speech provider plugs into the runtime directly instead of each app writing the same adapter.
* **sdk:** microphone capture ships as part of the framework, so an app embedding SpeechKit no longer implements recording itself.
* **sdk:** the settings loader takes a public configuration file and no longer requires the desktop app's own config types.
* **sdk:** the Voice Agent wire contract has a golden example file that the Go server, the web client, and the Android client all verify against, so a rename on one side fails the build instead of producing a session that connects and says nothing.
* **voiceagent:** tapping to interrupt a spoken answer works from the web and Android clients, not only from the desktop app.
* **voiceagent:** transcripts carry the speaker's label and name on web and Android when speaker recognition is on.
* **voiceagent:** the web and Android clients read which speech backend is serving the session, so a surface can show it before the first word.

### Changed

* **desktop:** Words and Replacements in Settings are searchable, paged, and deletable. Aliases live on the Word; several spoken forms may rewrite to the same term, and duplicate terms are merged on save.
* **desktop:** Customization saves Words and leftover Replacements in one call. Aliases rewrite in Dictation, Assist, and Voice Agent.
* **desktop:** Words and Replacements use one compact row per term, with spoken aliases on the same line.
* **desktop:** AssemblyAI LLM Gateway is a native SpeechKit LLM backend (same API key as STT). Qwen 3.5 4B Fast handles live turn cleanup; Qwen 3 32B handles summaries. Universal-3.5 Pro realtime can attach the gateway per turn. Settings has Connect kombify Cloud for the hosted origin.
* **desktop:** Dictation Settings switches Deepgram Nova-3 and AssemblyAI Universal 3.5 in one control. AssemblyAI keeps its native LLM on. kombify Cloud also keeps a small Cloudflare AI Gateway model ready so summaries and Assist do not fail without a configured LLM.

### Fixed

* **voiceagent:** a refused Voice Agent session says what to change to get in, and every error carries the id support needs to find the request.
* **desktop:** Full-capture Dictation uses the selected speech engine instead of always trying Deepgram first.
* **desktop:** Dictation Settings switches Live and Full capture. Overlay shortcut functions can show the same Live/Full control and the Deepgram/AssemblyAI engine, each on or off like Copy and Language.

## [0.61.0](https://github.com/kombifyio/SpeechKit/compare/v0.60.0...v0.61.0) (2026-08-26)

### Highlights

- **Connect without losing your working setup**: Android keeps the tester connection until you explicitly switch to Kombify Cloud, so voice features stay available during setup.
- **See each voice mode at a glance**: Voice Agent starts with Deepgram and each provider key has its own icon, making the active path clear before you speak.
- **Spoken numbers and fillers come out clean**: saying "1 von 5" or "1.7" writes as digits, and äh/ähm no longer land in the transcript.
- **Install and restart shows the real version**: a Windows update no longer leaves the previous version number on screen after it installs.

### Added

* **android:** kombify tester builds dial the hosted SpeechKit server without typing a key. Companion replaces that token only after Connect kombify Cloud or `speechkit://connect/kombify`.
* **android:** keep the tester origin until an explicit kombify Cloud connect ([850db89](https://github.com/kombifyio/SpeechKit/commit/850db895))
* **android:** publish the coinstall contract as a consumable library ([5c10dff](https://github.com/kombifyio/SpeechKit/commit/5c10dff7))
* **android:** name the Android app SpeechKit ([c3d77e2](https://github.com/kombifyio/SpeechKit/commit/c3d77e22))
* **voiceagent:** ship duplex turn-taking as a finished endpoint ([4dea30f](https://github.com/kombifyio/SpeechKit/commit/4dea30f3))

### Fixed

* **android:** store release builds no longer include a SpeechKit service token. Testers still get one from Firebase or the local tester install script.
* **android:** kombify Cloud without a signed-in Companion session stays on the device instead of using the tester server.
* **android:** default Voice Agent to Deepgram and give each key its own icons ([b4e8878](https://github.com/kombifyio/SpeechKit/commit/b4e88786))
* **desktop:** dictation writes spoken numbers as digits and strips German filler words, including when the language is set to all languages ([a0ccd81](https://github.com/kombifyio/SpeechKit/commit/a0ccd816))
* **desktop:** overlay language mark matches the size of the other action icons ([d1a1ef0](https://github.com/kombifyio/SpeechKit/commit/d1a1ef03))
* **windows:** after Install and restart, the app shows the published version instead of the previous one ([71d7b4e](https://github.com/kombifyio/SpeechKit/commit/71d7b4e1))
* **voiceagent:** a hung provider close can no longer leave a live session stuck ending ([e5b6200](https://github.com/kombifyio/SpeechKit/commit/e5b62002))
* **voiceagent:** reject the agent's own voice as barge-in, and leave barge-in off until acoustic echo cancellation is in the path ([b150bad](https://github.com/kombifyio/SpeechKit/commit/b150bad7), [9f2607c](https://github.com/kombifyio/SpeechKit/commit/9f2607c7))
* **audio:** play the voice-agent reply at 24 kHz, matching the downlink ([d02e35a](https://github.com/kombifyio/SpeechKit/commit/d02e35a0))
* **box-media:** truncated local audio writes fail instead of silently dropping the rest of a recording ([82e75c4](https://github.com/kombifyio/SpeechKit/commit/82e75c40))

## [0.60.0](https://github.com/kombifyio/SpeechKit/compare/v0.59.1...v0.60.0) (2026-08-25)

### Highlights

- **Find the server on your home network**: a homelab SpeechKit can announce itself so a Box, desktop, or phone finds it without typing an address.
- **Hosted servers stay silent**: discovery stays off until you turn it on, so a public instance does not advertise itself on someone else's network.
- **Nothing secret goes on the wire**: devices still sign in after they connect. The announcement only says where to knock.

### Added

* **android:** fetch Companion session in the background ([dbc0987](https://github.com/kombifyio/SpeechKit/commit/dbc09874d9969745b2931c418212cec39e2773c5))
* **android:** keyboard setup is the typing IME, not the system assistant ([b9efe17](https://github.com/kombifyio/SpeechKit/commit/b9efe17d5efe03e0bf6275828823c6ab1b044233))
* **android:** kombify testers use hosted SpeechKit without typing keys ([c37b699](https://github.com/kombifyio/SpeechKit/commit/c37b699e1fc49a390e67eeb3f07201f03c26ead6))
* **android:** LAN finder, drop HF onboarding, match shipped keyboard docs ([9116e6e](https://github.com/kombifyio/SpeechKit/commit/9116e6eadee8a0d13855c0846937cb02577ffa41))
* **android:** map Companion Gateway roots onto /v1/speechkit paths ([52c84c8](https://github.com/kombifyio/SpeechKit/commit/52c84c8ebd1fcc4efc0878610a63da4cf2f1073f))
* **android:** one keyboard, a more page, and voice-note transcription ([3430631](https://github.com/kombifyio/SpeechKit/commit/3430631fffd7ac3005fef3553d9b4c2fc102013a))
* **android:** summarize WhatsApp and Telegram voice notes ([c696c58](https://github.com/kombifyio/SpeechKit/commit/c696c58f0aba5e9113047738db0020c11fb18143))
* **cli:** find LAN SpeechKit servers with speechkitctl discover ([#299](https://github.com/kombifyio/SpeechKit/issues/299)) ([0742c50](https://github.com/kombifyio/SpeechKit/commit/0742c50e2aa7651ca42076171373d8097e171fd6))
* **desktop:** find a SpeechKit server on the LAN ([ef79291](https://github.com/kombifyio/SpeechKit/commit/ef79291bfcc0dd8c4ef5b10e21eecb097e07bfdc))
* **desktop:** show the live overlay language as a code mark ([a861f5e](https://github.com/kombifyio/SpeechKit/commit/a861f5e9980c88b689091c73172cdc2a1780de3a))
* **desktop:** toggle overlay shortcut functions in Settings ([4848a5e](https://github.com/kombifyio/SpeechKit/commit/4848a5ecbe871f9c6e617b42e8e50c7b55622100))
* **server:** LAN discovery via mDNS/DNS-SD (_speechkit._tcp, opt-in) ([c3903be](https://github.com/kombifyio/SpeechKit/commit/c3903be58fda83ddbcca43d20c0bb649b5c14c33))


### Fixed

* **android:** put SpeechKit on the Gboard suggestion strip ([34b3342](https://github.com/kombifyio/SpeechKit/commit/34b3342ca2874054ee14643b13a1a0cc68c88144))
* **android:** put the provider buttons back on the keyboard ([6ceede3](https://github.com/kombifyio/SpeechKit/commit/6ceede30b39193d823ce939a201720f844d3645a))
* **catalog:** export provider-option manifests and add model freshness fields ([d7c4db8](https://github.com/kombifyio/SpeechKit/commit/d7c4db8fd5d4e1e5a5ecdfb97875bf7172ac9b17))
* **ci:** use SERVER_TOKEN as production smoke authority ([faa846f](https://github.com/kombifyio/SpeechKit/commit/faa846f1b48ccf22e4817ec6029326fcb3e37665))
* **release:** unblock 0.60.0 version sync and reconcile current release ([#298](https://github.com/kombifyio/SpeechKit/issues/298)) ([a59409e](https://github.com/kombifyio/SpeechKit/commit/a59409e4d5b9ebeb5fccab9821daf8710d8fc2f9))
* **test:** harden desktop Voice-Agent waits and goleak filters ([1aa35bf](https://github.com/kombifyio/SpeechKit/commit/1aa35bf515a5da7f33f0c306e359ac454f517aca))
* **tts:** declare Hugging Face and Piper provider-option manifests ([1987d8a](https://github.com/kombifyio/SpeechKit/commit/1987d8a93cc6b71f97378a36ceef4048dab9fff0))

## [0.59.1](https://github.com/kombifyio/SpeechKit/compare/v0.59.0...v0.59.1) (2026-08-21)


### Fixed

* **windows:** typecheck overlay snapshots so the installer builds ([0c8de77](https://github.com/kombifyio/SpeechKit/commit/0c8de777c541fe230c86ff560d82dd864402dc2b))

## [0.59.0](https://github.com/kombifyio/SpeechKit/compare/v0.58.0...v0.59.0) (2026-08-21)

### Highlights

- **Meetings that start from what you wrote down**: your notes lead, the transcript fills in the rest, and the write-up cites the words it came from instead of inventing a summary.
- **The call is recorded as two voices, not a mix**: microphone and system audio stay on separate tracks, so speaker labels come for free and the audio is never kept.
- **A call starting is enough to offer notes**: SpeechKit notices when a calling app takes the microphone and waits for you to accept before it records anything.
- **The overlay is the control strip**: choose which actions sit on it, rotate the speech language, start a meeting, and the keyboard says when it needs a server instead of looking finished.



### Added

* **android:** let the user pick the glyph for each keyboard mode ([812d8aa](https://github.com/kombifyio/SpeechKit/commit/812d8aabfc373e6492290746ddbae6b5cecf4315))
* **android:** move the keyboard actions into the toolbar as icons ([fa205f9](https://github.com/kombifyio/SpeechKit/commit/fa205f9f9195335b24af6f8fecea4bcace7a2b35))
* **android:** put the server connection in Settings, where it belongs ([8ad3078](https://github.com/kombifyio/SpeechKit/commit/8ad30788be7428df85cc85ef04d6da2f2a6e8d9f))
* **meeting:** add a dual-capture hardware smoke ([5879872](https://github.com/kombifyio/SpeechKit/commit/58798721bfc9cbc674c20f7e87935d2e2e958313))
* **meeting:** add the meeting notepad window ([60c4b05](https://github.com/kombifyio/SpeechKit/commit/60c4b057e770b4291702fb2e06aea2c2faeaf6e1))
* **meeting:** give recording segments a capture channel and a timeline ([3bf306f](https://github.com/kombifyio/SpeechKit/commit/3bf306fe1c792f51c09ef84aa406a57edcc7301a))
* **meeting:** make meeting capture a first-class runtime ([dadc69d](https://github.com/kombifyio/SpeechKit/commit/dadc69dd453c31e5f045fcf191822d4f5b367899))
* **meeting:** never keep meeting audio, and let meetings expire ([19f63b8](https://github.com/kombifyio/SpeechKit/commit/19f63b83fecf0c94dfb81aa24af615e27ef9c64d))
* **meeting:** notice when a call starts and offer to take notes ([6cfb3a1](https://github.com/kombifyio/SpeechKit/commit/6cfb3a148cdcae1da298199a60044581af745e47))
* **meeting:** persist the notes a user writes during a meeting ([ff89d81](https://github.com/kombifyio/SpeechKit/commit/ff89d81b2faf200e4ccb7efb8eed5b47ceaed2f3))
* **meeting:** read a meeting as its write-up in the dashboard ([0fff660](https://github.com/kombifyio/SpeechKit/commit/0fff66062cc200c7a1bf3259485dc8e57babc874))
* **meeting:** write a meeting up as soon as it ends ([b27f1ef](https://github.com/kombifyio/SpeechKit/commit/b27f1eff3c95b7d381280af90b97d1655f48139e))
* **meeting:** write meetings up from the transcript and the user's notes ([ec4441c](https://github.com/kombifyio/SpeechKit/commit/ec4441c02ff516a5e83a6d198549a4a3cba4c6d8))
* overlay actions, language cycle, meeting start, and Android assist answers ([8b1db23](https://github.com/kombifyio/SpeechKit/commit/8b1db2362530611e0943590b3adc9ccc44a91785))


### Fixed

* **android:** make Settings reachable whether or not onboarding finished ([60b2949](https://github.com/kombifyio/SpeechKit/commit/60b29496ac37dbe977bd50e5113eb4b0d2641ec9))
* **deploy:** stop the blueprint advertising the origin as the public URL ([6b14b01](https://github.com/kombifyio/SpeechKit/commit/6b14b01c8522ab06865a82057046430fe452a8db))
* **meeting:** keep the call out of the notes, and finish what was stopped ([3cfe69b](https://github.com/kombifyio/SpeechKit/commit/3cfe69b3b1352e118ca09d08c611d0749b1a9d6b))
* **meeting:** let the local model finish writing a meeting up ([b57a6a4](https://github.com/kombifyio/SpeechKit/commit/b57a6a47d4442007308e55d8f0d2efabba867c80))
* **meeting:** stop one capture channel from discarding the other ([fd63653](https://github.com/kombifyio/SpeechKit/commit/fd63653c95cde8022649fccc198cd17324c8e428))
* **oss:** export the meeting capture sources so the public tree builds ([ce23108](https://github.com/kombifyio/SpeechKit/commit/ce23108275e343dbe61209e91956e3c4e3b5b8c7))
* overlay stop state, keyboard server hint, and Windows probe caches ([80e8342](https://github.com/kombifyio/SpeechKit/commit/80e834236188ed59264a76eb47a2c5268730fd7b))

## [0.58.0](https://github.com/kombifyio/SpeechKit/compare/v0.57.1...v0.58.0) (2026-08-19)

### Highlights

- **Every voice mode one tap from the keyboard**: a new row above the
  keys — dictate on your device or through your SpeechKit server, or talk
  to Deepgram, AssemblyAI or GPT. Actions needing a server say so rather
  than failing when pressed.
- **The microphone is there when you install it**: the voice key used to
  stay hidden until you enabled a second input method, and the dictation
  panel opened at the top of the screen instead of over the keys. Both
  are fixed.
- **One app, one icon**: installing SpeechKit no longer leaves two
  launcher icons behind, one of which opened the keyboard's settings
  rather than the app.

### Added

* **android:** make GPT the third direct Voice Agent button, not Gemini ([19c30d2](https://github.com/kombifyio/SpeechKit/commit/19c30d2b10777bcf46dc8375c5f0995623fcc71a))
* **android:** put dictation and the Voice Agent on the keyboard itself ([70afa60](https://github.com/kombifyio/SpeechKit/commit/70afa60dab77a42986f4879e8a5c252dd0d91439))
* **voiceagent:** list gpt-realtime-2.1 and its mini, without promoting them ([390ef31](https://github.com/kombifyio/SpeechKit/commit/390ef3102176f7c8effd5f18219b0c35010f3d87))


### Fixed

* **android:** stop the keyboard fork from adding a second launcher icon ([5f0a965](https://github.com/kombifyio/SpeechKit/commit/5f0a96534349ec3e1e55d234e7b06e8c86941b59))
* **release:** stop the version sync failing when only the highlights differ ([07830b1](https://github.com/kombifyio/SpeechKit/commit/07830b12815ab9061b3a2d3ebd79712e27255600))

## [0.57.1](https://github.com/kombifyio/SpeechKit/compare/v0.57.0...v0.57.1) (2026-08-19)

### Highlights

- **A keyboard that dictates, built in**: SpeechKit now ships its own
  Android keyboard instead of borrowing one. Tap the voice key and the
  dictation panel opens inside the keyboard, right where you were
  typing — no app switch, no lost cursor.
- **Configure a mode before you turn it on**: settings for Transcribe,
  Assist and Voice Agent stay open and editable while the mode is
  switched off, and turning a mode off no longer throws you out of the
  page you were working in.
- **The window says what it is**: the desktop shell carries the real
  SpeechKit mark and calls itself SpeechKit UI.
- **The Android source is public**: the complete corresponding source
  for the shipped keyboard is published alongside the release.

### Added

* **android:** answer the keyboard's voice key with the dictation panel ([4b3cc7c](https://github.com/kombifyio/SpeechKit/commit/4b3cc7cfe2fcb7acd15468af0ff5d8a345fd966c))
* **android:** dictate from the HeliBoard fork's voice key ([8702c23](https://github.com/kombifyio/SpeechKit/commit/8702c23b5037abab1a36b8b6740b2f48f2127fbc))
* **android:** fork HeliBoard and wire it in as a submodule ([ec8a6ff](https://github.com/kombifyio/SpeechKit/commit/ec8a6ffe99ca1744a1fa80c2e21f183b276f4cc4))
* **android:** include the HeliBoard fork in the Gradle build ([468d1ce](https://github.com/kombifyio/SpeechKit/commit/468d1cefd4c9c80edda24fb8bad9f357f4d6978d))
* **android:** ship the HeliBoard keyboard in the app APK ([f5acf0b](https://github.com/kombifyio/SpeechKit/commit/f5acf0b88ad1ce2baad291db19989c6d41e1a155))
* **oss:** publish the Android corresponding source ([984404b](https://github.com/kombifyio/SpeechKit/commit/984404b9aaf15dcbbcacc0e46d0c2e39c5dc606b))
* **ui:** brand the shell as SpeechKit UI and keep mode settings reachable ([e947869](https://github.com/kombifyio/SpeechKit/commit/e947869f79924106fde64e736843ece130186854))


### Fixed

* **android:** let the keyboard actually appear ([d0ded81](https://github.com/kombifyio/SpeechKit/commit/d0ded810c473627df6dc003ae13a8df25d150fae))
* **android:** mount the dictation panel inside the keyboard's input view ([3ca9c81](https://github.com/kombifyio/SpeechKit/commit/3ca9c818d8d7880394d0e68a003b74929f8700f4))
* **android:** recognise our own keyboard as the system spells it ([2637c2e](https://github.com/kombifyio/SpeechKit/commit/2637c2e7c471a954539773c8cb499013e3f426c7))
* **android:** stop advertising the Voice Agent as unreleased ([ab7bd7d](https://github.com/kombifyio/SpeechKit/commit/ab7bd7d66eaff41561a617797df7dbfcbe7d7869))
* **android:** stop the voice IME from squatting HeliBoard's method.xml ([9c2c40f](https://github.com/kombifyio/SpeechKit/commit/9c2c40f8faafa1fcb4f46f6b89b3872c2660f2cd))
* **ci:** check out the keyboard submodule, and scope the Android gates ([83060ab](https://github.com/kombifyio/SpeechKit/commit/83060ab50395d8bd8db23cdc0a8907c5f676e838))
* **ci:** check out the submodule wherever the OSS export runs ([23ac30c](https://github.com/kombifyio/SpeechKit/commit/23ac30c5cf316c98b1d34e6a76ea45e09700960e))
* **oss:** let the export carry the Android corresponding source ([e978fb2](https://github.com/kombifyio/SpeechKit/commit/e978fb21729adb1f1a50202e37223db30e1cbc81))
* **release:** fail loudly when NSIS never actually installs ([80a4fd4](https://github.com/kombifyio/SpeechKit/commit/80a4fd4ed631638e1da09af51f807fdba6b12d2e))
* **release:** stop the draft reaper from deleting the in-flight release ([a9e5f94](https://github.com/kombifyio/SpeechKit/commit/a9e5f943757a39be50fc77d231c7758f23daaf56))


### Changed

* **release:** cut the next release as 0.57.1 ([1c32fe8](https://github.com/kombifyio/SpeechKit/commit/1c32fe833ca6dbf3968f34437c4932969dd38905))

## [0.57.0](https://github.com/kombifyio/SpeechKit/compare/v0.56.0...v0.57.0) (2026-08-14)

### Highlights

- **Talk to the assistant on your phone**: the Android app now holds a real
  spoken conversation — press to talk, hear the answer, watch the transcript
  build as you speak — instead of promising one for later.
- **A conversation without leaving the text field**: the SpeechKit keyboard
  can open the assistant in place, so you can think something through and
  then go back to typing. Dictation still writes what you say into the
  field; the conversation stays out of it on purpose.
- **One assistant, one face**: the animation on Android is now the same one
  the Windows overlay and the web app draw, from a single shared source, so
  a change to the visual language reaches every screen at once.
- **The keyboard setup finds the right keyboard**: enabling and selecting
  SpeechKit as your keyboard is now detected correctly, so onboarding stops
  claiming a step is done before it is.

### Added

* **android:** add the realtime Voice Agent client the platform was missing ([ecd04c4](https://github.com/kombifyio/SpeechKit/commit/ecd04c49027b11a7579bf694920888cba162a7c2))
* **android:** fold Voice Agent sessions into one render state ([8115caa](https://github.com/kombifyio/SpeechKit/commit/8115caa25bf6f87bb264dc06c8d635a60eb31bc7))
* **android:** hold a conversation from inside the keyboard ([afcd2e0](https://github.com/kombifyio/SpeechKit/commit/afcd2e0fce12faef9227ce354cdbbc17a8aaf12a))
* **android:** let the assistant speak one visual language throughout ([bce2ed8](https://github.com/kombifyio/SpeechKit/commit/bce2ed8eb98f8855ef75f204eb0964f72535669b))
* **android:** make the Voice Agent reachable in the app ([359ee48](https://github.com/kombifyio/SpeechKit/commit/359ee48766010b20805927a2d0945b5671299b61))
* **android:** publish the shared orb so other apps can stop copying it ([60496d4](https://github.com/kombifyio/SpeechKit/commit/60496d4d191cda52db4ae586e10f0b1bf8289bb0))


### Fixed

* **android:** detect our own keyboard instead of a stranger's ([288723a](https://github.com/kombifyio/SpeechKit/commit/288723a54e7cce34da31efc2f6c7a8da4cf3cd00))
* **ci:** let dependency updates past the standards gate too ([f7000f1](https://github.com/kombifyio/SpeechKit/commit/f7000f199b9d482588b03591cb15986f1f9fc297))
* **delivery:** unblock security updates stuck behind an unavailable token ([6722ad3](https://github.com/kombifyio/SpeechKit/commit/6722ad355f8d877de1d931f35cafd4650fdef1da))
* **release:** actually merge the installer mirror instead of warning about it ([9db0160](https://github.com/kombifyio/SpeechKit/commit/9db0160f22af8b9b27f9036607552d04c29b6062))

## [0.56.0](https://github.com/kombifyio/SpeechKit/compare/v0.55.0...v0.56.0) (2026-08-13)

### Highlights

- **A voice overlay that gets out of the way**: the Voice Agent surface is
  now one dark, readable panel led by the animation and the conversation —
  no titlebar, no buttons competing for attention, and controls that appear
  only when you reach for them.
- **Let go and the conversation ends**: releasing hold-to-talk closes the
  dialog for good instead of leaving it open, while a short window still
  lets an immediate second press carry on where you left off.
- **Opening SpeechKit opens SpeechKit**: launching from the taskbar or Start
  menu brings the window up, and clicking the shortcut again returns to the
  running app instead of doing nothing.
- **Quick Note where you actually are**: the note action moved to the
  overlay, reachable from whatever you are working in, and every overlay
  icon now says what it does when you hover it.

### Added

* **desktop:** move Quick Note to the overlay and label every overlay icon ([df3c892](https://github.com/kombifyio/SpeechKit/commit/df3c8921f93f287dcf43e387a6fd123ced5df535))
* **desktop:** rebuild the Voice Agent overlay around the conversation ([2358a48](https://github.com/kombifyio/SpeechKit/commit/2358a48b24143f96a104d39b1595f20fbc485f21))


### Fixed

* **desktop:** end the conversation when hold-to-talk is released ([a2d6b90](https://github.com/kombifyio/SpeechKit/commit/a2d6b9042439fcc68071a650a4d891b0c2b14040))
* **desktop:** make the launch-time window open verify itself ([bb3fbcf](https://github.com/kombifyio/SpeechKit/commit/bb3fbcf6d6a0fa94f8b2304a65d75b52309765ec))
* **desktop:** stop the overlay resizing itself and follow the conversation ([f5a2021](https://github.com/kombifyio/SpeechKit/commit/f5a20214ea31d9abc578cfac55ef49f05167641f))
* **release:** read the curated highlights from main, not the release branch ([d6f5876](https://github.com/kombifyio/SpeechKit/commit/d6f58769601c58bc1272fe6193375af4daf6cfb2))

## [0.55.0](https://github.com/kombifyio/SpeechKit/compare/v0.54.10...v0.55.0) (2026-08-13)

### Highlights

- **One Voice Assistant look everywhere**: the same animated assistant —
  the Aura orb or the new Waveform style — now renders identically in the
  desktop app and on your server's web page.
- **Make it yours**: pick the animation, the centre logo, and whether the
  live transcript shows by default, straight from Settings with a live
  preview.
- **A modern voice overlay**: the desktop Voice Agent window trades its
  classic titlebar for a translucent glass card that keeps the focus on
  the conversation.
- **Talk to your server from any browser**: every SpeechKit server now
  serves a ready-to-use Voice Assistant page — open it, connect, and
  start talking.

### Added

* **desktop:** focus the glass overlay on the animation and the dialog ([e3642e3](https://github.com/kombifyio/SpeechKit/commit/e3642e357ef00bc579a43174ff3fac7cdd407ed5))
* **desktop:** hover-reveal the speaker row and drop the hero detail line ([ad93074](https://github.com/kombifyio/SpeechKit/commit/ad93074e1d0cb96022101249aa8b3fe917d43023))
* **desktop:** Voice Assistant appearance settings and glass overlay ([601d0be](https://github.com/kombifyio/SpeechKit/commit/601d0be405c09e80f26b14f1b9e7deffdc8b624b))
* **server:** /assistant — the hosted Voice Assistant web page ([ed0d402](https://github.com/kombifyio/SpeechKit/commit/ed0d402af2d54e06472fa17156f525ccfdf6c3a5))
* **voice-ui:** waveform variant, semantic marks, and OSS lab guard ([64ede5d](https://github.com/kombifyio/SpeechKit/commit/64ede5d3900267c29c7eff6b6784e464b280b44b))


### Fixed

* **delivery:** migrate repository auth contract ([#273](https://github.com/kombifyio/SpeechKit/issues/273)) ([865f99d](https://github.com/kombifyio/SpeechKit/commit/865f99dfa38f6fa01a3616e290ad5e93c88d5b42))
* **desktop:** open the app when you open the app ([5ae847d](https://github.com/kombifyio/SpeechKit/commit/5ae847d4c39222a8ca1d681f1aae42023a94add2))
* **desktop:** stop a hung Voice Agent from freezing the app ([287fd11](https://github.com/kombifyio/SpeechKit/commit/287fd119fab49efd7aeae9e25540f78afcf267f1))
* **release:** keep curated highlights out of Release Please's way ([611d6e5](https://github.com/kombifyio/SpeechKit/commit/611d6e5334cd3840c8d9c6c1b831678989ce1986))
* **release:** stop the version sync from timing out on a full clone ([be6fda6](https://github.com/kombifyio/SpeechKit/commit/be6fda6831112f8652222e25032bf2742171f647))
* **release:** verify existing npm package contents ([#272](https://github.com/kombifyio/SpeechKit/issues/272)) ([3edd753](https://github.com/kombifyio/SpeechKit/commit/3edd753f7c3f9a3f5fa1f35334c0ae24ea01151f))

## [0.54.10](https://github.com/kombifyio/SpeechKit/compare/v0.54.1...v0.54.10) (2026-08-13)

Speech provider and update reliability release. No existing public API was removed.

### Added

- Deepgram Flux v2 is now available across realtime Voice Agent,
  speech-to-text, and text-to-speech paths.

### Fixed

- Model setup now retries transient Whisper runtime downloads instead of
  failing the installation immediately.
- The desktop updater now shows a distinct verification phase instead of
  appearing to stall at 100%.

## [0.54.1](https://github.com/kombifyio/SpeechKit/compare/v0.54.0...v0.54.1) (2026-08-12)


### Fixed

* **server:** stop one transient probe failure from killing the server ([ba48092](https://github.com/kombifyio/SpeechKit/commit/ba48092ee06633782068af9d057b348f7763db72))
* **stt:** stop the local whisper ready flag from latching false forever ([754372d](https://github.com/kombifyio/SpeechKit/commit/754372dea4f206b20b7c5ac06953c9619290a05a))

## [0.54.0](https://github.com/kombifyio/SpeechKit/compare/v0.53.0...v0.54.0) (2026-08-12)

### Highlights

- **Nothing you say disappears quietly**: When a recording produces no text —
  usually because the spoken language did not match the configured one —
  SpeechKit now says so instead of dropping it without a trace.
- **Your words keep their own language**: Transcripts are no longer labelled
  German regardless of what was spoken, so custom vocabulary and replacements
  apply to the language you actually used.
- **Media on your paired Box**: A paired Kombify Box now plays media through
  the local device agent, so the audio path stays on your own network.

### Added

* **server:** wire local Box media lifecycle ([#170](https://github.com/kombifyio/SpeechKit/issues/170)) ([ecb20fd](https://github.com/kombifyio/SpeechKit/commit/ecb20fdd94498df067924f8b5ca3c06108b216df))


### Fixed

* **lint:** keep the empty-final commit inside the contextcheck exemption ([96af795](https://github.com/kombifyio/SpeechKit/commit/96af79585728b5de454d43ede9b652fe534e896e))
* **stt:** openrouter had the same invented "de" label, plus manifest truth ([5f6e0b6](https://github.com/kombifyio/SpeechKit/commit/5f6e0b6c65482085d1bfdcaea0109e290ce158d3))
* **stt:** stop losing speech to empty transcripts and invented locales ([e1b8374](https://github.com/kombifyio/SpeechKit/commit/e1b8374231b9683a7426c5e68b4db9e691d39660))
* **wyoming:** bound untrusted PCM metadata before building the WAV header ([e6d8826](https://github.com/kombifyio/SpeechKit/commit/e6d88265d34a6de038939a862ca89e844c5969d0))

## [0.53.0](https://github.com/kombifyio/SpeechKit/compare/v0.52.14...v0.53.0) (2026-08-11)


### Added

* **agentbridge:** Call GPT voice tools with explicit call semantics (M3) ([8146567](https://github.com/kombifyio/SpeechKit/commit/8146567a6fe263527fad306ab92d3b3cf83d434c))
* **agentbridge:** codex app-server JSON-RPC client — steer, interrupt, approvals (M2) ([41d633c](https://github.com/kombifyio/SpeechKit/commit/41d633ca49291f1bc98e8af917e78c8b02ec9648))
* **agentbridge:** Device-Target integration — tool dispatcher, idle-timer-safe narration, HTTP approval decision route (M4 device piece 1/2) ([5a84d0b](https://github.com/kombifyio/SpeechKit/commit/5a84d0bfb9b7d4c1e8818bd8124f7455e56089d6))
* **agentbridge:** external coding agent seam + Codex exec client (M1, default off) ([ae29572](https://github.com/kombifyio/SpeechKit/commit/ae29572c53a3fbd5625c7ca775eaae37190cf123))
* **android:** assistant orb with the kombify AI mark in the overlay ([32e22ec](https://github.com/kombifyio/SpeechKit/commit/32e22ec30b09bd5949bba04967c183bd10e8e0d2))
* **android:** bring the assistant orb to spec parity (7evz, part 1) ([507307d](https://github.com/kombifyio/SpeechKit/commit/507307d140cd1d31626f346d934045245c680cb7))
* **android:** drive the assistant orb from real microphone levels ([529f0b1](https://github.com/kombifyio/SpeechKit/commit/529f0b1d3e2cbcacfa22d3a0cfe338a8e607b098))
* **clients:** close the five voice-agent contract gaps from the UI-Lab dogfooding; Aura Orb chosen as default VA look ([677f13e](https://github.com/kombifyio/SpeechKit/commit/677f13e71ababeaba2578b60a3905ee99f5fae58))
* **device-target:** render the canonical kit orb in the prompter (t2ng) ([351c04c](https://github.com/kombifyio/SpeechKit/commit/351c04ccf4f985e23e7d871dfcf0fab66e7ce476))
* **google:** English primary plus the user's language as an alternative ([02a97ff](https://github.com/kombifyio/SpeechKit/commit/02a97ff5647348ed34a51b9fe07d2f64bd6685dd))
* **prompter:** overlay approval card for the External Coding Agent Bridge (M4 device piece 2/2) ([6ad4702](https://github.com/kombifyio/SpeechKit/commit/6ad4702097f18e9d673cefcaf95721389f82b701))
* **prompter:** ship the kombify AI mark in the Voice Agent orb (Device-Target) ([9a7ac8b](https://github.com/kombifyio/SpeechKit/commit/9a7ac8bdc4101ee584f91f45e0444be1b31a1f4f))
* **providers:** record each provider's native multilanguage expression, verified against vendor docs ([cb68345](https://github.com/kombifyio/SpeechKit/commit/cb68345de09826006c3578e8a2fda2b3f2a1c77c))
* **voice-ui-lab:** brand-mark switcher in the orb centre; fix flaky idle-hangup test and contextcheck lint ([336ada3](https://github.com/kombifyio/SpeechKit/commit/336ada36cc4b8555f4994b5fe0cc63dbfe7c99b3))
* **voice-ui-lab:** record the branding decision — AI-teal rosette standard, k monogram and no-logo alternatives ([001b851](https://github.com/kombifyio/SpeechKit/commit/001b85187dc18729883d84c26bea9a19364fd269))
* **voice-ui-lab:** Voice Assistant UI Lab — 3 mockup variants with fake + live voice drivers ([7d71835](https://github.com/kombifyio/SpeechKit/commit/7d71835ed67c91497e62f634ec3e135487ca8d4b))
* **voice-ui:** promote Aura Orb as speechkit-voice-assistant (b3rv) ([eff5f36](https://github.com/kombifyio/SpeechKit/commit/eff5f366a856f78e42b3f9cc17f1713c303f05b8))
* **voiceagent:** additive agent_progress host prompt (M4 kernel piece) ([9d96ab7](https://github.com/kombifyio/SpeechKit/commit/9d96ab7fa6b403601c3ef34a9eab267c2b73536d))


### Fixed

* **android:** revive the dead system assistant and make multilanguage the set value ([de4e314](https://github.com/kombifyio/SpeechKit/commit/de4e314660ff115a3e3d1e84fefb9cb74743f546))
* **ci:** clear the remaining main-branch lint and audit debt ([45b34e2](https://github.com/kombifyio/SpeechKit/commit/45b34e2d9bc5c718838a1f0ee1435e9a198533e2))
* **ci:** sync website asyncapi mirror, clear security-scan findings, absorb audit-bump bundle growth ([b0272bb](https://github.com/kombifyio/SpeechKit/commit/b0272bb19c2f01317b3641a5085cd9b36fb94e76))
* **clients:** pre-release audit fixes; state-coupled brand marks + immersive 3D rosette in the lab ([b9aa081](https://github.com/kombifyio/SpeechKit/commit/b9aa081187d52209e72fb33ae7f1e44ec224a362))
* **kernel:** stop pinning German by default, and stop forwarding the sentinel ([3ce7112](https://github.com/kombifyio/SpeechKit/commit/3ce711226f539cf48b051023920c552dd58700fa))
* **lint:** collapse the language-option helper onto its actual use ([504594c](https://github.com/kombifyio/SpeechKit/commit/504594cdf20b8ec211e2360e503b63ef2d6868c5))
* **release:** build voice-ui workspace deps in publish workflows; cut 0.53.0 changelog section ([789acce](https://github.com/kombifyio/SpeechKit/commit/789acceb104b162781d4491fcade13faf73ebe0a))
* **release:** dedupe same-version changelog sections preferring the hand-written entry ([58372e6](https://github.com/kombifyio/SpeechKit/commit/58372e629e5a7925627f87389d5a2b84f5b8ca9f))
* **security:** clear the remaining OSV findings (nanoid, postcss, undici) ([ad446c7](https://github.com/kombifyio/SpeechKit/commit/ad446c7779d7f74c0fdbd84e6bfb8144d1b493c9))
* **voice-ui-lab:** keep the k monogram smaller than the rosette ([7eb7368](https://github.com/kombifyio/SpeechKit/commit/7eb7368ebe3c886090a2c2aadc0496a6648c162a))
* **wakeword:** stop disabled-mode toast spam, make the debounce warning honest ([813711c](https://github.com/kombifyio/SpeechKit/commit/813711c938f338c34ce57b12aeeba212a757e425))

## [0.52.14](https://github.com/kombifyio/SpeechKit/compare/v0.52.12...v0.52.14) (2026-08-10)

### Fixed

* **release:** restore one monotonic SpeechKit version line above packages published by the retired direct-publish path

## [0.52.12](https://github.com/kombifyio/SpeechKit/compare/v0.52.11...v0.52.12) (2026-08-10)


### Fixed

* **release:** close Release Please after publication ([#225](https://github.com/kombifyio/SpeechKit/issues/225)) ([1335f12](https://github.com/kombifyio/SpeechKit/commit/1335f12db3c671c54ee927ca1430c1ca0927c52f))
* **update:** relaunch the app ourselves instead of trusting the installer ([8a56909](https://github.com/kombifyio/SpeechKit/commit/8a56909024b94679e2cbb481985ba0fb0e3eb6dd))

## [0.52.11](https://github.com/kombifyio/SpeechKit/compare/v0.52.10...v0.52.11) (2026-08-10)


### Fixed

* **release:** accept release-please changelog entries ([#222](https://github.com/kombifyio/SpeechKit/issues/222)) ([7f60d84](https://github.com/kombifyio/SpeechKit/commit/7f60d84857af8c7889800a44e384cc6d7a5eea80))
* **release:** enforce one normal SpeechKit release path ([#220](https://github.com/kombifyio/SpeechKit/issues/220)) ([0dfdaac](https://github.com/kombifyio/SpeechKit/commit/0dfdaacc417b86912e5b5725ca017e8884af0f4e))
* **update:** install updates silently instead of opening a doomed wizard ([c27f454](https://github.com/kombifyio/SpeechKit/commit/c27f454a6184188c5ded3a8facc37f1cbf731fcb))

## [0.53.0] - 2026-08-10

Voice Assistant UI groundwork release. A browser-based UI lab drives the
chosen assistant look across watch, keyboard, phone, overlay, and floating
panel — live against a SpeechKit server — and the public client packages
close the gaps that live voice sessions surfaced.

### Highlights

- **See your voice assistant before you ship it**: A UI lab renders the
  assistant in five device frames at once — smartwatch, keyboard bar,
  desktop overlay, phone, and floating panel — animated by real or scripted
  voice sessions.
- **Live voice in the browser**: A new adapter connects the voice UI kit to a
  SpeechKit server with one call — microphone in, agent audio out, transcripts
  streaming into the UI.
- **Barge-in that actually goes quiet**: Interrupting the assistant now drops
  its queued speech immediately instead of letting stale audio finish.
- **Pick your realtime voice per session**: Gemini, OpenAI, Deepgram, or
  AssemblyAI can be selected at session start.

### Added

- Voice Assistant UI Lab: an in-repo design lab renders three assistant
  mockup variants (Aura Orb — the chosen default look — Glass Waveform, and
  Ring) in compact and expanded sizes with a transcript toggle, driven either
  by scripted dialogues or by a live SpeechKit server session with a real
  microphone. A matching example server profile enables browser access for
  local testing.
- Voice UI Kit: official controller adapter `createVoiceAgentUiController()`
  (subpath `@kombifyio/speechkit-voice-ui/voiceagent-adapter`, optional peer
  dependency on the voice-agent client) — microphone capture, ticket
  WebSocket, and playback reduced into the canonical
  `speechkit.voice_surface.v1` stream the kit renders.
- Voice-agent client: per-session realtime backend selection in the start
  frame (also documented in the AsyncAPI spec), playback flushing for
  barge-in with an audio-level meter tap, a dedicated interruption hook, and
  a transcript normalizer for providers that stream deltas instead of
  cumulative text.

## [0.52.10] - 2026-08-10

Release-history repair. Public releases now require exact changelog-backed
notes, and stale draft objects left by earlier delivery races have been removed.

### Fixed

- Fast pre-1.0 publishing can no longer replace release notes with a delivery
  ID and source-SHA placeholder. The exact numeric version must exist in this
  changelog before a public release object is created.
- The missing public notes for the released v0.51.37 through v0.52.9 delivery
  points are restored from their exact source revisions.

## [0.52.9] - 2026-08-09

Update discovery repair. Numeric pre-1.0 builds are normal releases and every
user-facing latest-version surface resolves the same release.

### Fixed

- Numeric v0.x releases publish as normal GitHub releases and advance the
  canonical `latest` alias.
- The website, installer mirror, and application update resolver now agree on
  the newest published release instead of selecting an older prerelease state.

## [0.52.5] - 2026-08-02

Release-quality patch. Publication is race-safe and the dictation fixes from
v0.52.0 are covered by stronger end-to-end quality checks.

### Added

- Quality gates now cover the gaps that previously allowed dictation shortcut
  and language regressions to ship.

### Fixed

- Concurrent publisher runs for one numeric version converge on a single
  GitHub Release instead of leaving duplicate untagged drafts.
- Tagging and public mirror handoff no longer wait indefinitely for an
  unavailable source-repository App grant.

### Changed

- The latest Windows installer mirror was refreshed from the verified v0.52.0
  release assets.

## [0.52.0] - 2026-08-02

Dictation reliability release. Holding the dictation shortcut records one
unbroken take again, the speech language you configure is the one actually
used, and the app can find and offer its own updates.

### Highlights

- **Dictation stays in one piece**: Holding the shortcut records a single
  continuous take. Brief dropouts from chattering keys, KVM switches or
  accessibility filters no longer chop a dictation into quarter-second
  fragments.
- **Your speech language is respected**: A configured language now reaches the
  speech provider instead of being replaced by automatic multilingual
  detection, so mixed-language dictation follows your setting.
- **Updates arrive again**: The app finds the releases it actually publishes
  and offers them, with a clear manual path whenever a build is unsigned.
- **Voice controls for the web**: A framework-neutral component library ships
  the standard voice controls for any site or app.

### Added

- Voice UI Kit: a new framework-neutral web-component library
  (`@kombifyio/speechkit-voice-ui`) ships the standard voice controls —
  a split button (dictation plus voice conversation), a compact glass
  voice-conversation overlay with live transcript, an audio-state visualizer,
  and a fail-closed voice consent gate. The elements work in plain HTML and in
  any framework, follow light/dark themes through CSS custom properties,
  ship in six languages (with right-to-left support), and respect reduced
  motion. Without a voice-conversation entitlement the button visibly locks
  and explains what to do instead of disappearing.
- Automatic updates with a user-chosen manual path: the app checks for a new
  version on a schedule, can fetch it in the background, and always leaves the
  install to an explicit confirmation. It never restarts on its own.
- Update channel: a new `channel` setting under `[update]` chooses which
  releases are offered — `auto` (default) follows the app's own maturity,
  while `stable` and `prerelease` pin the choice. This decides what is
  offered, not what may be installed: an unsigned build still needs a matching
  published checksum before it can be installed at all.

### Fixed

- Dictation no longer breaks into fragments while the shortcut is held. A
  held shortcut is not the clean signal it looks like — chattering keys, KVM
  and remote-input stacks and accessibility filters all produce brief
  dropouts, and every one of them used to tear the recording down and start a
  new one. Momentary dropouts are now absorbed; letting go still ends the take
  immediately.
- Toggle-mode shortcuts no longer swallow a quick second press, so a
  deliberate double tap reliably stops the recording it started.
- A dictation could get stuck recording with no way to stop it short of
  restarting the app, when the two independent key-state sources disagreed.
- The configured speech language is applied instead of being silently replaced
  by multilingual detection. Picking a language in Settings had no effect on
  Deepgram transcription before; it does now, and multilingual code-switching
  remains the default when no language is set.
- Streaming and batch transcription agree on which language wins when a client
  asks for one and the server is configured for another: the request wins in
  both, as documented.
- Server-side assist no longer pins transcription to the server's reply
  language when the caller did not ask for one.
- Regional language variants such as `en-GB` and `de-DE` are passed through
  instead of being rejected, and underscore spellings like `de_DE` are
  corrected rather than failing the request.

### Changed

- The Deepgram language control is now offered in Settings as a real choice.
  Existing installs keep multilingual code-switching unless a language is set.

## [0.51.46] - 2026-08-02

### Fixed

- Published numeric pre-1.0 releases now reach the public download and update
  surfaces instead of remaining inaccessible after the build completed.

## [0.51.44] - 2026-08-02

### Fixed

- Hold-to-talk release debounce is concurrency-safe, preventing rapid key-state
  events from ending or restarting a capture incorrectly.
- The configured speech language is applied consistently across streaming,
  batch, and server transcription targets.

## [0.51.41] - 2026-08-01

### Fixed

- The numeric pre-1.0 delivery gate is pinned to the passing maturity-aware
  workflow revision.

## [0.51.40] - 2026-08-01

### Changed

- Optional pre-1.0 secret gates were removed from the public release path.
- Release quality uses the bounded R2 fixture and the maturity-aware delivery
  profile for exact-source validation.

## [0.51.37] - 2026-08-01

Automatic-update, Voice UI, and dictation reliability rollup.

### Added

- Automatic update checks and downloads retain an explicit user-confirmed
  installation path.
- The first Voice UI package delivers framework-neutral dictation and voice
  conversation controls.

### Fixed

- Hold-to-talk dictation no longer breaks into fragments and configured STT
  language reaches the selected provider.
- Dependency updates clear the high-severity advisories present on the prior
  release line.
- Numeric pre-1.0 delivery, public mirror authentication, server-image checks,
  and asynchronous multi-platform publishers use the shared Delivery v2 path.

## [0.51.2] - 2026-07-24

Patch release. Makes Windows dictation reliable when the PC is under heavy load.

### Fixed

- Windows: dictation no longer cuts out mid-sentence, and the status overlay no
  longer gets stuck, when the machine is busy. Under heavy CPU load the recorder
  could stop while you were still speaking, and the overlay could show
  "processing" before you had finished. SpeechKit now runs its capture and its
  helper processes at priorities that keep recording responsive even when other
  programs saturate the processor, judges speech pauses by the recorded audio
  itself instead of wall-clock time, and keeps the on-screen status in step with
  what is actually being captured. A new optional performance settings block
  lets you turn the priority tuning off if you prefer.

## [0.51.1] - 2026-07-22

Patch release. Fixes a Windows wake-word / startup regression present in the
v0.50.0 and v0.51.0 Windows installers.

### Fixed

- Windows: wake-word activation no longer fails on startup. The bundled speech
  runtime was one C-API version behind what the on-device wake-word engine now
  requires, so the wake-word helper aborted immediately and crash-looped — and
  on some machines that cascaded into the app window failing to open at all. The
  v0.50.0 and v0.51.0 Windows installers were affected; this build bundles a
  compatible runtime. Update to this build if wake-word stopped working or the
  app would not start after updating on Windows.

## [0.51.0] - 2026-07-20

Companion follow-up release. A paired Kombify Box can now complete a full voice
turn through the device agent, and the on-device wake-word models ship from a
reproducible, auditable training pipeline.

### Highlights

- **Talk to a paired Box**: A paired Kombify Box can take one complete voice
  turn and speak back a local result — the Touch-to-Talk path for standalone
  Companion hardware, each turn bound to the paired device and its Home
  Assistant claim.
- **Auditable wake-word models**: The on-device wake words ("Kombify",
  "Jarvis") are now produced by a fully reproducible, pinned training pipeline,
  so anyone can rebuild and verify the exact model that ships.

### Added

- Paired Box media turn: a paired Kombify Box submits one complete microphone
  turn as raw `audio/L16` (16 kHz mono) to `POST /v1/box-media/turn` with its
  pairing token and the SHA-256 of the exact audio, and receives one complete
  local text-to-speech result. The turn reuses the durable Home Assistant
  claim, exact target/state readback, and the claim-bound local TTS path; the
  verified input-audio fingerprint joins the claim so replaying one request ID
  with different audio fails closed. Touch-to-Talk can drive a turn today; an
  on-device wake path follows once a verified wake model lands.

### Changed

- Wake-word model training is now a single, pinned, auditable pipeline with its
  own tests, so the "Kombify" and "Jarvis" on-device models are reproducible
  from source instead of relying on ad-hoc training steps.

## [0.50.0] - 2026-07-19

Unified Voice release. Streaming dictation over one warm WebSocket, a second
first-class speech provider, account-level voice preferences, and sign-in
support for per-user clients.

### Highlights

- **Live voice typing**: Streaming dictation delivers draft words while you
  speak — the low-latency path keyboards and realtime text entry have been
  waiting for, with automatic fallback to classic transcription.
- **AssemblyAI joins Deepgram**: Both premium speech providers are now
  first-class for transcription, including live streaming partials, and short
  clips finish in a single round trip.
- **Your providers, everywhere**: Pick your preferred speech providers once
  and every hosted request follows them automatically, with graceful fallback
  when a provider is unavailable.
- **Sign in with your account**: One deployment now accepts both service
  tokens and identity-provider logins, unlocking per-user mobile and web
  clients.

### Added

- User voice preferences, end to end: a signed-in account's preferred
  speech providers (for example Deepgram first, AssemblyAI second) now
  travel with every hosted request and steer transcription automatically —
  no per-request configuration in any app. Preferences are edited once in
  the Workbench and apply across every surface; requests may still pin an
  exact provider profile explicitly (`provider_profile_id`, also on batch
  transcription now), and when a preferred provider is unavailable the
  server falls back gracefully and reports which provider actually ran.

- AssemblyAI Universal-3.5 Pro across all speech paths. Short dictation
  clips (up to two minutes) now ride AssemblyAI's synchronous endpoint and
  come back as a finished transcript in a single round trip — no more
  upload-and-poll for everyday utterances — with automatic fallback to the
  classic flow for long audio, diarization, and redaction requests.
  Transcription quality can now be steered with a short situational prompt
  ("homelab voice commands from a non-technical speaker"), explicit key
  terms, and the new `conversation_context` field carrying the preceding
  dialogue turns. AssemblyAI also becomes the second provider for live
  streaming dictation partials, with the situational hint applied from the
  first audio frame.

- New server auth mode `bearer_or_oidc`: one deployment accepts both the
  static service bearer token and IdP-issued JWTs (validated against the
  configured JWKS endpoint). This lets per-user clients such as mobile apps
  sign in through an identity provider while existing service callers keep
  their bearer token — no second deployment or proxy needed.

- Streaming Dictation over WebSocket: `POST /v1/dictation/stream/sessions`
  mints a single-use ticket, then the client upgrades to
  `GET /v1/dictation/stream/sessions/{id}/ws` and streams raw PCM up while
  live draft and final transcripts flow down — the low-latency voice-typing
  path for keyboards and other realtime text-entry clients. One warm socket
  carries any number of sequential segments (one per mic press): `start` →
  partial/final `transcript` frames → `finalize` → `segment_done`. Requires a
  streaming-capable speech provider (Deepgram today); deployments without one
  report `capabilities.streaming = false` at session creation so clients fall
  back to the existing batch endpoint. Configurable via
  `[server.dictation_stream]` (session caps, idle timeout, per-session audio
  budget); the wire contract is specified in
  `docs/server/asyncapi.dictation-stream.v1.yaml` with golden example frames
  in `docs/server/fixtures/dictation-stream.v1.json` that CI verifies against
  the server's actual frames.

### Changed

- Native WebSocket clients (mobile apps, CLIs) no longer need the
  empty-Origin development override to reach the ticketed Voice Agent and
  streaming Dictation sockets: a request without an `Origin` header that
  presents its session ticket subprotocol now proceeds straight to ticket
  verification. Browser requests remain subject to the configured origin
  allowlist, and ticketless requests without an `Origin` stay denied by
  default.

### Fixed

- Streaming dictation sessions that were created but never attached (for
  example when a client falls back to batch transcription or loses its
  network right after session creation) no longer occupy per-user session
  slots forever — they are reaped automatically once their ticket expires,
  so repeated fallbacks cannot lock an account out with a
  "per user limit exceeded" error.
- The streaming Dictation session response now honors the configured public
  server URL when building `ws_url`, matching the Voice Agent behavior
  behind reverse proxies and Docker port mappings.
- Short dictation clips without an explicit language keep automatic language
  detection instead of being transcribed as English: the synchronous
  AssemblyAI fast path is only used when the request pins a language.

## [0.49.0] - 2026-07-14

Companion Device release. This release turns SpeechKit's voice-companion
functions into a public, reusable part of the SDK and adds the pieces a
standalone smart-speaker companion and ESPHome / Home Assistant voice
satellites need.

### Highlights

- **Single-word "Kombify" wake word**: A new one-word wake model ("Kombify",
  no "Hey") is available for the desktop app and companion hosts, alongside the
  existing "Hey ..." phrases.
- **Reusable Voice-Companion skills**: The built-in skills (time, date, math,
  weather, timer, reminder, Wikipedia, temperature) are now a public SDK package
  any Go host can embed, and timers and reminders now fire on their own.
- **ESPHome / Home Assistant voice satellites**: A new opt-in Wyoming speech
  backend lets Home-Assistant-mediated ESPHome satellites use SpeechKit for
  speech-to-text and text-to-speech, and a public wake-word model endpoint
  serves models to devices.

### Added

- A public voice-companion skill catalog that hosts can embed directly,
  including a new temperature-conversion skill (Celsius / Fahrenheit / Kelvin)
  in German and English.
- A built-in timer and reminder scheduler so those skills ring on their own,
  with a callback hosts can use to play their own alert sound.
- A single-word "Kombify" wake-word model in the wake-word catalog and the
  desktop download manager.
- An opt-in Wyoming speech backend so ESPHome / Home Assistant voice satellites
  can use SpeechKit for speech-to-text and text-to-speech over the local
  network.
- A public wake-word model endpoint that serves openWakeWord models today and
  the ESPHome on-device wake-word manifest format when those models are
  published.

### Changed

- When a matched companion skill cannot answer a request, hosts that have a
  language model configured now fall through to that model instead of returning
  an empty result, matching the built-in assistant behavior.

## [0.48.1] - 2026-06-28

Patch release for the v0.48 Windows desktop line.

### Fixed

- Dictation now keeps transcript content out of the overlay and only inserts the
  final text into the active target.
- The Voice Agent conversation surface no longer owns a Windows taskbar button,
  so taskbar activation belongs to the dashboard window.
- A normal tray icon click now opens the dashboard, matching the existing tray
  menu and double-click behavior.
- The recording-session state migration is now idempotent on existing local
  databases, avoiding duplicate-column warnings during startup.

## [0.48.0] - 2026-06-28

Long dictation and meeting capture release candidate. This release moves
long-form transcription from one large final upload toward stable segments,
meeting-style system audio capture, and provider-safe live behavior.

### Highlights

- **Long Dictation That Keeps Up**: Longer speaking sessions now finalize in stable segments instead of waiting for one large upload.
- **Meeting Capture Foundation**: SpeechKit can now record system audio into editable meeting transcripts and prepare them for summaries.
- **Safer Live Transcription**: Provider streaming stays capability-gated so unsupported providers fall back to the stable segmented path.

### Added

- Long dictation now has an explicit segmented processing path that can commit
  finalized sections while the recording continues, with duplicate protection
  for repeated provider results.
- Windows system-audio capture can now be used for meeting-style sessions with
  ordered transcript segments, editable session details, JSON export, and
  summary generation.
- The dashboard now includes a Meetings surface for creating, inspecting,
  correcting, summarizing, finishing, exporting, and deleting captured
  sessions.
- Local release checks now cover long dictation fixtures, system-audio
  loopback capture, provider-backed long audio, and provider-backed meeting
  transcription from real playback.

### Changed

- Dictation processing can be selected as full final upload, segmented batch,
  provider-native streaming where supported, or automatic selection.
- Providers without a verified native live dictation adapter now use segmented
  batch processing instead of pretending to stream live.
- The compact overlay now shows live draft feedback and finalized dictation
  segments without pasting draft text into the target app.

### Fixed

- Long pauses during dictation no longer end the entire transcript path early
  when segmented processing is active.
- Repeated phrases are preserved across distinct segments while duplicate
  provider commits for the same segment are ignored.
- The Windows client now separates login startup from dashboard auto-open:
  enabling Windows startup writes the per-user Run entry immediately and
  refreshes it on every app launch, while dashboard auto-open remains a
  separate tray/window preference.

## [0.47.0] - 2026-06-24

Provider professionalization release for Go voice apps. This release makes
SpeechKit's provider abstraction more interchangeable across Gemini, Deepgram,
AssemblyAI, OpenAI, and cascaded fallback hosts while keeping the public API
additive and compatible.

### Highlights

- **Provider Switching For Voice Apps**: Go hosts can select providers,
  profiles, models, capabilities, and fallbacks through the shared live
  provider metadata instead of branching on provider-specific constructors.
- **Words And Replacements v2**: Custom vocabulary, snippets, templates, and
  commands now flow through deterministic runtime contracts across Dictation,
  Assist, and Voice Agent.
- **Credential-Backed Provider Proof**: Deepgram, AssemblyAI, Gemini, and
  OpenAI live gates now exercise the same Server-Target host code with clear
  `blocked_by_auth` reporting when credentials are absent.

### Added

- Provider capabilities now include tested provider-option matrix coverage,
  Deepgram/AssemblyAI/Google streaming evidence notes, and a reusable
  LiveProvider contract harness for realtime Voice Agent adapters.
- Words And Replacements v2 now resolves Words into native provider biasing
  for Deepgram, AssemblyAI, and Google, with prompt-hint fallbacks for
  providers that do not expose native keyterms.
- Customization settings now default to install scope, require keys for explicit
  user/org/workspace/session scopes, and include scope conflict visibility,
  provider-bias preview, and first-class Snippet, Template, and Command rules.
- Command rules now produce structured action metadata. Known intents
  (`copy_last`, `insert_last`, `summarize`) dispatch through existing quick
  actions on desktop, while unknown intents are exposed in transcript,
  API-response, and event payloads without being executed.

### Fixed

- Voice Agent microphone capture now stops and discards its buffered PCM on
  hold-to-talk release, activation teardown, and deactivation. Dictation and
  Assist also reject suspicious stale capture buffers instead of sending
  multi-hour phantom audio to STT.

## [0.46.1] - 2026-06-23

Public SDK and Windows client stabilization patch. No public API change.

### Fixed

- Voice Agent sessions now create a compact session summary automatically when
  a hold-to-talk dialog ends. SpeechKit prefers an active provider-native or
  local summary integration and only falls back to a clearly labeled short
  recap when no summary provider or model is active.
- Deepgram-backed summary setup is now recognized by the dashboard and settings
  surfaces, so users are not prompted to download a local model when an active
  cloud summary integration is already available.
- Mode toggles in Settings now round-trip through the local control API before
  persisting UI state. This prevents disabled Dictation, Assist, or Voice Agent
  hotkeys from staying visually enabled after the runtime rejects a change.
- Disabled mode shortcuts now show an actionable hint instead of failing
  silently, including for Voice Agent shortcut presses from the floating panel.
- Deepgram credential checks now cover the same transcription configuration used
  at runtime, improving readiness feedback for provider-backed Dictation and
  Assist profiles.
- The Voice Agent panel and tray icon now use the current transparent shell and
  product icon assets consistently on Windows.

## [0.46.0] - 2026-06-21

Public in-process provider embeddability release. This release opens the real
speech provider adapters to Go embedders and includes the latest Windows client
and Voice Agent stability fixes.

### Highlights

- **Real Providers In Your App**: Build Dictation, Assist, and Voice Agent experiences with SpeechKit's public Go packages and no SpeechKit server in the path.
- **Smoother Voice Agent Sessions**: Hold-to-talk conversations, session summaries, and brief pauses now feel more responsive and predictable.
- **Reliable Text Delivery**: Transcripts land more consistently in terminals, browsers, and desktop apps with smarter paste and typing options.

### Added

- External Go embedders can now use the real provider adapters from
  `pkg/speechkit/stt`, `pkg/speechkit/tts`,
  `pkg/speechkit/voiceagent/live`, and
  `pkg/speechkit/voiceagent/cascaded` without running a SpeechKit server.
- `pkg/speechkit/netsec` and `pkg/speechkit/audio` now expose the network
  safety and PCM audio helpers needed by custom provider hosts.
- Dictation output now understands where it is pasting. SpeechKit detects the
  app in focus and uses the right paste shortcut automatically: browsers and
  Windows Terminal get Ctrl+V, terminals like Termius and Hyper get
  Ctrl+Shift+V, and PuTTY-style terminals get Shift+Insert — so transcripts
  land in SSH sessions and terminal windows without manual workarounds.
- A new "Text Injection" section on the General settings page lets you choose
  between the automatic per-app behavior, plain clipboard paste, or simulated
  typing (which never touches your clipboard), and lets you turn clipboard
  restore on or off. Per-app overrides are available in `config.toml` under
  `[[output.app_overrides]]`.
- The Voice Agent panel has a refreshed live visual: a soft, translucent
  aurora that breathes and reacts to the live audio level, replacing the older
  solid orb.
- The hold-to-talk resume window is now configurable in Settings > Modes >
  Voice Agent (0–120 seconds, default 60). After releasing the hotkey the
  dialog stays resumable for that long; the session summary and the panel
  only wrap up once the window expires without another press.
- A new "Pause tolerance" setting (0–3000 ms, default 800) keeps the Voice
  Agent from answering during brief thinking pauses: short silences are
  filtered out of the mic stream, so the agent waits until you have actually
  finished your sentence.
- Session summaries now work out of the box: when no summary-capable model is
  installed, the dashboard shows a one-time banner that downloads the
  recommended local Gemma model with a single click — the same model that
  powers Assist. The hint can be dismissed permanently.

### Fixed

- Switching from Dictation or Assist into another voice mode while recording no
  longer submits the old audio through the newly selected mode. SpeechKit now
  cancels and discards the interrupted capture before starting the next one.
- Pressing the Voice Agent hotkey a second time now reliably stops the active
  realtime session, and session summaries finish in the background instead of
  delaying microphone and stream cleanup.
- Starting the Windows app now opens the dashboard on normal launches, while
  still using the first-run setup source for fresh local installs.
- Pasting transcripts into browser and website form fields is far more
  reliable. SpeechKit now waits until you have released the hotkey keys before
  sending the paste shortcut (holding Alt or the Windows key used to turn the
  paste into a different shortcut that browsers ignore), verifies the target
  window actually has focus before injecting, and retries clipboard access
  when another app briefly holds the clipboard.
- Your previous clipboard content is no longer overwritten when you copy
  something yourself in the moment right after a transcript was pasted.
- Assist selection capture works again when you are still holding the Assist
  hotkey keys at the moment the selection is read.
- Voice Agent replies no longer repeat the speaker label on every streamed
  sentence. A continuous answer now appears as one block that fills in line by
  line, and a new block starts only when the other speaker talks.
- The Voice Agent session summary is now an actual summary. When a text model
  is available it is summarized by that model; when none is configured the
  panel shows a short, clearly labeled recap of the assistant's points instead
  of echoing the whole transcript back.
- The collapsed Voice Agent panel now shows the latest line of the
  conversation instead of only a generic status, so you can follow along while
  it is folded.
- Speech is no longer lost when you keep talking while the agent already
  answers: as long as you hold the talk key, the microphone stays open and
  your continuation interrupts the answer, so multi-part questions arrive
  complete instead of only the last fragment.
- The big Voice Agent panel got a cleaner look: the live aura sits centered at
  the top above the status, and the panel itself is glassier and more
  translucent.

## [0.45.1] - 2026-06-10

### Added

- Assist answers can now take into account which application and window you
  are in when you trigger it, so a request like "summarize this" adapts to your
  email client, browser, or editor. A new setting lets you turn this off if you
  would rather not share window titles.
- Assist now applies your custom vocabulary (Words & Replacements) when
  generating answers, not only when transcribing — so your product names,
  jargon, and preferred spellings carry through into the response.
- Provider advanced settings now have Dictation, Assist, and Voice Agent tabs,
  so each mode has its own model choices and provider options in one focused
  view instead of one long list.

### Fixed

- Assist now reliably picks up the text you have selected: when nothing is
  selected it no longer mistakes the previous clipboard contents for your
  selection, and it waits a moment longer so slow apps still hand over the
  selected text in time.
- Provider advanced settings now label override and value switches, removing
  the ambiguous duplicate-looking toggles in option rows.
- SpeechKit Server now returns documented Bad Request errors for unsupported
  customization and vocabulary scope filters.

## [0.45.0] - 2026-06-10

Voice Agent professionalization release: OpenAI Realtime joins Gemini Live and
Deepgram as a realtime backend on both the desktop app and the server, holding
the Voice Agent shortcut now carries a full conversation, and the settings
experience gets a unified integrations layout.

### Highlights

- **OpenAI Realtime Everywhere**: pick OpenAI Realtime as the Voice Agent
  backend on the desktop app or switch any server session to it on the fly —
  alongside Gemini Live, Deepgram, and the local pipeline.
- **Hold-to-Talk Conversations**: keep the shortcut held for a full dialog —
  speak, hear the answer, and speak again — with a playback-aware echo guard
  and headset barge-in for interrupting the agent mid-answer.
- **Redesigned Voice Agent Window**: a clearer status area, an animated voice
  visualizer, and a scrolling conversation view.
- **One Settings Experience**: speech defaults live on the General page,
  Home Assistant and Piper become integration cards with dialog configuration,
  and every language picker is a curated dropdown.

### Added

- Voice Agent now supports OpenAI Realtime as a selectable backend on the
  desktop app: set the Voice Agent provider to `openai` and SpeechKit runs the
  realtime dialog against the configured OpenAI realtime model, alongside the
  existing Gemini Live and Deepgram options.
- Pressing the Voice Agent shortcut again right after releasing it now resumes
  the running conversation instead of starting a new session.
- You can now interrupt the Voice Agent mid-answer when using headphones:
  with a headset connected the microphone stays live while the agent speaks,
  so simply talking stops the answer and starts your turn. A new
  `barge_in` setting controls this (`auto` by default, `always` for setups
  with echo cancellation, `never` for the previous push-style behavior).

### Changed

- Holding the Voice Agent shortcut now carries a full conversation: speak,
  hear the answer, and speak again while keeping the key held. Releasing the
  key lets the agent finish its current answer — including the audio that is
  still playing — before the session closes, instead of cutting it off after a
  fixed timer.
- The Voice Agent now keeps the microphone muted until the agent's answer has
  actually finished playing, so the agent no longer hears itself through the
  speakers and multi-turn conversations stay stable.
- The Voice Agent window status now follows what you hear: it shows
  "Speaking" until the answer has finished playing, then returns to
  "Listening".
- The expanded Voice Agent window has a redesigned live-dialog surface with a
  clearer status area, an animated voice visualizer, and a scrolling
  conversation view.
- Speech defaults (language, audio format, endpointing, TTS speed, and the
  punctuation/formatting toggles) moved from the Integrations page to the
  General settings page. Providers still override individual options in their
  Advanced dialog on the Integrations page.
- Home Assistant and Piper now appear as integration cards in a new Extended
  Integrations group on the Integrations page. Their configuration opens in a
  dialog, matching the cloud provider cards, instead of inline forms on the
  page.
- Every language selection in Settings is now a dropdown with a curated
  language list instead of a free-text field. A previously saved custom
  language stays selectable.

### Fixed

- Selecting the OpenAI realtime backend for a Voice Agent session on the
  SpeechKit server now works regardless of the server's configured default
  provider. Previously the per-session backend switch only offered Deepgram,
  Gemini, and the local pipeline.
- The configured Voice Agent fallback model is now used when the primary
  realtime model is unavailable.
- Restarting the Voice Agent immediately after a session ended no longer
  risks a silent or muted session: the previous session's cleanup could stop
  the new session's audio output and microphone stream while it was still
  finishing the conversation summary.

## [0.44.0] - 2026-06-09

Customization release for Words and Replacements, provider options, and more
reliable segmented transcription. This release turns voice customization into a
framework concept instead of a mode-specific vocabulary feature.

### Highlights

- **Two Core Extension Primitives**: Words teach SpeechKit what terms to
  recognize, while Replacements shape final text with deterministic
  substitutions and synonyms.
- **Customization Packs**: Words, Replacements, Lexicons, and Rulesets can now
  move between local apps, server deployments, and embedded hosts through one
  portable pack format.
- **Provider Options**: Speech, text, and realtime providers now expose
  structured option contracts, making provider-specific controls visible and
  easier to validate.

### Added

- Added first-class Words and Replacements APIs for Device and Server targets,
  including Lexicon, Ruleset, and Customization Pack surfaces.
- Added a Settings Customization page for user-managed Words and
  substitution/synonym rules.
- Added scoped storage for customization records so hosts can author global,
  app, install, organization, user, or session-level behavior without changing
  runtime hooks.
- Added provider option metadata for STT, TTS, and realtime voice providers.

### Changed

- Dictation now applies resolved post-transcription substitutions and synonyms
  after segmented STT output has been merged.
- Assist transcript cleanup now consumes the same customization service before
  the existing one-shot utility and model path.
- Voice Agent sessions can receive active Words as recognition and context
  hints.
- The website and technical docs now position Words and Replacements as the
  public customization model. Dictionary-shaped surfaces are treated as
  migration projections, not as the framework extension concept.

### Fixed

- Multi-segment recordings are now submitted as one transcription job, processed
  in parallel, merged in order, and transformed once before output delivery.

## [0.43.1] - 2026-06-09

Server web experience refresh patch. No public API change.

### Highlights

- **First-Class Server Setup**: The self-hosted server setup flow now feels closer to the Windows client with a cleaner, more operational interface.
- **Clearer Runtime Checks**: The browser smoke dashboard now presents server readiness and mode checks with stronger status hierarchy and easier navigation.

### Changed

- The server setup, settings, smoke dashboard, and admin sign-in pages now share
  a polished SpeechKit visual system with clearer status chips, responsive
  layouts, and direct navigation between setup and smoke checks.

### Fixed

- Server web pages stay fully styled under strict browser-security headers
  without relying on external stylesheets or weakening the inline asset policy.

## [0.43.0] - 2026-06-09

Provider expansion release for cloud speech, speaker-aware transcription, and
real-time voice. No breaking public API change.

### Highlights

- **More Speech Providers**: Deepgram and AssemblyAI join the provider lineup, giving teams more choice for Dictation, Assist, and Voice Agent workflows.
- **Realtime Voice Options**: Deepgram Voice Agent support adds another low-latency conversation path for brainstorming and follow-up work.
- **Speaker-Aware Notes**: Provider speaker labels make multi-person transcripts easier to review, summarize, and reuse.

### Added

- **Deepgram is now a first-class provider** across all three modes: Nova-3
  speech-to-text with speaker labels, Aura-2 text-to-speech, and the Deepgram
  Voice Agent for real-time conversations. Add your Deepgram API key in Settings
  to turn it on.
- **AssemblyAI is now a first-class speech-to-text provider**, including speaker
  identification, selectable for Dictation and Assist.
- You can now choose which language model powers the Deepgram Voice Agent's
  replies, and point it at your own model deployment (bring-your-own-key)
  instead of the built-in default.

### Fixed

- Long replies spoken by the Deepgram Aura voice now always produce a single,
  valid audio file. Previously, text longer than about 1,900 characters
  requested in WAV format could produce a malformed file that some players
  refused to play.
- Self-hosted source releases now include the tracing support required by
  server builds, so downstream builds compile cleanly.
- The server smoke page now works with strict browser-security headers, so
  deployment readiness checks can run without weakening the policy.
- Self-hosted local-only Docker installs now allow the compose-internal Voice
  Agent WebSocket origin, so installer E2E checks exercise all three modes
  without weakening production defaults.

## [0.42.1] - 2026-06-07

Maintenance release that ships the v0.42 line as its first published build; no
functional changes since 0.42.0.

### Fixed

- Restored and hardened the release pipeline so the published artifacts and the
  GitHub Release stay in lockstep with the version shown on the website.

## [0.42.0] - 2026-06-05

Enterprise hardening and observability release. No breaking public API change.

### Highlights

- **Enterprise single sign-on**: Sign in with your company identity provider (Azure AD, Okta, Google Workspace), with each organization's data kept isolated.
- **Tamper-evident audit trail**: Every audit entry is hash-chained, so any later edit, reordering, or deletion is detectable — with a tool to verify a log.
- **Hardened by default**: Strict browser-security response headers, replay-resistant request authentication, and end-to-end request tracing out of the box.

### Security

- The server now sends strict security response headers on every request —
  Content-Security-Policy, X-Frame-Options, Referrer-Policy, and
  X-Content-Type-Options — with optional HSTS for TLS-terminated deployments.
  The admin sign-in page allow-lists only its own inline assets by hash, so no
  `unsafe-inline` is required.
- Audit-log entries are now tamper-evident: each record carries a hash that
  chains to the previous one, so any later edit, reordering, or deletion is
  detectable. Set an integrity key (`AUDIT_INTEGRITY_KEY`) for HMAC-backed
  evidence; the new `speechkit-audit-verify` tool checks a log and reports the
  exact record where the chain breaks.

### Added

- OIDC authentication mode (`auth_mode = "oidc"`): the server can validate
  Bearer JWTs from an external identity provider — Azure AD, Okta, Google
  Workspace, Auth0, and the like — against its JWKS endpoint, sourcing the
  caller's user, organization, and role from the verified token. Self-hosted
  deployments get real multi-tenancy without writing an edge-HMAC proxy.
- Optional pprof profiling endpoints for production debugging, off by default
  and enabled with `[server.debug] pprof_enabled`. When on, they stay bound to
  loopback and auth-gated unless the operator also sets `pprof_public`.
- Request correlation and tracing: every server request now carries an
  `X-Request-Id` (reused from the edge or generated, echoed on the response and
  included in logs), and transcription and speech-synthesis calls emit
  OpenTelemetry spans with provider, language/format, and latency so a
  configured collector can trace requests end to end.

### Changed

- Internal: speech-to-text provider construction is now centralized in a single
  registry, so every entry point builds providers from one mapping instead of
  duplicating the per-provider wiring. No user-facing behavior change.
- Internal: the voice-output profile-to-provider mapping is now shared between
  the framework kernel and the embeddable SDK from one source of truth, removing
  a duplicated copy. No user-facing behavior change.
- Internal: the device and server adapters now resolve text-to-speech provider
  credentials and defaults through one shared helper instead of two near-identical
  copies, so configuration precedence stays consistent across both. No user-facing
  behavior change in common configurations.

## [0.41.0] - 2026-06-04

Feature and hardening release for the voice framework. No breaking public API change.

### Highlights

- **Speaker labels in transcripts**: Dictation and Assist can now show who said what, with speaker segments and word-level labels.
- **Replay-resistant server auth**: Captured authentication headers can no longer be replayed against the server later.
- **Safer defaults, patched runtime**: A stray wildcard no longer opens voice sessions to other sites, and the runtime ships the latest security fixes.

### Added

- Speaker diarization for Dictation and Assist. Transcripts can now include
  per-speaker segments and word-level speaker labels when you send speaker
  options on the request; without them, transcription stays text-only.
  Available through Deepgram, AssemblyAI, and Google Speech-to-Text (batch
  audio), enabled with `DEEPGRAM_API_KEY`, `ASSEMBLYAI_API_KEY`, or the
  dedicated Google Speech-to-Text key. AssemblyAI can additionally attribute
  speakers to names or roles you supply. Speaker results are stored with the
  transcript and removed by the existing privacy deletion. Real-time streaming
  diarization and biometric voiceprint recognition are not part of this change.

### Security

- Edge-authenticated requests (`edge_hmac` mode) can now carry an optional
  `X-Edge-Auth-Ts` timestamp that is bound into the HMAC signature. When present,
  the server rejects requests whose timestamp is more than 5 minutes old,
  limiting how long a captured edge-signed header can be replayed. The header is
  optional and backward compatible — edges that do not send it keep working.
- Voice Agent WebSocket upgrades now ignore a wildcard `*` in the allowed-origins
  list by default. A stray wildcard in configuration no longer opens cross-origin
  WebSocket access — such upgrades stay denied and a warning is logged. Operators
  who genuinely need wildcard origins for local development can opt in with
  `SPEECHKIT_ALLOW_WILDCARD_ORIGIN=1`.
- Updated the bundled Go toolchain to 1.26.4, picking up upstream Go
  standard-library security fixes.

## [0.40.7] - 2026-05-28

### Added

- Add cross-mode Hands-Free target routing docs and scaffold options for Assist,
  Voice Agent, and Dictation-UI flows as part of the public source export.

## [0.40.6] - 2026-05-27

Stabilization rollup for the v0.40 framework line.

### Highlights

- **Windows release publishing now runs in smaller bounded phases.**
  Public-source export, Windows build, signing, packaging, and publish gates
  are separated so long-running release waits fail with clear diagnostics
  instead of hiding inside one oversized job.
- **Desktop mode settings now drive lifecycle state through the HTTP API.**
  Assist and Voice Agent enablement changes made through the local control
  plane now exercise the same lifecycle registry path as the desktop toggles.
- **Voice Agent session limits are now configured in the Voice Agent section.**
  Server deployments can set capacity under `voice_agent.limits` while older
  server limit keys continue to work.

### Added

- `GET /v1/voiceagent/sessions` now includes session usage metrics with total,
  active, pending, per-identity, and configured capacity values.

### Changed

- Release export determinism is checked by comparing two explicit export
  directories, allowing the publish workflow to reuse the first export for the
  public source mirror.

### Fixed

- Local mode start and stop commands now return a structured disabled response
  when the requested mode is off or has no hotkey binding.

## [0.40.5] - 2026-05-27

Release-gate and installer availability rollup for the v0.40 framework line.

### Highlights

- **Fresh Windows clones now include the current installer mirror.**
  The repository carries the latest Windows setup files plus checksum
  and unsigned-build notes so a cloned tree can be installed and tested
  on a second Windows device without hunting through release artifacts.
- **Public framework exports now prove the SDK boundary automatically.**
  Release checks discover every public SpeechKit framework package and
  build an external consumer module that imports Assist, companion, TTS,
  and wake-word APIs without relying on hidden source.
- **The v0.40 release record now matches the shipped tag history.**
  The documented API baseline starts with the v0.40.1 SDK-surface merge
  and rolls forward through the current patch line; no v0.40.0 tag is
  backfilled.

### Changed

- **Public source exports now keep the agent-native framework surface
  buildable.** The exported tree keeps the SDK, self-host server, CLI, MCP,
  docs, and examples while excluding desktop source, installer project files,
  release-only guards, E2E fixtures, and private website sources.
- **MCP architecture summaries now use public framework docs.** The embedded
  MCP documentation points agents at the SDK boundary and Framework API instead
  of relying on maintainer-only architecture notes.

### Fixed

- **Roadmap and website mirrors now use the same release sources as CI.**
  The roadmap keeps the required v0.40 schema bucket while explaining the
  factual patch-line baseline, and the docs website mirrors the canonical
  server API and agent markdown.

## [0.40.4] - 2026-05-26

Release-note, OSS export, and public CI rollup for the v0.40 line.

### Highlights

- **Runtime modularity is the v0.40 foundation.** Dictation-only use no
  longer eagerly starts Assist, Voice Agent, Genkit, TTS, or LiveKit-specific
  UI pieces, so embedders can keep the smallest voice path active without
  paying for the full assistant stack.
- **Embeddable SDK packages are now the primary integration surface.** Go
  hosts can compose wake-word detection, hands-free companion sessions, spoken
  output, Assist routing, and runtime events through the public
  `pkg/speechkit/...` APIs.
- **Voice Agent WebSocket tickets are subprotocol-first.** New clients receive
  `ws_url` plus `ws_subprotocol` so one-time tickets do not ride in browser
  URLs, while legacy query URLs remain compatible.
- **GitHub Release notes and website highlights now share one changelog
  source.** The v0.40 public rollup includes explicit `Highlights`, allowing
  automated releases and the website "What is new" cards to present the same
  main features instead of the latest small patch note.

### Fixed

- **Public CI no longer expects private deployment actions.** The public export
  skips deployment-secret release guards when private deployment files are not
  present, while the private source repository keeps those checks active.
- **The v0.40 OSS export includes the files required by public configuration
  tests and release smokes.** Enterprise presets, subprotocol-first Voice Agent
  smoke checks, and the public release notes now move through the same release
  path.

## [0.40.3] - 2026-05-26

OSS export correction for the v0.40 line.

### Fixed

- **Enterprise preset files are included in the OSS export.** The
  public repository now receives `deploy/presets/`, matching the
  documented enterprise deployment guide and the public config tests.

## [0.40.2] - 2026-05-26

Release-smoke compatibility patch for the v0.40 line.

### Fixed

- **Production smoke checks now use subprotocol-first Voice Agent
  tickets.** The `sk-e2e` release smoke client now dials Voice Agent
  sessions with `ws_url` plus `ws_subprotocol`, matching the v0.40.1
  WebSocket contract while keeping legacy ticket-query URLs compatible.

## [0.40.1] - 2026-05-26

SDK-surface modularity patch on top of the v0.40 runtime-modularity work.

### Added

- **Embeddable voice building blocks are now public SDK packages.**
  Go hosts can compose wake-word detection, hands-free companion
  sessions, spoken output, Assist routing, and runtime event
  subscriptions through `pkg/speechkit/wakeword`,
  `pkg/speechkit/wakeword/sherpa`, `pkg/speechkit/companion`,
  `pkg/speechkit/tts`, and the expanded `pkg/speechkit/assist`
  surface.
- **Hands-free companion composition is now executable, not just a
  facade.** `companion.NewHandsFree` publishes wake events, delegates
  host wake sinks, converts wake detections through a host transcript
  request factory, calls Assist, optionally synthesizes spoken output,
  and publishes Assist/TTS lifecycle events.

### Changed

- **Voice Agent session responses are subprotocol-first.** New clients
  use `ws_url` plus `ws_subprotocol` so one-time WebSocket tickets do
  not ride in URLs. The deprecated query-ticket compatibility response
  field has been removed in the breaking hardening branch.
- **TTS routing uses provider kinds instead of provider names.**
  Local-only and cloud-only routing now works for Piper, Kokoro-local,
  self-hosted local providers, Hugging Face, OpenAI, and Google without
  hard-coded string matching.

## v0.40.0 Planning Baseline (not a released tag)

Runtime-modularity was prepared under the v0.40.0 milestone bucket, then
shipped through the v0.40.1 and later patch line. There is no standalone
v0.40.0 release tag.

### Added

- **Runtime modularity prepared the v0.40 framework line.**
  Dictation-only use no longer eagerly starts Assist, Voice Agent,
  Genkit, TTS, or LiveKit-specific UI pieces. Mode transitions now
  report clear status and latency, and disabled modes return a
  consistent unavailable response instead of partially starting.

### Changed

- **Voice Agent UI assets are isolated from the Dictation shell.**
  The bundle budget now protects the lean Dictation path while still
  allowing Voice Agent visuals to load when needed.

## [0.38.14] - 2026-05-25

Windows local onboarding release hygiene hotfix. No public API change.

### Fixed

- **Local setup always starts a Dictation-ready speech-model download.**
  Choosing Local immediately starts the selected model download in the
  background, defaulting to the smallest local Dictation model when no
  larger model is selected.
- **Windows installers follow the on-demand model design.** Fresh
  installs no longer require `ggml-small.bin` to be bundled; setup can
  finish while the client downloads the chosen local model after or
  during onboarding.
- **Release gates now verify the full clean-install path.** The
  Windows NSIS and MSI install checks assert that speech weights are not
  shipped in the installer, download local models on the cold path, and
  run Dictation, Assist, and Voice Agent from the installed build.
- **Public release history now only lists published versions.** Failed
  draft attempts were removed from the public release surface so the
  changelog and latest release page point at the same shipped line.

## [0.38.13] - 2026-05-25

Windows local onboarding and install-gate hotfix. No public API
change.

### Fixed

- **Local setup now starts a speech-model download immediately.**
  Choosing Local starts the smallest Dictation-ready model by default,
  and choosing a larger model starts that selected download in the
  background during onboarding.
- **Windows install verification now matches the on-demand model
  design.** The installer gate no longer requires `ggml-small.bin` to
  be bundled and instead verifies that Dictation speech models remain
  client-side downloads after onboarding.
- **Release smoke now verifies Voice Agent with the correct origin.**
  Local Native probes stay originless; production smoke sends the
  configured public browser origin so WebSocket hardening is tested
  through the deployed path.

## [0.38.9] - 2026-05-25

### Fixed

- **E2E smoke client sends an Origin header on WebSocket dials.** The
  sk-e2e Go client did not set an Origin header, which the v0.38
  cross-origin hardening gate rejects by default. The client now
  derives the Origin from its `--server` URL.

## [0.38.8] - 2026-05-25

### Fixed

- **Strict readiness no longer fails when an optional feature is
  disabled.** A deliberately disabled component (e.g. wake-word
  training uploads) previously reported "unavailable" and caused
  `/readyz/strict` to return 503. New `disabled` health status has the
  same readiness rank as `ok`, so operators get an accurate signal.

## [0.38.7] - 2026-05-25

Consolidated security hardening and installer release. Rolls up
v0.38.0 through v0.38.6 plus two post-v0.38.6 fixes into a single
OSS release.

### Highlights

- **Security defaults now fail closed.** Public binds without
  authentication are refused, browser settings writes require CSRF
  protection, risky CORS combinations are rejected, and Voice Agent
  session tickets no longer need to ride in query strings.
- **Voice-Companion is now local-first.** Multi-turn skills, Piper TTS,
  and Home Assistant/Piper settings let "Hey Quby" handle short
  follow-ups and spoken answers without requiring a cloud TTS key.
- **Windows releases are smaller and more reproducible.** Speech models
  download on demand instead of inflating the installer, builds pin
  their inputs, and the public export path now verifies supply-chain
  determinism before publishing.

### Security

- **Server refuses to bind to a public address with no authentication.**
  `auth_mode = "none"` on a non-loopback bind address now fails at
  startup instead of silently running an open server.
- **Voice Agent WebSocket prefers a session ticket in the
  `Sec-WebSocket-Protocol` subprotocol** over the legacy `?ticket=`
  query string. Cross-origin browsers without an `Origin` header are
  rejected by default. Per-frame read limit tightened to 64 KiB.
- **Admin settings PATCH requires a CSRF token** (double-submit cookie
  pattern). Bearer and edge-auth callers bypass CSRF.
- **Rate limiter is cost-weighted with a per-plan daily cap.** Expensive
  endpoints draw more tokens; demo-tier callers have a hard daily
  ceiling; service-bearer callers are keyed by remote IP.
- **Wake-word upload validates activation IDs and serialises quota
  checks.** Oversized or path-traversal-shaped IDs are rejected; two
  concurrent uploads at the quota edge cannot both pass.
- **Wildcard CORS is rejected when the admin login UI is enabled.**

### Added

- **Multi-turn skill conversations** (from v0.38.0). Skills that need
  follow-up keep the conversation open for 60 seconds. Desktop build
  now also supports follow-ups (previously server-only).
- **All-local TTS via Piper** (from v0.38.0). New `[tts.piper]` config
  block with voice directory, per-locale defaults, and health probe.
- **Home Assistant and Piper Settings UI** (from v0.38.2). Configure
  HA bridge and Piper voice picker from the Settings page.

### Fixed

- **Wake-word with openWakeWord now actually fires.** The sidecar
  reset the consecutive-frame counter on non-scoring chunks; with
  `min_consecutive_frames = 2` no detection was ever emitted.
- **Dictation auto-stop after silence works again.** Falls back to an
  RMS-level detector when Silero VAD is unavailable.
- **Bundled Whisper model removed from the installer.** The 465 MB
  `ggml-small.bin` was inflating the installer from ~150 MB to
  ~530 MB. Speech models are now downloaded on-demand at first use
  via the built-in downloads manager.
- **Bundled ONNX Runtime bumped to v1.25.1** to match the API version
  expected by the wake-word sidecar.

### Changed

- Default `dictate_silence_timeout_sec` lowered from 10 s to 3 s.
- **Reproducible builds.** Windows builds pin `SOURCE_DATE_EPOCH`,
  pass `-trimpath` + `-buildvcs=false`, and digest-pin container
  base images.

### Supply chain

- `go mod verify` in CI, Trivy with `.trivyignore` baseline,
  pinned OpenAPI/AsyncAPI CLIs, determinism gate on the OSS export.

### Test coverage

- Voice Agent persona/role catalog ~0.24 → ~0.94, Assist shortcut
  resolver ~0.41 → ~0.77, auth-provider registry race-detector
  contract, five new packages in race-test scope.

## [0.38.6] - 2026-05-25

### Security

- **Server refuses to bind to a public address with no authentication.**
  The Linux container's `auth_mode = "none"` shortcut used to silently
  attach an anonymous identity to every request even on `0.0.0.0`. The
  defence-in-depth backstop now refuses the anonymous fallback when the
  bind address is non-loopback, so an operator who forgets
  `SPEECHKIT_AUTH_MODE` no longer ships an open server. Admin-session and
  smoke-token logins still work as configured.
- **Voice Agent WebSocket prefers a session ticket in the
  `Sec-WebSocket-Protocol` subprotocol over the legacy `?ticket=` query
  string.** The legacy form still works for backwards compatibility, but
  new clients ride the upgrade handshake and never leak the ticket to a
  fronting reverse proxy's access log. Cross-origin browsers without an
  `Origin` header are now rejected by default; native clients can opt in
  via `SPEECHKIT_ALLOW_EMPTY_ORIGIN=1`. Per-frame WebSocket read limit
  tightened from 1 MiB to 64 KiB (configurable via
  `[server] ws_read_limit_bytes`).
- **Admin settings PATCH now requires a CSRF token.** When a request
  comes in with an admin-session cookie (the browser-based login),
  state-changing settings calls (`PATCH /v1/server/settings`) require
  an `X-CSRF-Token` header that matches the new `speechkit_admin_csrf`
  cookie set on login. Bearer or edge-auth calls bypass CSRF — those
  credentials are not browser-auto-attached. The first-run setup UI
  already sends the header, so no flow change for end users.
- **Rate limiter is cost-weighted and demo traffic has a hard daily
  cap.** Expensive endpoints (`POST /v1/dictation/transcribe`,
  `POST /v1/assist/process`, `POST /v1/voiceagent/sessions`) draw more
  tokens than cheap GETs — configurable via
  `[server] rate_limit_endpoint_costs`. The new
  `[server] demo_daily_quota` ceiling caps Plan="demo" callers (the
  smoke-token surface) per UTC day so a casual scraper can't run up
  provider bills overnight. Service-bearer callers (`UserID="service"`)
  are now keyed on remote IP instead of sharing one global bucket so
  one noisy neighbour can't starve other consumers.
- **Wake-word upload guards a hostile activation ID and a quota race.**
  `metadata.id` is capped to 64 characters and constrained to
  `[A-Za-z0-9_-]` at validation time; oversized or path-traversal-shaped
  IDs are rejected with 400 before any filesystem work runs. Concurrent
  uploads from the same user are serialised by a per-owner lock, so two
  simultaneous uploads at the quota edge cannot both pass the
  "below quota" check and silently double the user's stored bytes.
- **Wildcard CORS is rejected when the admin login UI is enabled.**
  Wildcard origins combined with the admin session cookie surface would
  let any origin issue authenticated requests once the admin tab visits
  a hostile page. Now refused at config validation with an explicit
  error naming `admin_auth_enabled`.

### Fixed

- **Wake-word with the openWakeWord backend now actually fires.** The
  detector frequently produced scores above 0.8 for clearly-spoken wake
  phrases, but the sidecar reset the consecutive-frame counter on every
  PCM chunk that did not yet contain a full decoder window — and at the
  default 32 ms audio framing two thirds of chunks do not produce a new
  score. With `min_consecutive_frames = 2` the counter could never reach
  the threshold, so no detection event was emitted. The sidecar now only
  feeds real scoring frames into the consecutive-frame logic and dispatches
  the wake-word into the hotkey bus as designed.
- **Dictation auto-stop after silence now works on the Windows reference
  build.** The Silero VAD has been disabled since the recent ONNX runtime
  clean-up, which silently left dictation without a voice-activity
  detector — the silence-cutoff watcher never armed, so wake-word
  triggered dictation sessions ran forever. SpeechKit now falls back to
  a small RMS-level detector when Silero is unavailable, so the
  silence-cutoff watcher arms and a quiet pause ends the session as
  documented.

### Changed

- Default `dictate_silence_timeout_sec` lowered from 10 s to 3 s for a
  more responsive dictation experience. Existing config files that pin
  the previous value are untouched.
- **Reproducible builds.** Windows desktop builds now pin
  `SOURCE_DATE_EPOCH` to the HEAD commit timestamp and pass
  `-trimpath` + `-buildvcs=false` to every `go build` invocation, so
  two consecutive builds from the same commit produce identical EXEs.
  Useful for signing pipelines and binary-diff investigations.
- **Linux container base images are digest-pinned.** The server image
  now resolves `golang:1.26-bookworm` and `debian:bookworm-slim` to
  explicit `@sha256:...` digests so an upstream tag rebuild doesn't
  silently change the runtime. Dependabot's `docker` ecosystem rotates
  the digests weekly.

### Supply chain

- `go mod verify` is now part of the Go Analysis CI step.
- Trivy stops silencing CVEs without a fix; waivers move into
  `.trivyignore` with rationale + 90-day review TTL.
- OpenAPI/AsyncAPI spec-lint CLIs (`@redocly/cli`, `@asyncapi/cli`)
  are pinned to specific versions instead of `@latest`.
- The OSS publish workflow now runs a determinism gate before
  wiping the mirror; a non-deterministic export fails the gate and
  doesn't push.

### Test coverage

- Coverage on the Voice Agent persona/role catalog rose from ~0.24 to
  ~0.94, on the Assist shortcut resolver from ~0.41 to ~0.77, and the
  auth-provider registry now ships a race-detector contract.
- Race-test scope in CI gained five pure-Go packages
  (`auth`, `downloads`, `netsec`, `voicebehavior`, `voiceeval`).
- Flaky 1.1 s real-time sleeps in the storage tests were replaced
  with a `waitForNextSecond` helper.

### Documentation

- Added `doc.go` to sixteen internal packages that previously had no
  package-level comment.
- Architecture doc now calls out the one intentional kernel-to-SDK
  back-edge and the OSS-vs-Cloud build-tag seam so future refactors
  don't accidentally treat them as cleanup candidates.
- New `SECURITY.md` section documents the long-lived publish-OSS PAT,
  its 90-day rotation cadence, and the planned migration to a
  scoped GitHub App.

## [0.38.5] - 2026-05-22

Release-hygiene patch on top of v0.38.4 so the OSS publish workflow
can actually push to the public repository. The v0.37.6, v0.37.7, and
v0.38.4 publish attempts all failed the same "public surface check"
because two source comments named an internal secrets-store
project. Reworded both without changing behaviour.

### Fixed

- Comment above the default TTS profile constant no longer references
  the internal secrets-store project name. It now describes the same
  fallback rationale in vendor-neutral terms — an existing
  `GOOGLE_AI_API_KEY` is the most common pre-configured key, so the
  Google Studio-O default ships first.
- Same rewording applied to the `tts.google.studio-o-de` bullet in
  this changelog's 0.37.x release notes.

## [0.38.4] - 2026-05-22

Tech-debt sweep accompanying the v0.38.2 / v0.38.3 work. No new
behaviour for end users.

### Changed

- Version is now `0.38.4` across the root `package.json`, the
  frontend `package.json`, the Windows resource manifest, and the
  NSIS installer, restoring single-source-of-truth alignment with
  `CHANGELOG.md`. v0.38.0–v0.38.3 left the manifests at `0.38.1`
  because the release commits did not bump them.

### Fixed

- Voice-directory filename parser now uses an `else if` branch
  instead of a nested `else { if }` block (gocritic). No behaviour
  change.

## [0.38.3] - 2026-05-22

Docs sync — no functional changes. Brings the Voice-Companion phase
roadmap and the architecture overview up to date with the features
that have shipped through v0.38.2 (multi-turn skills in v0.38.0,
Piper TTS in v0.38.0, Home Assistant Settings UI + per-locale Piper
voice picker in v0.38.2). The previous text still listed
`home_assistant` as "Phase 4 (later)" and the TTS provider table
referenced backends that were never wired.

## [0.38.2] - 2026-05-22

Settings UI catches up with the Voice-Companion and Piper additions
shipped in v0.37.0 and v0.38.0/v0.38.1 — both were previously
TOML-only.

### Added

- New "Home Assistant Bridge" section under Settings → Integrations.
  Configure base URL, the env-var that holds the long-lived access
  token, and an optional language override. A "Test connection"
  button verifies the URL + token against the running HA instance
  without sending any utterance.
- New "Piper Local Voices" section under Settings → Integrations.
  Set the binary path and voice directory, then refresh the list to
  see every installed `.onnx` voice model. The per-locale dropdown
  pins which voice Piper uses for English and German output without
  hand-editing the TOML.
- New `POST /settings/homeassistant/test` and
  `GET /api/tts/piper/voices` endpoints on the Windows client back
  the two Settings UI sections.

### Changed

- The Settings UI also surfaces `tokenConfigured` for the configured
  Home Assistant token env-var, so users can spot a missing or
  whitespace-only token before clicking "Test connection".

### Fixed

- Partial Settings POSTs (e.g. from the onboarding wizard) no
  longer clear previously configured Home Assistant URL, token
  env-var, language, Piper enabled flag, binary path, voice
  directory, timeout, or per-locale default voice. The wizard only
  submits a handful of fields; before this fix, the missing fields
  silently flipped to empty values when the wizard finished. Same
  regression class previously fixed for the overlay-enabled flag.

## [0.38.1] - 2026-05-22

Voice-Companion Phase 2+3 ehrlich. Closes the gap between the
v0.38.0 release-note claim ("Multi-turn skills" + "All-local TTS")
and what the wiring actually delivered. Piper had no instantiation
path on either target — selecting it from settings would silently
fall through to cloud providers. Multi-turn skill follow-ups
worked on the server but not on the desktop. This release wires
both into both targets, ships the voice-fetch script, and adds
the `[tts.piper]` config block so operators can actually enable
the all-local stack end to end.

### Added — Piper Provider Wiring

- New `[tts.piper]` config block (`enabled`, `binary`, `voice_dir`,
  `default_voices`, `timeout_sec`). Voice models are not bundled;
  Piper stays disabled until an operator opts in and points
  `voice_dir` at a directory of `.onnx` files.
- Setting `[model_selection.tts] primary_profile_id = "tts.local.piper"`
  now actually routes to the Piper subprocess on both the desktop
  build and the standalone server. Before v0.38.1 the same setting
  resolved to an unregistered provider name and the synthesis
  request failed silently.
- New `scripts/prepare-piper-voices.ps1` and
  `scripts/prepare-piper-voices.sh` fetch ONNX voice models from
  `rhasspy/piper-voices` on Hugging Face. Defaults match the
  built-in fallback voices (`en_US-amy-medium`,
  `de_DE-thorsten-medium`); operators can pass `-Voices` /
  `VOICES=` to fetch other languages.

### Added — Multi-Turn on the Desktop Build

- The desktop build now allocates a 60-second in-memory skill
  follow-up store per app instance, reused across model-profile
  switches so an in-flight follow-up question survives a user
  changing the Assist LLM mid-dialog. Before v0.38.1 the Timer
  skill's "Für wie lange?" / "How long?" prompt on the Wails
  Desktop was silently disabled — the next utterance re-routed
  fresh instead of back to the skill.
- The desktop build sets a stable per-device session key on every
  Assist call so the multi-turn branch actually engages. The
  desktop is single-user, so a constant key is the contract; the
  standalone server keeps deriving its key from the authenticated
  user identity for multi-tenant isolation.

### Fixed

- `/api/v1/modes` contract count test had asserted 3 mode
  contracts but the default catalog has returned 4 since v0.37.2
  (when TTS became a first-class model-selection axis). The test
  now asserts 4 (Dictation, Assist, VoiceAgent, TTS).

## [0.38.0] - 2026-05-22

Voice-Companion goes beyond wake-word learning. This release lands
both **Multi-turn skills** and the **All-Local TTS stack** so the
Voice-Companion can hold a short follow-up conversation and answer
without any cloud TTS key. Pairs with the Ollama LLM provider
already shipped in the v0.37 chain — together they form a fully
offline-capable Assist Mode.

### Added — Multi-turn skills

- Multi-turn conversation slot per user. Skills that need one more
  piece of information from the user (e.g. "How long?" for a Timer)
  now keep the conversation open for 60 s instead of immediately
  punting to the LLM. The slot is keyed by user identity so
  multi-tenant deployments do not bleed conversations across users.
- Timer skill now asks "Für wie lange?" (de) / "How long?" (en)
  when the first utterance does not include a duration. On the
  next turn the spoken duration completes the request. If the
  follow-up still does not parse, the LLM takes over rather than
  re-asking forever.
- Pipeline option that lets the host enable or disable the
  multi-turn store. Hosts that opt in also need to supply a stable
  session key per request; an empty key skips persistence and
  preserves the v0.37 single-turn behaviour for callers that have
  not yet been updated.
- Server-target derives the session key from the authenticated
  user identity so each tenant gets their own conversation slot.
  The Assist pipeline mounts a 60 s in-memory store at boot.

### Added — All-local TTS (Piper)

- New Piper TTS provider that wraps the `piper` command-line
  binary as a subprocess. Reads UTF-8 text from stdin, writes a
  WAV stream to stdout, returns a `Result` with `Provider="piper"`
  ready for the existing TTS router.
- Default voice mapping for English (`en_US-amy-medium`) and
  German (`de_DE-thorsten-medium`). Callers can override per
  request via `SynthesizeOpts.Voice`. Unknown locales fall back to
  English so the provider stays useful even with a partial voice
  install.
- Locale normalisation helper that collapses `de-DE` / `EN_US` /
  `fr_FR` to short codes for the default-voice lookup.
- Voice models are NOT bundled. The `piper` binary must be on
  `PATH` or pointed at explicitly via configuration. The Health
  probe verifies the binary is reachable but does not validate
  voice models individually so a partial install does not block
  readiness for the locales you do have.

### Tests

- New unit tests cover the in-memory multi-turn store (set/get/
  clear, TTL expiry, defensive copies, nil-receiver safety,
  empty-key safety) and the Piper provider (voice resolution,
  constructor defaults, locale normalisation, WAV-header helpers).
- New integration tests demonstrate the full multi-turn flow
  through the Assist pipeline: a follow-up routes back to the same
  skill, two concurrent users keep their slots isolated, and an
  empty session key opts out of persistence.
- The Timer skill's prior "unparseable is silent" expectation was
  updated: an unparseable first turn now asks the follow-up
  question; a second-pass failure still falls through to silent.

### Implementation notes

- The store is process-scoped. Multi-replica server deployments
  that need cross-replica continuity should swap in a Redis-backed
  `SkillContextStore`; the interface is stable.
- Follow-up state is carried into the next turn via
  `ToolCall.Context` (newline-delimited `key=value` lines). Skills
  read it directly — no new ToolCall fields were introduced so
  existing single-turn skills continue to work unchanged.
- Default TTL is 60 s. Long-running multi-turn flows like
  reminder-collection that need more time should ship their own
  follow-up state with a `last_seen_at` marker and refresh on each
  turn rather than relying on the store TTL.

## [0.37.8] - 2026-05-25

Consolidated Hands-Free Voice-Companion release. Rolls up v0.37.0
through v0.37.7 into a single OSS release.

### Highlights

- **Hands-Free Voice-Companion.** "Hey Quby" now drives a one-shot
  Assist session with seven voice-oriented skills and an optional Home
  Assistant bridge for smart-home voice control.
- **Wake-word training data is opt-in and manageable.** Local
  activation capture, optional server upload, per-user quota, and a
  Settings tab cover the full browse, label, and delete loop.
- **Voice output can start locally.** Kokoro is the default TTS
  profile for fresh installs, Piper is available for local/HA-friendly
  voice output, and cloud TTS providers remain selectable fallbacks.

### Added

- **Hands-Free Voice-Companion.** "Hey Quby" now drives a one-shot
  Assist session with seven voice-oriented skills — Time, Date, Math,
  Weather, Timer, Reminder, and Wikipedia — plus an optional Home
  Assistant bridge for smart-home voice control. Voice-Agent mode
  keeps its existing multi-hour realtime semantics.
- **Multi-turn skill conversations.** Skills that need follow-up
  information (e.g. "How long?" for a Timer) keep the conversation
  open for 60 seconds instead of punting to the LLM. The slot is
  keyed by user identity for multi-tenant isolation.
- **All-local TTS via Piper.** New Piper TTS provider wrapping the
  `piper` CLI as a subprocess. Default voices for English
  (`en_US-amy-medium`) and German (`de_DE-thorsten-medium`). Voice
  models are not bundled — install Piper and point the config at your
  `.onnx` voice directory.
- **TTS as a first-class model-selection mode.** Five provider profiles
  ship in the default catalog: Kokoro 82M (local, default), Piper
  (local HA-compatible), Google Studio-O, OpenAI tts-1-hd, and
  Hugging Face Parler. The Settings UI exposes primary + fallback
  selection alongside Dictation, Assist, and Voice Agent.
- **Wake-word training data.** Local capture of wake-word activations
  (pre-roll + post-roll WAV + JSON sidecar), optional background
  upload to a SpeechKit server with per-user quota and multi-tenant
  isolation, a Settings UI tab for browsing/labelling/deleting clips,
  and an E2E test scenario covering the full upload round-trip. All
  toggles default OFF — explicit user opt-in required.
- **Home Assistant and Piper Settings UI.** Configure HA base URL,
  token env-var, and language override with a "Test connection" button.
  Set the Piper binary path and voice directory, browse installed
  voices, and pin per-locale defaults — all from the Settings page
  without editing TOML.

### Changed

- Catalog default TTS profile moved from Google Studio-O to
  Kokoro 82M (local) so fresh installs without a cloud key get a
  voice out of the box.
- `tts.local.piper-de` profile ID renamed to `tts.local.piper`.
  Operators who pinned the old ID need to update their config.

### Fixed

- Voice-Companion skills returning `Action="silent"` now correctly
  fall through to the Assist LLM when one is configured.
- Home Assistant intent routing fixed — the enabled flag was not
  flipped when HA config was present.
- Multi-turn follow-ups wired into the desktop build (previously
  server-only).
- Partial Settings POSTs (e.g. from the onboarding wizard) no longer
  clear previously configured HA or Piper settings.
- Internal source comments that named a private project have been
  reworded for OSS export compatibility.

## [0.37.7] - 2026-05-22

End-to-end test coverage for the wake-word activation upload chain.
`sk-e2e` now ships a `wakeword` scenario that walks the full
public-interface round-trip — POST a synthetic activation, GET the
metadata back, PATCH a label, DELETE the row — and gracefully
treats a 503 from a server that opts out of training-data uploads
as a smoke-pass.

### Added

- `sk-e2e --scenarios wakeword` exercises
  `POST/GET/PATCH/DELETE /api/v1/wakeword/activations` against the
  deployed server. Default scenario list now includes wakeword
  alongside health, deployment, dictation, assist, and voiceagent.
- The scenario probes `accept_uploads` via an upfront GET. When the
  server returns 503 with a structured `training_data_disabled`
  envelope the run logs the smoke-pass and exits clean; only
  unexpected statuses fail the build.
- Round-trip assertions: the GET response must echo back the
  client-supplied id and phrase_id, and the cleanup DELETE must
  return 204 so subsequent runs stay idempotent.

### Carry-over

- `install-e2e-windows.yml` retains its cron + manual + workflow_call
  triggers. Tag-push trigger was considered for symmetry with
  `install-e2e-linux.yml` but deferred — the Windows runner is paid
  and the 50 EUR/month spending cap takes precedence over symmetry.

## [0.37.6] - 2026-05-22

Wake-word activation Settings UI (Phase C — labelling + management).
Closes the v0.37.4+5 chain by giving users a Wails Settings tab to
browse the activations captured locally, play them back in the
browser, apply a label (`correct` / `false_positive`), and delete
clips they don't want kept.

### Added

- New "Wake-word training data" panel under Settings → Audio →
  Wake-word. Lists every locally captured clip newest-first, with
  inline audio playback via the standard `<audio controls>` element.
- Per-activation label dropdown (Unlabelled / Correct / False
  positive). Saving a label rewrites the JSON sidecar in place and
  resets the `uploaded` flag so the background uploader picks up
  the change on its next tick when remote upload is enabled.
- Per-activation Delete button (with a confirmation prompt). Both
  the WAV file and the JSON sidecar are removed from disk.
- Empty-state hints that distinguish "capture disabled — enable it
  in the panel above" from "capture enabled but nothing recorded
  yet — trigger the wake-word a few times".
- New local control-plane endpoints (loopback-only, no auth header
  needed because the Wails app is the only caller):
  - `GET    /api/wakeword/activations` — list locally captured clips
    + the directory path the sidecar uses + the current
    `local_capture_enabled` value.
  - `GET    /api/wakeword/activations/{id}/audio` — stream the WAV
    bytes for inline playback.
  - `PATCH  /api/wakeword/activations/{id}` — update label.
  - `DELETE /api/wakeword/activations/{id}` — remove .wav + .json.

### Tests

- 9 Go cases for the new routes (listing, ordering, audio stream
  Content-Type, label patch + uploaded-flag reset, bogus label
  rejection, delete, not-found, method-not-allowed, helper unit
  tests).
- Existing frontend tests continue to pass (4/4).

## [0.37.5] - 2026-05-22

Wake-word activation capture (Phase B — server storage + upload).
Activations captured locally in v0.37.4 can now flow to a SpeechKit
server, where they are stored per-tenant with strict owner scoping.
The endpoint defaults OFF on both ends: the device only uploads when
the user enables `upload_enabled`, and the server only accepts uploads
when the operator enables `[server.training_data].accept_uploads`.

### Added

- `POST /v1/wakeword/activations` accepts multipart uploads (audio
  WAV bytes + a metadata JSON form field). Returns `201` with the
  stored row, `409` for duplicate IDs, `413` when the per-user quota
  is exhausted, `503` when the operator has the feature off.
- `GET /v1/wakeword/activations` and `GET
  /v1/wakeword/activations/{id}` list and fetch the caller's stored
  activations. Every read is scoped to `Identity{UserID, OrgID}` so
  one tenant cannot see another tenant's recordings.
- `GET /v1/wakeword/activations/{id}/audio` streams the raw WAV
  bytes for review or re-labelling.
- `PATCH /v1/wakeword/activations/{id}` updates the `label` field
  (`correct`, `false_positive`, or empty for "unlabelled").
- `DELETE /v1/wakeword/activations/{id}` removes both the database
  row and the audio file from disk.
- New `[server.training_data]` config block: `accept_uploads`
  (default `false`), `audio_dir` (default
  `<model_dir>/wakeword-activations`), `per_user_quota_bytes`
  (default 1 GiB), `retention_days` (default 180).
- New `[wakeword.training_data]` device-side fields: `upload_enabled`
  (default `false`), `upload_server_url`, `upload_token_env`,
  `upload_only_labeled` (default `false`),
  `upload_interval_minutes` (default 5).
- Device-side background uploader that scans the local capture dir,
  POSTs each unsent activation, and marks the JSON sidecar
  `uploaded: true` after a successful `201` (or a `409` from a prior
  successful upload — idempotent).
- Health component `api.wakeword_training` reports the endpoint
  state on `/readyz`. Non-blocking so the rest of the server stays
  green when the feature is disabled or the configured store does
  not support activation persistence.

### Security & Privacy

- Multi-tenant isolation enforced at every read, write, label
  update, and delete. The store returns `sql.ErrNoRows` for
  cross-owner access instead of leaking existence.
- Path-traversal guard: client-supplied IDs are scrubbed down to
  alphanumeric + `-` + `_` before joining with `audio_dir`. The
  literal `..` is reduced to `_` so a malicious client cannot
  escape the audio root.
- `O_EXCL` on audio create prevents duplicate-ID overwrites; the
  server rejects with `409` instead of clobbering an existing file.
- Per-user byte quota is checked before reading the upload body so
  abusive clients fail fast on `413`.

### Forward Compatibility

- Both SQLite and Postgres backends satisfy the new
  `WakewordActivationStore` interface introduced in this release.
- The Settings UI for browsing, labelling, and deleting captured
  activations is scheduled for v0.37.6.

## [0.37.4] - 2026-05-21

Wake-word activation capture (Phase A — local capture only). Every
wake-word detection now optionally saves the surrounding audio
(pre-roll + post-roll WAV plus a JSON metadata sidecar) to a local
directory. Default OFF — explicit user opt-in required because the
feature touches microphone audio.

Server-side upload + Settings UI are scheduled for v0.37.5 + v0.37.6
respectively. v0.37.4 ships the local-only foundation so the captured
data lives on disk in a forward-compatible schema; manual
`scp`/`curl` upload works today, the auto-uploader + REST endpoints
land in v0.37.5.

### Added
- `internal/wakeword.TrainingCapture` — ring buffer of the last
  `pre_roll_ms` of S16 mono 16 kHz PCM. On every detection,
  snapshots the ring, collects `post_roll_ms` of further audio,
  writes a canonical RIFF/WAVE PCM file plus a JSON sidecar with
  id/phrase/score/backend/captured_at metadata. 11 unit tests
  covering ring rotation, post-roll latency, immediate-flush, no-op
  when disabled, concurrent ingest/trigger races, filesystem-safe
  filename normalisation, and WAV header shape.
- `WakewordConfig.TrainingData` (`WakewordTrainingDataConfig`) — TOML
  block `[wakeword.training_data]` with eight knobs:
  `local_capture_enabled` / `local_capture_dir` / `local_max_files` /
  `local_retention_days` / `pre_roll_ms` / `post_roll_ms` /
  `upload_enabled` / `upload_server_url` / `upload_token_env` /
  `upload_only_labeled` / `upload_interval_minutes`. ALL booleans
  default false; the upload knobs are forward-compat surface that
  v0.37.5's auto-uploader will consume.
- `ServerConfig.TrainingData` (`ServerTrainingDataConfig`) — sibling
  block `[server.training_data]` with `accept_uploads` (default
  false, gates the v0.37.5 REST endpoint), `audio_dir`,
  `per_user_quota_bytes` (default 1 GiB), `retention_days` (default
  180).
- Sidecar flags (both `speechkit-wakeword` and
  `speechkit-openwakeword`): `--training-enabled` /
  `--training-dir` / `--training-pre-roll-ms` /
  `--training-post-roll-ms`. The Wails host passes them through when
  `LocalCaptureEnabled=true` in TOML.
- IPC event `training_capture` — emitted once per WAV+JSON pair
  written. Carries `trainingId`, `trainingAudioPath` (basename),
  `trainingAudioBytes`, `trainingPreRollMs`, `trainingPostRollMs`
  plus the existing phrase fields. Lets the host UI (v0.37.6)
  refresh its activation list without polling the filesystem.
- `cmd/speechkit/desktop_wakeword_backends.go`:
  `resolveTrainingCaptureDir` (falls back to
  `%LOCALAPPDATA%/SpeechKit/wakeword-activations` on Windows,
  `~/.speechkit/wakeword-activations` elsewhere) +
  `ensureTrainingCaptureDir` (0o700 user-only permissions) +
  `effectiveTrainingPreRollMs/PostRollMs` helpers.
- `docs/wakeword-training-data.md` — full privacy contract, default-
  off matrix, architecture diagram, audit-event catalog entries,
  SQL schema (consumed by v0.37.5), REST endpoint reference
  (delivered in v0.37.5), DSGVO/GDPR compliance notes.
- bd issue `kombify-SpeechKit-bvg`-style tracker filed for the
  v0.37.4+5+6 chain.

### Behaviour matrix
| Toggle | Default | Effect |
|---|---|---|
| `[wakeword.training_data] local_capture_enabled` | **false** | Sidecar saves each detection's audio to `local_capture_dir`. No network traffic. |
| `[wakeword.training_data] upload_enabled` | **false** | v0.37.5 background uploader sends labeled clips to `upload_server_url`. Currently a forward-compat config field — no uploader yet. |
| `[server.training_data] accept_uploads` | **false** | v0.37.5 REST endpoint accepts client uploads when enabled. Currently a forward-compat config field — no endpoint yet. |

### Deferred (v0.37.5)
- `internal/store.WakewordActivationStore` (SQLite + Postgres impl).
- `POST /v1/wakeword/activations` multipart upload + `GET` / `PATCH`
  / `DELETE` per-activation endpoints + `GET .../audio` blob fetch.
- `internal/wakeword/training_uploader.go` background goroutine
  with backoff + retention worker.
- Audit-log events `wakeword.activation.captured` /
  `wakeword.activation.uploaded` / `wakeword.activation.deleted`.

### Deferred (v0.37.6)
- Wails Settings UI tab with activation list, audio playback,
  label dropdown (correct / false-positive / unknown), and
  batch-send button. Until then, captured clips can be inspected
  in any audio player (the WAV files are standard 16 kHz mono
  S16) and metadata edited in the JSON sidecar by hand.

### Verification
- `go test ./internal/wakeword/... ./internal/config/... ./cmd/speechkit/
  -count=1 -short` — green (11 TrainingCapture tests + 3
  sidecar-args tests + existing suites).
- `gofmt -s -l` clean on touched files.

## [0.37.3] - 2026-05-21

Voice-Companion TTS catalog refresh. Pulls in the current Hugging Face
TTS leaders as Local Built-in profiles so the v0.37 hands-free flow has
sane "ships in the installer, no API key needed" voices out of the box.
Required by the Thalia + Companion-Live deployments and aligns with
the user's request to integrate Hugging Face TTS alongside the
existing LLM and STT model selection.

### Added
- `tts.local.kokoro-82m` Local Built-in profile — Hugging Face's
  current TTS leader (68M+ downloads, 6.1k likes). Apache-2.0,
  StyleTTS2-based, ~50 MB ONNX (int8), bundled into the installer
  as the recommended Voice-Output default. Marked `Default: true`
  on the catalog so fresh installs without an API key get a local
  voice. Phase-3 runtime via `onnxruntime-go` sidecar.
- `tts.local.supertonic-3` Local Built-in profile — Hugging Face's
  trending #3 TTS model (May 2026, trending score 331). OpenRAIL,
  ONNX, multilingual across 32 languages including DE/EN/JA/AR/KO.
  Phase-3 runtime via same sidecar pattern as Kokoro.
- `tts.local.chatterbox-multilingual` Local Built-in profile —
  best open voice-cloning TTS on Hugging Face (`onnx-community/
  chatterbox-multilingual-ONNX`). MIT, 24 languages, voice-clone
  reference clip configurable per Persona. Phase-3 runtime.
- `tts.local.piper` Local Built-in profile — kept as the
  HA-compatible fallback (Piper is Home Assistant Voice's canonical
  engine). MIT, broad voice catalog (Thorsten DE, Amy EN, ...).
  Replaces the earlier v0.37.2 `tts.local.piper-de` entry; the old
  profile ID is gone — operators who pinned that string need to
  migrate to `tts.local.piper`.
- `internal/tts/router.go` `PreferredProviderForProfileID` extended
  with the four new local profile mappings:
    * `tts.local.kokoro-*`     → `kokoro_local`
    * `tts.local.supertonic-*` → `supertonic_local`
    * `tts.local.chatterbox-*` → `chatterbox_local`
    * `tts.local.piper(-*)`    → `piper`
  Each maps to a distinct internal Provider.Name() so the in-process
  runtime adapter (Phase 3) can register itself without colliding
  with the existing `kokoro` (OpenAI-compatible self-hosted) adapter.
- Router test `TestPreferredProviderForProfileID_MapsCatalogEntries`
  extended with the four new Local Built-in mappings.

### Changed
- Catalog `Default: true` moved from `tts.google.studio-o-de` to
  `tts.local.kokoro-82m`. The Direct-Provider Google entry remains
  the `DefaultTTSPrimaryProfileID` at the config layer (so installs
  with `GOOGLE_AI_API_KEY` already set keep getting Studio-O on
  bootstrap), but the catalog-wide default now points at the
  shipped-in-installer local model — the v0.37 "works out of box
  without a cloud key" baseline.

### Migration
- `tts.local.piper-de` → `tts.local.piper`. The voice variant
  `piper.de.thorsten-medium` is still the recommended DE voice
  inside the merged profile. No code change required if you
  pinned the variant, only if you pinned the profile-ID.

## [0.37.2] - 2026-05-21

Adds Text-to-Speech as a first-class entry in SpeechKit's model-
selection axis alongside the existing Dictation / Assist / Voice-Agent
mode pickers. Required for the Thalia + Companion-Live deployments
where the picked voice ("Studio-O DE", "OpenAI Nova HD", "Piper
Thorsten DE", ...) needs to be pinned per-install without editing
the lower-level `[tts.providers.*]` blocks. Builds on top of v0.37.1.

### Added
- `pkg/speechkit.ModeTTS` (`"tts"`) — fourth product-facing mode in
  the SDK catalog with its own contract (input=text, output=audio,
  `CapabilityTTS` allowed; everything else forbidden).
- `pkg/speechkit.IntelligenceVoiceOutput` — paired intelligence kind.
- Five new TTS `ProviderProfile` entries in the default catalog:
  - `tts.local.piper-de` (Local Built-in, Phase 3 stub for the
    voice-companion all-local stack)
  - `tts.openedai.kokoro` (Local Provider, OpenAI-compatible
    self-hosted Kokoro endpoint)
  - `tts.huggingface.parler-multilingual` (Cloud Provider, Parler-
    TTS via the HF Inference Router)
  - `tts.google.studio-o-de` (Direct Provider, recommended default —
    works out of the box when a `GOOGLE_AI_API_KEY` is already
    configured)
  - `tts.openai.tts-1-hd` (Direct Provider, six built-in voices)
- `ModelSelectionConfig.TTS ModeModelSelection` field — same shape as
  the existing Dictate/Assist/VoiceAgent mode selections. New TOML
  block `[model_selection.tts]` with `primary_profile_id` and
  `fallback_profile_id`.
- `DefaultTTSPrimaryProfileID` + `DefaultTTSFallbackProfileID`
  constants. Fresh installs default to Google Studio-O DE primary,
  OpenAI tts-1-hd fallback.
- `tts.OrderByPreferredProvider(providers, preferred)` and
  `tts.PreferredProviderForProfileID(profileID)` helpers in
  `internal/tts/router.go`. Both deployment-target wirings
  (`internal/server/core/assist_wiring.go` `buildTTSRouter` and
  `cmd/speechkit/app_init.go` `buildTTSRouter`) now consult these
  to pin the user's selected provider to the front of the
  strategy-determined order.
- Catalog regression tests: `TestTTSProfilesAdvertiseTTSCapability`,
  `TestNormalizeMode_TTSAliases`, plus extended
  `TestEveryModeExposesFourProviderKinds` to cover ModeTTS.
- Router tests: `TestOrderByPreferredProvider_*` (3 cases),
  `TestPreferredProviderForProfileID_MapsCatalogEntries` (10 cases).

### Changed
- `ValidateDefaultCatalog` now also walks ModeTTS — the V23 invariant
  "every mode exposes all four provider groups" is enforced for
  Voice-Output too. A future build that removes a TTS provider group
  fails the catalog test rather than shipping silently.
- `internal/server/catalog/handler.go` now exhaustively handles
  ModeTTS in `selectedProfiles`, `modeEnabled`, and `runtimeReady`.
  TTS is a capability-mode (consumed by Assist + VoiceAgent), so
  `modeEnabled` treats it as implicitly enabled rather than requiring
  `"tts"` in `[server].modes`, and `runtimeReady` derives readiness
  from `[tts].enabled`.

## [0.37.1] - 2026-05-21

Patch on top of v0.37.0 covering two issues caught immediately after
the v0.37.0 cut: the documented "skill returns silent → fall through
to LLM" contract was not wired into `assist.Pipeline.handleTool`, and
three Voice-Companion files needed a `gofmt -s` pass.

### Fixed
- Voice-Companion skills returning `Action="silent"` with empty Text
  now correctly fall through to the Assist LLM flow when one is
  configured. Without this fix, unparseable math expressions, Wikipedia
  disambiguation pages, and HomeAssistant requests on an unconfigured
  bridge would have returned literal silence to the user instead of
  letting the LLM answer. Legacy silent semantics (no LLM available
  OR explicit silent-with-text from host skills) preserved with two
  narrow guards.
- HomeAssistant intent now actually routes when configured. The default
  `UtilityRegistry` ships `home_assistant` with Enabled=false (the
  intent is only meaningful when a HA URL + Token are wired). The
  v0.37.0 cut forgot to flip the flag in both deployment targets when
  HA config is present, which silently sent "schalte das Licht aus" to
  the LLM instead of HA. Both the server (`buildAssistPipeline`) and
  the desktop (`initDesktopAssistRuntime`) now register an
  Enabled=true HA definition when URL+Token resolve.
- Repo-root `package.json` + matching `package-lock.json`,
  `installer/speechkit.nsi`, and `cmd/speechkit/winres.json` bumped
  to 0.37.1 alongside `frontend/app/package.json` (CI's
  changelog-lint reads the root manifest, not the SvelteKit
  app's). `scripts/sync-version.mjs --version=0.37.1` did the
  cascade.
- gofmt sweep on `internal/shortcuts/catalog.go`,
  `internal/assist/skills/voice_companion/weather_skill.go`, and
  `internal/assist/skills/voice_companion/timer_skill_test.go`
  cleared CI's Check-gofmt gate.

### Added
- Three regression tests in `internal/assist/pipeline_test.go`
  documenting the silent-fallthrough contract:
  - `TestSilentToolResultFallsThroughToLLM`
  - `TestSilentToolResultWithoutLLMReturnsSilent`
  - `TestSilentToolResultWithTextStaysSilent`

## [0.37.0] - 2026-05-21

Hands-Free Voice-Companion release. "Hey Quby" now drives a one-shot
Assist session with a catalog of seven voice-oriented skills (Time,
Date, Math, Weather, Timer, Reminder, Wikipedia) and an optional Home
Assistant bridge for smart-home control. Voice-Agent mode keeps its
multi-hour realtime semantics for Companion-Live and party-mode
use-cases. No public API breakage from v0.36 — the new pipeline is
opt-in via two TOML lines.

Detailed walk-through:
The v0.37.0 release notes were folded into this changelog.

### Added
- Seven new Voice-Companion skills under `internal/assist/skills/voice_companion/`
  answering Time, Date, Math, Weather, Timer, Reminder, and Wikipedia
  intents. All pure-Go, locale-aware (DE+EN), with httptest doubles
  for the HTTP-using skills so CI never reaches the live internet.
- Home Assistant Conversation API bridge as a Voice-Companion skill.
  Configure `[assist.home_assistant] url = "..."; token_env = "..."`
  to wire SpeechKit's wake-word into HA's intent engine for full
  smart-home control. The token is never stored in TOML.
- New `voice_companion.CompositeExecutor` pattern: skills dispatch
  first, host-side legacy executors (Copy/Insert/Summarize/QuickNote)
  fall through unchanged.
- `[assist.home_assistant]` config block for HA URL + Token-Env +
  optional Language override.
- 16 new IntentLexicon entries (8 intents × DE+EN) under
  `internal/shortcuts/catalog.go` covering "Wetter", "Timer",
  "schalte das Licht aus", "tell me about", and the rest of the
  Voice-Companion phrasebook.
- Public doc: [docs/voice-companion.md](docs/voice-companion.md) —
  canonical Voice-Companion design with pipeline diagram, skill
  surface, configuration block, latency budgets, phase roadmap.

### Changed
- Assist Pipeline construction on both deployment targets (Device-
  Target via `cmd/speechkit/desktop_services.go`, Server-Target via
  `internal/server/core/assist_wiring.go`) now wraps the host-side
  ToolExecutor with `voice_companion.CompositeExecutor`. Existing
  intents continue to work unchanged.
- `buildAssistUtilityRegistry` documentation clarifies that an unset
  `[assist].enabled_tools` returns the full default registry so
  freshly-shipped Voice-Companion skills are usable out-of-box.
  Hosts with an explicit allow-list see no behaviour change.

### Deferred
- MCP CLI Command Decomposition (the originally-planned v0.37
  outcome) moves to v0.38. The hands-free voice-companion work
  claimed v0.37 because it ships a user-visible capability — the
  MCP refactor remains internal-cleanup that can land on its own
  cycle.
- Multi-Room Satellite topology (LiveKit / ESPHome-API) stays a
  v0.38+ topic; v0.37 ships Single-Node only.

## [0.36.0] - 2026-05-21

Beta consolidation release. v0.36.0 bundles the v0.35.9 → v0.35.23
patch wave (15 patches over three days) into a single named Beta
release. Five themes consolidated: Onboarding 2.0, Installer
Hardening, Auto-End Family, Wake-word Reorg, Server Smoke Browser
Gate. Plus two long-open Desktop Runtime Stability tasks closed
(first-run init, shutdown cleanup with goleak). No public API
changes from v0.35.x — drop-in upgrade.

Detailed walk-through:
The v0.36.0 release notes were folded into this changelog.

### Added
- First-run startup now explicitly creates every persistent runtime
  directory (data, local data, secrets, audit log) and logs which
  ones were created versus already present, with the resolved path
  and permission mode. Previously each consumer performed its own
  ad-hoc directory creation on first write, making first-run
  permission errors hard to diagnose from a single log file.
- Shutdown now emits a single summary log line reporting total
  cleanup callbacks run, how many had explicit subsystem names, how
  many panicked, and total duration. A panic in one cleanup
  callback no longer orphans the later ones — every remaining
  callback still runs and the panic is logged with the registering
  subsystem name.

### Fixed
- Two Go source files had drifted out of `gofmt -s` since the
  v0.35.21/22 settings work landed (struct-field and inline-comment
  alignment). The CI gofmt gate is green again.
- Removed an unused frontend export that the strict dead-code gate
  had flagged: `onboardingWakeWordPhrases` from the dashboard
  setup-wizard data module. The export had been declared as "kept
  for the wake-word settings panel" in v0.35.20 but no callsite
  ever consumed it.

### Documentation
- `STATUS.md` and `ROADMAP.md` caught up to v0.35.23 and now
  declare v0.36.0 as the current focus, with the rolling Desktop
  Runtime Stability stream linked from the milestone-details
  section.
- A consolidated v0.36.0 release-notes document under
  `docs/release-notes/` walks through the five themes that rolled
  out across v0.35.9 → v0.35.23.

This release inherits every change from v0.35.9 → v0.35.23 — see
those per-tag entries below for the detailed feature-level
behaviour.

## [0.35.23] - 2026-05-21

v0.35.20 added a "Tips" panel at the top of the dashboard home as its
own section above the existing "Welcome to SpeechKit" / Quick Start
block. That meant the same screen now had two tips surfaces. This
release removes the standalone panel and folds the new tips
(wake word, providers) into the existing Quick Start grid as cards 04
and 05. Card 04 carries an "Enable in Settings" button, card 05 carries
an "Open Settings" button.

### Changed
- Dashboard home: the standalone Tips section introduced in v0.35.20 is
  gone. The wake-word and providers tips now live inside the existing
  Welcome → Quick Start grid as new cards (04, 05) with Settings CTAs.
- Quick Start cards now accept an optional CTA button rendered next to
  the body text. The original three feature-intro cards (hold-to-talk,
  hover-pill, summarize) stay text-only.

## [0.35.22] - 2026-05-21

Voice Agent hotkey-toggle now auto-ends on silence, the dictate silence
timeout is configurable from the General Settings page (no more TOML
edit required), and Assist mode inherits the same auto-stop because it
reuses the dictate pipeline.

### Added
- General Settings now has an "Auto-Stop on Silence" section with a
  seconds input. Set to `0` to disable. The value seeds both
  `dictate_silence_timeout_sec` in TOML and the runtime watcher
  introduced in v0.35.21.
- Voice Agent toggle-mode sessions now run the same silence + exit
  phrase auto-end policy that wake-word triggered sessions already
  used. Activated whenever the Voice Agent hotkey is configured as
  Toggle (not Hold-to-Talk). Hold-to-talk still terminates on key-up
  and does not need the watcher. The silence-cutoff value comes from
  the existing `[wakeword.auto_end]` TOML block (default 10 s plus the
  built-in DE/EN exit phrases).

### Changed
- Assist mode auto-stops on silence using the dictate timeout. Assist
  already used the dictate capture pipeline; the new watcher applies
  uniformly so a forgotten Assist session no longer keeps the
  microphone live.

### Verification
- Go test suites for the desktop client, config layer, and framework
  kernel pass locally.
- Frontend suite: 189/189 passing, TypeScript clean, ESLint clean.

## [0.35.21] - 2026-05-21

Dictate Mode now auto-stops after a configurable silence window. The
toggle-mode dictation session previously kept the microphone hot until
the user pressed the hotkey a second time — if they walked away, the
session stayed open. The new silence watcher ends the dictate session
after a configurable window of no observed speech (default 10
seconds). Hold-to-talk keeps releasing on key-up as before; the
watcher does not interfere there because hold-to-talk sessions are
typically short.

Wake-word configuration moved out of General Settings into its own
sub-page under Audio Settings. The General page was getting overloaded
and wake-word has enough surface area (phrase picker, backend chooser,
self-test, threshold) to deserve a dedicated tab.

### Added
- New config knob `[general] dictate_silence_timeout_sec` (default
  `10`). Set to `0` to disable the auto-stop watcher.
- New Settings tab "Wake Word" inside the Audio Settings nav group,
  alongside Integrations, SpeechKit Server, and Storage & Data.

### Changed
- Dictate Mode auto-stops on silence using the configured timeout.
  Hold-to-talk behaviour is unchanged; toggle-mode dictation no longer
  needs a manual second hotkey press to release the microphone after
  the user stops talking.
- Removed the Wake Word panel from the General Settings page. Existing
  configuration is preserved — only the location changed.

### Tests
- New unit coverage for the silence watcher: fires after the timeout,
  stays quiet while speech is active, and is cleared cleanly when the
  user stops the dictate session manually.

## [0.35.20] - 2026-05-21

The Wake Word step is gone from the onboarding wizard. Onboarding now
ends after the Integrations step. Wake-word configuration moves to two
places where users will actually look for it:

1. A new Tips section on the dashboard home page, always visible above
   Recent Activity, with one card titled "Use a wake word" linking to
   Settings.
2. The existing Settings → Wake Word panel, where the full backend
   configuration (phrase choice, backend selector, status indicator)
   already lived. Onboarding never added anything Settings did not.

The wizard is now four steps: Welcome → Local Model → Integrations →
Done (was five through v0.35.19, six through v0.35.17).

### Changed
- Onboarding flow shortened again: removed the Wake Word step. Welcome
  → Local Model → Integrations → Done.
- Integrations Continue button now lands on Done.
- Server-Connect submit now lands on Done (previously Wake Word).
- Progress-dot arrays trimmed to four steps.

### Added
- Dashboard home: new Tips section with three cards (Use a wake word,
  Master the three hotkeys, Tune your providers). All three CTAs open
  Settings. The section is always visible, not gated on empty activity
  state.

### Removed
- `WakeWordStep` component (and its imports of `enableWakeword`,
  `disableWakeword`, `fetchWakewordState`, `WakewordState`,
  `onboardingWakeWordPhrases`) from `setup-wizard.tsx`. The data
  export `onboardingWakeWordPhrases` is kept in `setup-wizard-data.ts`
  for use by `settings/wakeword-panel.tsx` callers.
- `wake_word` from the `WizardStep` union.

### Tests
- `setup-wizard.test.tsx`: server-connect test now asserts the Done
  step's "Start Using SpeechKit" button instead of the wake-word
  heading.
- `dashboard-app.test.tsx`: removed the `skipWakeWord` test helper and
  its five call sites — Integrations Continue lands on Done directly.

## [0.35.19] - 2026-05-21

The Voice Agent profile step is gone from the onboarding wizard.
v0.35.18 still showed a confirmation page even though the choice was
auto-derived — the page itself was redundant. The profile is now
applied as a side-effect of the welcome-screen target choice
(Local → On-Prem, Cloud or Server-Connect → BYOK Cloud), the apply
runs in the background, and the wizard advances straight from
Wake Word to Done. Failures are logged but never block onboarding;
users can re-apply from Settings later.

### Changed
- Onboarding flow shortened: removed the dedicated Voice Agent
  profile step. The wizard is now Welcome → Local Model →
  Integrations → Wake Word → Done (one step shorter than v0.35.18).
- Welcome-step target submit (Get Started + Use this server) now
  also calls `applyVoiceAgentProfile` with the derived profile.
  Failures are surfaced via `console.warn` and do not block the
  wizard transition.

### Removed
- `voice-agent-profile-step.tsx` and its test file — replaced by the
  silent background apply in the welcome handlers.
- `voice_agent_profile` from the `WizardStep` union and from the
  progress-dot arrays.

### Tests
- `setup-wizard.test.tsx`: added apply-profile mock + assertions
  that Local → `on-prem`, Cloud → `cloud-byok`, Server-Connect
  → `cloud-byok`, and that an apply failure does not block the
  wizard advancing to the next step.

## [0.35.18] - 2026-05-21

Onboarding no longer asks for the Voice Agent profile on the last step —
the choice is now derived from the Local-vs.-Cloud selection on the
welcome screen. Picking Local on the welcome card applies the On-Prem
cascaded profile; picking Cloud applies the BYOK Cloud profile. The
profile is applied on mount and the wizard advances to Done once the
backend confirms it.

The integrations step now warns when the providers a user has enabled
do not cover all three modes (Dictation, Assist, Voice Agent). The
warning names the missing mode(s) and lets the user finish onboarding
anyway — the missing capability can be filled in from Settings later.

### Changed
- Onboarding Voice Agent step: no more manual profile choice. The
  profile is auto-derived from the welcome-screen target choice and
  applied on mount. Continue is gated until the backend confirms the
  profile. A Retry button surfaces if `/api/v1/voice-agent/apply-profile`
  errors out.
- Onboarding integrations step: shows a coverage-gap warning when one
  or more providers are toggled on but the selection does not cover
  Dictation + Assist + Voice Agent together. Zero-selection (pure-local
  path) keeps its previous quiet behavior.

### Tests
- `voice-agent-profile-step.test.tsx`: auto-apply per target, gated
  Continue, Retry flow.
- `setup-wizard.test.tsx`: coverage-warning shown/hidden across the
  Hugging Face (full coverage), OpenAI (partial), and combined-provider
  scenarios.

## [0.35.17] - 2026-05-21

Silent installs (`/S`) no longer auto-launch the app or create a
Desktop shortcut. v0.35.16 shipped the FINISH-page checkboxes
correctly but NSIS MUI2 fires the underlying hooks even in silent
mode, regardless of the `_NOTCHECKED` defines — so a `/S` install
ended up doing both. Added an `IfSilent` guard at the top of each
hook so silent installs really stay silent.

### Fixed
- `installer/speechkit.nsi`: `LaunchSpeechKitFromFinishPage` and
  `CreateDesktopShortcut` short-circuit when `IfSilent` is set.
  GUI installs still expose the unchecked-by-default checkboxes
  and run the hooks only when the user opts in.

## [0.35.16] - 2026-05-21

Installer now respects the user's intent after copying files: a
"Launch SpeechKit now" checkbox and a "Create a Desktop shortcut"
checkbox both default to OFF, so a quiet install stays quiet. The
uninstaller now offers to remove user data (config, install state,
secrets, audio cache) alongside the binaries — without this, a
reinstall sees the prior `install.toml` with `setup_done=true` and
silently skips the onboarding wizard, which is exactly the trap
that caught the v0.35.14 first-launch testers.

Docs site header now carries the SpeechKit microphone icon next to
the wordmark, matching the desktop client's branding.

### Added
- NSIS finish page: "Launch SpeechKit now" + "Create a Desktop
  shortcut" checkboxes (both unchecked by default).
- NSIS uninstaller: prompt to also remove `%APPDATA%\SpeechKit\`
  (config.toml, install.toml, secrets, feedback.db, audio cache).
  Silent uninstalls (/S) default to NO so automation does not nuke
  user state.
- `Website/src/Layout.svelte`: SpeechKit favicon icon rendered
  next to the wordmark + Beta badge in the docs site header.

### Fixed
- Uninstaller now also removes the desktop shortcut and the `logs/`
  subdirectory under the install location.

## [0.35.15] - 2026-05-21

Wake-word panel no longer claims "Listening" when the sidecar has
only just spawned and there is no proof audio is flowing yet. The
status now progresses through three honest stages (`Starting` →
`Microphone open` → `Active` or `ERROR`) driven by real events from
the sidecar, and the first audio-flow heartbeat arrives within five
seconds instead of thirty.

Project is also marked as Beta on the docs site header and in the
repository README so deployment targets know to pin to specific
versions until SpeechKit reaches 1.0.

### Changed
- Wake-word panel status: honest three-stage messaging
  (`Starting … — waiting for audio confirmation` →
   `Microphone open ('Mic Name') — waiting for first audio heartbeat (~5 s)` →
   `Active — listening for 'X' → mode (audio Y KB/5s, Z decodes)` OR
   `ERROR — no audio reaching detector for 'X' (0 KB/5s)`).
  Sidecar emits the first heartbeat after 5 seconds so the user
  knows within five seconds whether the detector is actually
  receiving audio frames, instead of staring at an unconfirmed
  "green" indicator for thirty.
- `Website/src/Layout.svelte`: SpeechKit wordmark in the docs site
  header carries an inline `Beta` badge.
- `README.md`: top callout names the project as Beta and links to
  the changelog for breaking-change notes.

## [0.35.14] - 2026-05-21

Same feature surface as v0.35.13. The per-machine MSI now actually
runs — earlier MSI builds shipped without 7 runtime DLLs that
whisper-server.exe and the wake-word sidecars depend on, so users
who installed via MSI saw silently-hanging dictation and broken
wake-word recognition. The NSIS installer was always fine.

### Fixed
- MSI installer now bundles `ggml-base.dll`, `ggml-cpu.dll`,
  `SDL2.dll`, and the four Visual C++ 2015-2022 runtime DLLs
  (`msvcp140`, `vcomp140`, `vcruntime140`, `vcruntime140_1`).
  Without these the MSI-installed `whisper-server.exe` could not
  initialise its GGML compute backend and the wake-word sidecars
  could not open their audio input. End-to-end install-e2e gate
  now exercises the full bundle.

## [0.35.13] - 2026-05-21

Rebuild of v0.35.12 — same feature surface, but `wix build`'s
`-bindpath` flag did not actually resolve heat's `SourceDir\` token
in the v4 toolchain. Replaced the runtime bind with a one-time path
rewrite before `wix build` runs.

### Fixed
- Per-machine MSI installer for Windows: `heat dir`'s SourceDir paths
  now get rewritten to absolute filesystem paths in the
  llama-fragment.wxs immediately after heat emits the file, so
  `wix build` sees concrete source paths without needing the
  `-bindpath` indirection that v0.35.12 attempted unsuccessfully.

## [0.35.12] - 2026-05-21

Rebuild of v0.35.11 — same feature surface (onboarding target
selection, openWakeWord backend) but the per-machine MSI installer
builds again. v0.35.11 stayed as a draft because WiX v4 surfaced
two more strictnesses that v0.35.10 had not exercised; both are now
addressed.

### Fixed
- Per-machine MSI installer for Windows now builds end-to-end. WiX v4
  required `wix build -bindpath "SourceDir=<llama-staging>"` to
  resolve the `SourceDir\` paths in the heat-generated llama-fragment;
  added that flag to `installer/wix/build-msi.ps1`.

## [0.35.11] - 2026-05-21

The first onboarding step now asks how you want SpeechKit to run —
on this device, against a hosted cloud provider, or against your
own self-hosted SpeechKit server. Wake-word detection got a second
backend option so noisy environments and quieter rooms each have a
recogniser tuned for them. Release pipeline regained the ability to
build the per-machine MSI installer.

### Added
- Onboarding welcome step asks "Local, Cloud, or my own server?"
  before continuing into model selection. The default stays Local;
  Cloud routes directly to provider integrations; an unobtrusive
  "I have my own SpeechKit server" link opens an inline form to
  test and persist the server URL + bearer token without leaving
  the wizard. Once connected, the wizard skips local model setup
  and continues with wake-word and persona configuration.
- Second wake-word backend (openWakeWord) shipped alongside the
  existing Sherpa-ONNX KWS sidecar. Each backend has its own
  pronunciation profile; Settings exposes the choice so operators
  in different acoustic environments can pick the recogniser that
  performs best.

### Fixed
- Per-machine MSI installer for Windows now builds again. WiX v4
  removed the `Component@NeverOverwrite` attribute the previous
  build relied on; replaced the MSI-side `config.toml` install with
  the existing app-side first-launch seed from
  `config.default.toml`, which is upgrade-safe and was already the
  intended path in `seedRuntimeConfigFromInstallTemplate`.

## [0.35.10] - 2026-05-20

Second release-pipeline hotfix, completing what v0.35.9 attempted.
v0.35.9's GitHub Release stayed stuck as a draft because the build
exposed two more issues that v0.35.9 itself could not detect: a self-
inflicted exit-code interpretation in our WiX wrapper, and an upstream
regression in the whisper.cpp container image that rejected every
audio upload. Both are fixed here. SpeechKit's audio pipeline is
unchanged; locally verified that our WAV encoding round-trips
correctly through the pinned whisper.cpp image.

### Fixed
- Public GitHub Release for Windows now publishes without manual
  intervention: the MSI build no longer aborts after a successful WiX
  v3-to-v4 schema migration. The PowerShell wrapper had treated
  `wix convert`'s "changes-made" exit code (2) as failure; it is now
  recognised alongside 0 as success.
- The Linux install-end-to-end gate's dictation scenario passes again.
  The local-only test client used `ghcr.io/ggml-org/whisper.cpp:main`,
  which regressed on 2026-05-18 such that every WAV upload —
  including syntactically perfect 16 kHz S16 mono WAVs that our own
  decoder reads without issue — was rejected with `error: failed to
  read audio data from (RIFF...)`. Pinned to the last known-good image
  digest until upstream publishes a tagged release.
- install-e2e-windows.yml's MSI matrix leg now builds the staging
  bundle with `-SkipInstaller`. The previous unconditional invocation
  required NSIS even on the MSI leg, where NSIS is intentionally not
  installed.

## [0.35.9] - 2026-05-20

Fixes the public Windows release pipeline so the GitHub Release for
this version is published with all its artifacts attached, instead of
hanging as a draft like v0.35.4 through v0.35.8 did. There are no
runtime behaviour changes from v0.35.8: SpeechKit itself is identical,
only the release-time packaging is corrected.

### Fixed
- Public GitHub Release now publishes with the per-machine MSI
  installer (`SpeechKit-x64.msi`), the NSIS installer
  (`SpeechKit-Setup.exe`), the portable bundle
  (`SpeechKit-Portable.zip`), the CycloneDX SBOM, and signed checksums
  (`SHA256SUMS.txt`) attached. Builds since v0.35.4 had been blocked
  by a WiX v4 toolchain mismatch (XML comments containing double
  dashes, and the heat-generated fragment still using the WiX v3
  namespace) that left the public release stuck as a draft.
- The Linux install end-to-end gate now ships the dictation, assist,
  and voice-agent audio fixtures so the dictation scenario can run
  against the OSS-mirrored repository. The gate had failed since
  v0.35.4 because the OSS export manifest omitted the
  `testdata/e2e/` subtree.

## [0.35.8] - 2026-05-20

Wake-word triggers now activate a true live Voice Agent dialog that
ends automatically. A fresh install ships with a starter speech model
pre-bundled so onboarding completes without any download. The dashboard
explains exactly what went wrong if local setup ever fails and offers a
one-click recovery. Both log surfaces are now privacy-first opt-in, so
a fresh install writes nothing to disk by default.

### Added
- Voice Agent sessions triggered by the wake-word (e.g. "Hey Quby")
  now end automatically after a configurable silence interval
  (default 10 s) or when the user says one of the configured
  closing phrases (default DE + EN: "danke", "tschüss", "ende",
  "stop", "thanks", "bye", "goodbye"). Configure in `config.toml`
  under `[wakeword.auto_end]` with `silence_cutoff_sec` and
  `exit_phrases`. No hard-cap on session duration — Voice Agent
  remains designed for multi-hour dialogs.
- Windows installer now bundles the ggml-small Whisper starter
  model (~466 MB) plus the wake-word KWS bundle. A fresh install
  reaches "Start Using SpeechKit" without any model download.
- Setup wizard shows a precise error banner when local setup
  cannot complete (e.g. bundled model missing on a self-built
  installer), offers "Back to local model" + a one-click
  "Download starter model" that auto-retries setup once the
  download finishes.
- New `SPEECHKIT_LOG_LEVEL` environment variable overrides the
  configured log level at startup. Support engineers can flip on
  debug logging for one session without editing `config.toml`.
  Accepted values: `debug`, `info`, `warn`, `error`, `off`.

### Changed
- General application logging is now off by default. Set
  `[logging] level = "info"` (or `"debug"`) in `config.toml` to
  capture transcription events, mode switches, and wake-word
  triggers. Operators with a compliance obligation should also
  set `[audit] enabled = true` — the audit trail is now opt-in.
- Wake-word-triggered Voice Agent sessions ignore the configured
  Hold-to-Talk vs Toggle hotkey behavior and rely on the auto-end
  policy for termination (the wake-word has no key-release
  counterpart). Hotkey-triggered sessions are unchanged.
- Wake-word-triggered Voice Agent sessions no longer fall back to
  the dictation-style capture path when no realtime provider is
  active. Instead, the user sees a clear preflight hint asking
  them to configure a Voice Agent provider in Settings — the
  earlier behavior surprised users with "recording starts but
  transcription only on manual stop" semantics.
- Default local STT model is now `ggml-small.bin` and the local
  whisper-server port moved from 8080 to 9000 to avoid colliding
  with common development tools.
- Server connection targets are now explicit user/operator
  configuration. Previously-seeded preset targets are no longer
  injected at startup; operators upgrading from a private build
  with pre-seeded targets keep them (existing targets are sticky),
  but new installations start with an empty target list.

### Fixed
- Fresh installs now read the installer-written `config.default.toml`
  template instead of silently applying Go defaults. The NSIS
  installer writes the template into the install directory, but
  the runtime config path resolves to `%APPDATA%` for installed
  (non-portable) builds — a mismatch that left every fresh install
  using the wrong overlay anchor, log defaults, and STT model.
- Switching the dictation provider from a local model to a cloud
  model (e.g. Hugging Face) no longer silently keeps routing
  on whisper-server. The router strategy and the
  prefer-local-under-seconds window now reconcile to the
  picker's intent on every settings save.
- The overlay no longer renders ~28 px right of centre on
  Windows 11. The OS-enforced minimum frameless window size
  (~136×39 with DWM drop-shadow) is now honoured when computing
  the screen-centre offset, so the pill anchor lands where the
  positioning math expects it.
- Saving Settings from the onboarding wizard no longer flips
  `overlay_enabled` to false. The wizard's partial POST omits
  overlay fields entirely; the form parser now falls back to the
  current config value when a key is absent instead of treating
  `""` as "unchecked".
- Wake-word detections no longer drop under default settings.
  The sherpa-onnx KWS backend emits discrete keyword events, so
  the per-decode "require N consecutive hits" gate is now forced
  to 1 and the per-keyword consecutive-hits map is actively
  cleaned between decode windows. Previous behavior accumulated
  stale counters and could spuriously fire on a similar word
  said later in the same session.
- Voice Agent session-end audit events now carry the precise
  termination reason: `wakeword_silence` (silence cutoff fired)
  and `wakeword_exit_phrase` (exit-phrase matched), in addition
  to the existing `user`, `error`, and `idle` reasons.
- Wails Chromium webview no longer panics during fresh first-run
  when the dashboard scheduler's "first-run-setup" popup races
  with the hotkey-gate's "setup-required" re-trigger inside the
  same ~70 ms window. The second focus call is now skipped while
  Wails embeds the webview.

### Security
- Patched three transitive dependency vulnerabilities surfaced
  by the OSV scanner: `github.com/go-git/go-git/v5` from 5.19.0
  to 5.19.1 (GHSA-crhj-59gh-8x96 + GHSA-m7cr-m3pv-hgrp), and
  `ws` from 8.18.0 to 8.20.1 inside the Cloudflare quality-gate
  worker package (GHSA-58qx-3vcg-4xpx). The npm `brace-expansion`
  moderate finding (GHSA-jxxr-4gwj-5jf2) was also resolved in
  `frontend/app` via `npm audit fix`.

## [0.35.7] - 2026-05-19

Wake-word hotfix. The "Enable wake-word" button in the onboarding
wizard and every Settings save that touched a Wake-word field
were killing the listener within milliseconds of starting it. The
listener now stays alive until the user disables it or quits the
app. No public API change.

### Fixed
- Enabling Wake-word from the onboarding wizard's
  "Enable wake-word" button no longer produces the toast pair
  `Wake-word ready: "<phrase>" → <mode>` followed immediately by
  `Wake-word sidecar exited (code 1)` in the same second. The
  listener now keeps running and emits regular heartbeats.
- Saving any Wake-word change in Settings (phrase, threshold,
  default mode, cooldown) hot-reloads the listener into a
  long-lived state instead of leaving it dead after the
  Settings dialog closes.

## [0.35.6] - 2026-05-19

CI hardening: every release now includes a headless-browser
smoke against the deployed origin before the workflow declares
success. The previous gate exercised `/api/v1/*` directly with a
bearer token, which left browser-only regressions invisible (the
v0.35.3 JS-guard collapse and the v0.35.4 WebSocket Origin
rejection both passed the old CI before failing in production).

### Added
- A new headless-browser smoke step drives the public smoke
  page on `/` in real Chrome, clicks "Start Smoke Test", and
  asserts every tile (Server Settings / Health / Readiness /
  Dictation / Assist / Voice Agent) reaches OK and the page
  header reaches Passing. Default 3-minute timeout; the release
  fails if any tile is red.
- Both the tag-driven release workflow and the manual Render
  deploy workflow now run the browser smoke against the deployed
  origin after the existing API smoke. A red tile blocks the
  release rather than waiting to be reported as a production
  incident.

## [0.35.5] - 2026-05-19

Second hotfix in the v0.35.x server-target patch line. With the
v0.35.4 smoke-token guard fix in place, Dictation and Assist
work from the browser, but the Voice Agent WebSocket handshake
still failed with HTTP 403 because the WS handler's same-origin
check used an empty allow-list.

### Fixed
- `ApplyServerRuntimeDefaults` now folds the resolved
  `SPEECHKIT_PUBLIC_URL` into `cors_allowed_origins` automatically.
  Same-origin browser WebSocket connections from the configured
  public URL no longer return 403. Existing operator-configured
  origins are kept, duplicates are skipped, and an explicit `*`
  short-circuits the auto-derive.

## [0.35.4] - 2026-05-19

Hotfix for v0.35.3. The smoke page's in-browser auth guard was
silently disabled by the renderer that ships the smoke token to
the page. Public deploy looked healthy in CI (server endpoints
work when called directly) but every smoke tile in the browser
still returned 401.

### Fixed
- The JS guard in `smokeBearerToken()` now builds its sentinel
  by runtime string concatenation. The server-side
  `strings.ReplaceAll` that embeds the token can no longer
  rewrite the guard into a self-comparison that always matches
  the real token.
- Added regression tests that fail the build if a future change
  reintroduces a literal sentinel that the renderer could
  collapse.

## [0.35.3] - 2026-05-19

Server-Target patch. Public demo at `https://speechkit.kombify.io`
now drives all three modes from the browser, and the OSS container's
STT routing default no longer pretends a local whisper.cpp exists.
No public API change.

### Added
- Optional `SPEECHKIT_SMOKE_TOKEN` env var. When set, the smoke page
  on `/` embeds the token through a server-rendered meta tag and
  attaches it as a Bearer header on every fetch. The smoke identity
  is tagged with a public source and a demo plan so the rate-limiter
  and handlers can treat anonymous demo traffic differently from
  service callers. Never inherits an admin role.
- `[routing] strategy` is now spelled out as `cloud-only` in
  `deploy/config/server.example.toml`, matching what the container
  actually ships.

### Fixed
- Public smoke page no longer returns 401 on Dictation, Assist, and
  Voice Agent tiles when the server is configured for bearer auth.
- Dictation no longer returns
  `503 provider_unavailable: local provider not configured` on a
  fresh OSS container that has cloud STT providers configured but
  no in-process whisper.cpp.

## [0.35.2] - 2026-05-18

Desktop runtime diagnostics improvements. No public API change.

### Added
- Every startup log line and every event-loop panic log line now
  records the running version and Go runtime build, so a customer
  support bundle's first log line is enough to attribute a failure
  to a specific release without grep-walking the rest of the file.
- When the audio capture session fails to open at startup, the fatal
  log entry now classifies the cause (`category`) and prints a
  user-facing remediation hint (`guidance`) — distinguishing Windows
  microphone privacy denial, missing input device, exclusive-use
  conflict, and build/install mismatches. Reduces support-bundle
  triage time from "run the user through every audio-stack
  hypothesis" to "the category tells you where to start".

## [0.35.1] - 2026-05-18

Post-shipping stabilization patch for v0.35.0. No public API change.

### Fixed
- `go build ./...` is once again clean in cgo-disabled environments;
  the wake-word sidecar build is now correctly skipped without cgo
  rather than failing with a hard reference to its native bindings.
- The Wails startup race that panicked the desktop client when a
  hotkey arrived before the main event loop had fully initialised —
  typical on fresh installs with phantom keyboard state inherited
  from the launching shell — no longer dispatches against an
  uninitialised main-thread handler. The first-run dashboard popup
  is owned exclusively by the safe scheduler path that waits for
  the dashboard window to become ready.

### Added
- Companion release notes for v0.35.0 at
  `docs/release-notes/v0.35.0.md` covering the Enterprise Hardening
  track with upgrade notes and known limitations.

## [0.35.0] - 2026-05-18

This release closes the Enterprise Hardening track for production-pilot
readiness: an enterprise customer can now deploy SpeechKit across managed
Windows endpoints, lock the configuration via Group Policy, mirror the
audit log into Windows Event Log or an OTLP-capable SIEM, run end-to-end
DSGVO subject-rights workflows, and pick their Voice Agent compliance
posture from a setup wizard. None of the new behavior changes existing
defaults — every enterprise feature is opt-in and disabled until the
customer enables it.

### Added

- **Dedicated audit-log stream**: a separate JSON Lines file
  (`<exe-dir>/logs/audit-YYYY-MM-DD.log`, daily rotation, 90-day default
  retention) captures eight event types — provider selection, voice-agent
  session start/end, settings changes, update installs, auth failures,
  privacy export/delete, BYOK key updates, and policy applications. The
  audit log is the source of truth for SOC 2 / ISO 27001 / DSGVO Art. 30
  evidence; the existing runtime log stays focused on operator
  troubleshooting. See `docs/compliance/audit-event-catalog.md` for the
  v1 schema and `docs/compliance/ENTERPRISE-DEPLOYMENT.md` for the
  full deployment reference.
- **DSGVO Subject-Rights APIs**: `POST /api/v1/privacy/export` returns
  every scoped record (transcripts, voice-agent sessions, dictionary
  entries) as JSON or as a ZIP with the raw audio bytes — satisfies the
  Right of Access (Art. 15). `POST /api/v1/privacy/delete` removes the
  same records plus the audio files on disk — satisfies the Right to
  Erasure (Art. 17). Both endpoints require an explicit `confirm: true`
  for delete and emit dedicated audit events with the requester's
  Windows SID + record count. See `docs/compliance/dsgvo-subject-rights.md`.
- **Per-machine WiX MSI installer for SCCM/Intune**: the public release
  now ships both the existing NSIS per-user installer
  (`SpeechKit-Setup.exe`) AND a per-machine WiX MSI (`SpeechKit-x64.msi`,
  `ALLUSERS=1`, `INSTALLDIR=%ProgramFiles%\kombify SpeechKit`). MSI is
  MST-transformable and rollback-safe, ready for application packaging
  in SCCM, Intune Win32, or legacy GPO Software Installation. See
  `docs/runbooks/nsis-to-msi-migration.md` for the packaging recipe.
- **ADMX policy templates (English + German)** for centralised lockdown
  via Group Policy. Eight policy keys cover update on/off, internal
  update mirror URL, telemetry, local-only routing, cloud-provider
  block, audit retention days, Windows Event Log mirror, and OTLP
  endpoint. Drop `installer/admx/SpeechKit.admx` into
  `%SystemRoot%\PolicyDefinitions\` for single-host policy, or into the
  AD Central Store for domain-wide enforcement. See
  `docs/runbooks/admx-deployment.md`.
- **Air-gap-friendly update channel**: enterprise admins can disable
  auto-update entirely (`[update] enabled = false`) or point at an
  internal mirror serving the GitHub releases JSON shape
  (`[update] manifest_url = "https://artifacts.internal/..."`).
  Optional signing-cert thumbprint pinning
  (`[update] signature_pin_thumbprint = "..."`) defends against
  compromised signing certs by rejecting installers whose Authenticode
  thumbprint doesn't match. See `docs/runbooks/internal-update-mirror.md`
  for the static-S3 + nginx reference recipe.
- **Windows Event Log audit mirror (opt-in)**: set
  `[audit] event_log_enabled = true` and the audit events also appear
  in the "kombify SpeechKit" Application Event Log channel — ingested
  by Splunk, Sentinel, QRadar, and other SIEMs that follow Windows
  channels by default. One-time admin install via
  `SpeechKit.exe --install-event-log-source` or via the MSI's elevated
  custom action.
- **OTLP exporter for the audit log (opt-in, mTLS-capable)**: set
  `[audit] otlp_endpoint = "loki.internal:4318"` and the audit events
  ship as OTLP log records to Loki, Datadog, Splunk-OTLP, or any
  OTLP-capable backend. mTLS via
  `[audit] otlp_cert_file / otlp_key_file / otlp_ca_file`.
- **BYOK Gemini Live with EU region selection**: bring your own Google
  Cloud API key and pin to `europe-west3`, `europe-west4`, `us-central1`,
  or `asia-southeast1` from the Voice Agent settings UI or via
  `[providers.google] region`. Setting any provider's BYOK key emits a
  `byok.key_updated` audit event with provider name, configured region,
  and a truncated key fingerprint (no plaintext key in the log). See
  `docs/compliance/byok-gemini-region-pinning.md` for the operational
  guide and DPA template at
  `docs/compliance/dpa-templates/google-cloud-byok.md`.
- **Diagnostics support-bundle CLI**: `SpeechKit.exe --collect-support-bundle out.zip`
  produces a single ZIP with the last seven days of runtime + audit logs
  (secrets redacted), the active `config.toml` (provider keys redacted),
  Windows system info (no PATH, no system secrets), and the SBOM if
  available. `--include-transcripts` opt-in adds audit-log transcript
  content for cases where the customer wants to share them with kombify
  support after review.
- **Voice Agent profile decision wizard in the setup flow**: a new step
  shows two cards — "On-Prem (Local Cascaded)" with badges "Zero egress"
  + "BSI C5 ready", and "BYOK Cloud (Gemini Live)" with badges
  "Sub-1s latency" + "EU region". Selecting Apply writes the matching
  preset (`enterprise-onprem.toml` or `enterprise-cloud-byok.toml`) and
  records the choice in the audit log. The full decision tree (four
  questions + tradeoffs) lives at
  `docs/compliance/voice-agent-decision-tree.md`.
- **NTFS ACL check for `config.toml` on Windows**: SpeechKit walks the
  DACL via `GetNamedSecurityInfo` at startup and emits a clear warning
  (with the exact `icacls` command to fix it) when any access-allowed
  ACE targets `Everyone`, `Authenticated Users`, or `BUILTIN\Users`.
  Defends multi-user Windows hosts where the config file might hold
  provider keys.
- **Two enterprise preset configs** under `deploy/presets/`:
  `enterprise-onprem.toml` (Profile A — zero egress, local-cascaded
  Voice Agent, audit on, telemetry off) and
  `enterprise-cloud-byok.toml` (Profile B — Gemini Live with EU region
  + BYOK Google Cloud key + audit on).
- **`--no-telemetry` CLI flag**: bypasses every outbound non-provider
  HTTP call (currently the auto-update check) regardless of config.
  Useful when an enterprise images SpeechKit into a baseline config
  and wants a per-launch override.
- **Provider TOM data sheets** for every shipping provider
  (whisper.cpp, OpenAI, Groq, Google, HuggingFace, self-hosted VPS,
  Gemini Live, gpt-realtime-2) at `docs/compliance/providers/` — each
  sheet documents endpoint base, region options, subprocessor list,
  DPA URL, retention defaults, disable procedure, and compliance
  posture so an auditor's first stop is pre-answered.
- **Enterprise Deployment reference** at
  `docs/compliance/ENTERPRISE-DEPLOYMENT.md` — single-page reference
  for customer IT and auditor with the full egress whitelist
  (host, port, protocol, purpose, how to disable), air-gap profile
  example, install paths and NTFS ACL guarantees, every compliance-
  relevant config switch, audit-log layout, and a pre-demo
  verification checklist.
- **Local-Cascaded Voice Agent benchmark scaffolding** at
  `scripts/bench-voice-agent-cascaded.ps1` plus the results-skeleton
  doc at `docs/compliance/voice-agent-cascaded-benchmark.md`. Customer
  runs the script on three hardware tiers (baseline / mid / pro) and
  fills the doc with measured TTFB / P50 / P95 / WER numbers.

### Changed

- **Log rotation defaults raised from 5 MB × 3 files to 50 MB × 30
  files** so enterprise audit-window retention is realistic out of the
  box. Both limits are configurable via `[logging] max_file_size_mb`
  and `[logging] max_files`.
- **`settings.changed` audit event now emits one event per changed
  top-level config section** with a hashed before/after value, instead
  of a single generic event. Values themselves are NEVER logged — only
  16-character SHA-256 prefixes.
- **Voice Agent `session.end` audit event** now fires on user,
  error, and idle termination paths (previously only user). The
  `terminated_by` resource field distinguishes the cause so an auditor
  can reconstruct session lifecycle from the log alone.
- **Internal audit-log API** replaced positional `Configure(enabled,
  dir, retentionDays, eventLogEnabled)` with `ConfigureFromOptions`
  taking an options struct. The old four-arg `Configure` is kept as a
  thin wrapper so existing call sites keep working.

### Fixed

- **STT router dynamic-fallback now emits the `provider.selected`
  audit event** when cloud providers fail and the local provider
  takes over. The previous path called `local.Transcribe` directly
  and silently bypassed the emit helper, leaving a hole in compliance
  evidence for the most operationally interesting case.
- **`/api/v1/privacy/delete` now also unlinks audio files on disk**
  after removing the SQLite rows. The previous behavior left the
  bytes on the filesystem, partially defeating Right-to-Erasure.

## [0.34.9] - 2026-05-17

### Fixed

- **Local LLM now actually works after a fresh Windows install**: the
  NSIS installer copied SpeechKit.exe and whisper-server.exe but
  completely skipped the bundled llama runtime, so Assist and the
  Voice Agent cascaded path hit "missing model" errors on first use.
  The installer now ships the full `llama/` runtime (llama-server.exe
  + its private DLLs) alongside the Whisper components, so the local
  LLM path is available immediately after install — no manual file
  copy needed.
- **Self-hosted server routes to the local Whisper sidecar**: a fresh
  install of `scripts/install-server.sh` left the STT router on the
  Device-Target default of "local-only", which the Server-Target has
  no in-process Whisper for. Every Dictation and Assist request
  responded 503 "local provider not configured". The self-hosted
  defaults now force the strategy to "cloud-only" so the server
  reaches the bundled whisper sidecar through the configured URL.
- **Server `/readyz` stays green when local-only TTS is intentional**:
  the `api.tts_direct` health entry was marked blocking even when TTS
  was deliberately disabled (no cloud TTS key on a self-hosted
  install), forcing `/readyz` to 503 even though every other component
  was healthy. The endpoint now marks itself non-blocking when TTS is
  disabled by design.
- **Self-hosted Docker stack ships with working Whisper + llama
  sidecars**: `deploy/docker/docker-compose.yml` only declared the
  server + postgres, yet `install-server.sh` wrote env vars pointing
  at `http://speechkit-whisper:8080` and `http://speechkit-llm:8080`
  — hosts that did not exist anywhere in the stack. The compose file
  now declares both sidecars (profile-gated behind
  `COMPOSE_PROFILES=local`, which `install-server.sh` enables
  automatically) so a fresh self-hosted install actually has a local
  STT and LLM at the other end of the configured endpoints.

### Added

- **Install-rollout E2E gates on the release pipeline**: every
  released SpeechKit version now goes through two install-E2E
  workflows (`install-e2e-windows` on a clean windows-2025 runner,
  `install-e2e-linux` on a clean ubuntu-24.04 runner) before the
  GitHub Release is published. The Windows gate silent-installs the
  NSIS bundle and exercises Dictation, Assist, and the local cascaded
  Voice Agent against bundled Whisper + Gemma. The Linux gate runs
  `install-server.sh --strict-local-only` end-to-end against the same
  three modes through the public REST and WebSocket surfaces. See
  the local-only server guarantee.
- **`install-server.sh --strict-local-only` flag**: refuses to write
  the generated `.env` when any cloud provider key (Google, OpenAI,
  Groq, OpenRouter, Hugging Face) is set in the environment. Used by
  the install-E2E pipeline to assert the local-only contract; useful
  manually for verifying a self-hosted setup script does not silently
  pick up shell-exported secrets.
- **Server-side `providers.cloud_keys_present` flag on
  `/v1/deployment/status`**: a single boolean operators can scrape to
  verify a deployment is genuinely zero-cloud (false) versus
  configured to fall back to cloud providers (true).

### Changed

- **Voice Agent gains a fully self-hosted "local" provider on Windows**:
  the cascaded turn-based provider (STT → LLM → TTS) is now selectable
  from the Voice Agent provider menu on the Windows client in addition
  to the existing Linux server build. Pick "local cascaded" to run a
  Voice Agent session without a Gemini Live API key — Whisper handles
  the listening, the bundled Gemma model produces the reply, and the
  client speaks it through its own audio stack.

## [0.34.8] - 2026-05-17

### Added

- **kombify server presets pre-registered in the Windows client**: launching
  SpeechKit now seeds the Settings → Server Target dropdown with three
  switchable endpoints — speechkit.kombify.io (Origin), api.kombify.io
  (Gateway), and Hugging Face Inference. Pick a target, paste the matching
  token under Token value, and Server mode is ready without hand-editing
  config.toml. User-customised entries are kept intact across launches.

### Fixed

- **Wake-word now actually fires on a fresh install**: the bundled
  `keywords.txt` was written in a format sherpa-onnx could not parse, so the
  spotter crashed at startup before listening began. The boost factor is
  now space-separated from the BPE tokens (`▁HE Y ▁QU B Y :1.5 @hey_quby`),
  and the on-device sidecar boots into a healthy listening state for all
  five SpeechKit phrases (Hey Quby / Hey Computer / Hey Jarvis / Hey Mira /
  Hey Kombify) plus the four upstream defaults.
- **Onboarding wizard offers the SpeechKit catalog instead of generic
  defaults**: the wake-phrase picker no longer shows Hey Siri / Alexa /
  Hi Google as the primary options; it lists the curated SpeechKit phrases
  whose detection labels match the runtime catalog.
- **Rebuilds no longer wipe your local config.toml**: the build script
  preserves the bundle's runtime configuration across rebuilds, so
  customised ports, audio devices, server targets, and wake-word picks
  survive `scripts/build.ps1` runs. Delete `dist/windows/SpeechKit/config.toml`
  manually to reset to the example template.

## [0.34.7] - 2026-05-17

### Added

- **One-click wake-word in the onboarding wizard**: the setup flow now has
  a dedicated wake-word step after integrations. Pick a wake phrase
  (Hey Siri / Hi Google / Alexa / Hello World), click **Enable wake word**,
  and SpeechKit writes the config, spawns the sidecar, and shows the
  listening status before you continue — no manual config editing or
  follow-up downloads required. Skip preserves the previous hotkey-only
  default.
- **Dedicated `/app/wakeword/{enable,disable,state}` endpoints**: a small
  REST surface tailored for one-click activation. The existing Settings →
  Wake word panel keeps the full configuration UI for power users.

## [0.34.6] - 2026-05-17

### Added

- **Wake-word works on the Windows reference client out of the box**: the
  on-device keyword spotter now runs in a sibling process
  (`speechkit-wakeword.exe`) and streams detection events back into the
  desktop app via a JSON protocol. Enable it in Settings, say your
  configured phrase, and the corresponding mode starts the same way a
  hotkey press would. The bundled gigaspeech keyword model ships with the
  installer, so no model download is needed for first use.

### Changed

- **Wake-word resilience**: a crash in the keyword-spotter no longer
  affects the rest of the desktop app — the sidecar's exit is logged,
  status is surfaced, and the rest of the application keeps running.

## [0.34.5] - 2026-05-17

### Changed

- **Wake-word backend swapped to sherpa-onnx Zipformer keyword spotting**:
  the framework module now uses an Apache-2.0 keyword-spotter that ships a
  matching MinGW-compiled ONNX runtime, removing the Windows ABI mismatch
  that previously made the feature unreachable. Keywords are declared as
  text — no per-phrase model retraining is required.
- **Wake-word is framed as a client-side framework module everywhere it
  is documented**. The Server-Target does not and will not host always-on
  audio; the framework provides a kernel that client targets (Windows
  desktop today, Local-Target / Android / iOS / Web on the roadmap) embed
  via their own adapters.

### Added

- **Reproducible bundling of the wake-word stack**:
  `scripts/prepare-sherpa-runtime.ps1` ships the matching MinGW-compiled
  native libraries next to the Windows executable;
  `scripts/prepare-wakeword-model.ps1` fetches a pinned, SHA256-verifiable
  release of the KWS model into the bundle so the demo works out of the
  box once the in-process integration is replaced by the sidecar.

### Known limitation

- **Wake-word ships disabled by default on Windows**. Enabling it in the
  current build still triggers a native crash inside the Wails-alpha
  runtime that is not present in the standalone test runner; the
  sidecar-process pivot tracked under
  the follow-up public readiness issue is the next step.

## [0.34.4] - 2026-05-17

### Fixed

- **Wake-word startup can no longer kill the desktop app**: any unexpected
  panic during wake-word initialisation is now caught and surfaced as a
  status message instead of taking the whole application down. Wake-word
  remains opt-in while the underlying ONNX runtime integration is being
  hardened.

## [0.34.3] - 2026-05-17

### Added

- **ONNX Runtime is bundled automatically on Windows**: the Windows build
  step now downloads and verifies a pinned ONNX Runtime release and ships
  it next to the executable, so wake-word and voice-activity detection no
  longer fall back to whatever copy Windows happens to have installed.

### Changed

- **Wake-word and VAD share one runtime environment**: a single
  process-wide ONNX Runtime initialiser now backs both features instead
  of each one initialising and tearing down independently, which removes
  a class of "second consumer fails to start" issues seen on Windows.
- **Bundled runtime version mismatches surface a clear hint**: when the
  loaded ONNX Runtime does not match what the Go bindings expect, the
  log now says the bundle needs refreshing instead of an opaque
  "Platform-specific initialization failed" line.

## [0.34.2] - 2026-05-17

### Fixed

- **Voice Agent and Assist tolerate longer local responses**: the request
  timeout for local LLM calls has been extended so CPU-bound first-token
  latency no longer aborts replies with a "deadline exceeded" error.
- **Local LLM uses all available CPU cores**: the bundled llama runtime now
  scales its thread count to the host (up to 8) instead of running on a
  fixed 4 threads, so Assist and the Voice Agent pipeline fallback respond
  noticeably faster on multi-core machines.
- **Developer rebuilds keep install state intact**: rebuilding the Windows
  bundle no longer wipes `data/install.toml`, so a portable install does not
  silently bounce back to the onboarding wizard between iterations.
- **Developer rebuilds keep manually-supplied onnxruntime.dll**: operators
  who place a compatible ONNX Runtime DLL next to the executable keep it
  across rebuilds instead of having to copy it back each time.

## [0.34.1] - 2026-05-15

### Highlights

- **More complete Voice Agent embedding**: Go clients can create session
  tickets, dial realtime Voice Agent sessions, send text and audio frames, and
  close sessions through the public client helpers.
- **Local storage is more complete for desktop use**: history, quick notes,
  voice sessions, audio references, and scoped settings now share a clearer
  SQLite-first data model for device, user, and tenant use.
- **Windows first-run behavior is steadier**: startup, tray dashboard access,
  and focus handling are documented for portable and installer builds.

### Added

- **Voice Agent game-instructor example**: a new Go example shows a realtime
  game moderator using personas, player roles, sequence prompts, and duplex
  Voice Agent frames.
- **Storage and settings architecture docs**: local scopes, audio references,
  and SQLite defaults are documented as part of the public framework surface.

### Changed

- **Agent getting-started content is easier to reuse**: the public prompts now
  provide clearer starting points for web apps, Android app prompts, Voice Agent
  examples, and Go framework integrations.
- **First-run setup steps stay consistent**: setup wizard copy and step data are
  organized so the dashboard flow can be kept in sync more easily.

### Fixed

- **Postgres upgrades from older stores recover missing language columns**:
  existing deployments now add the normalized language fields before rebuilding
  history and voice-session indexes.

## [0.34.0] - 2026-05-15

### Highlights

- **Agent-ready app generation is clearer**: the public getting-started flow
  explains how generated apps should connect to a local SpeechKit Server and
  report Dictation, Assist, and Voice Agent results.
- **Windows package metadata is aligned for the 0.34 line**: portable and
  installer builds carry the updated 0.34 version metadata.
- **SpeechKit Server examples are easier to run locally**: generated app
  prompts now emphasize a fresh Docker Compose server for local integration.

### Changed

- **Generated app result files are more explicit**: result JSON now records the
  manifest link, mode outputs, and whether each SpeechKit mode was exercised in
  the app.
- **OpenAI Agents SDK runner guidance is refreshed**: runner instructions now
  match the current public prompt flow.

## [0.33.0] - 2026-05-14

### Highlights

- **Agent-ready getting started**: The website now gives coding agents
  copy-ready prompts, MCP setup guidance, and source docs in one place.
- **Storage groundwork**: Speech sessions can now link to first-class
  audio assets, expose storage stats, and return voice-session details for
  upcoming storage workflows.
- **Stricter server failures**: Empty Dictation transcripts and empty
  Assist results now return clear errors instead of looking like successful
  empty responses.

### Added

- **Agent-ready documentation**: The public website now keeps the short
  getting-started prompts, detailed agent guidance, MCP setup instructions,
  OpenAPI links, and Voice Agent AsyncAPI links together so coding agents can
  discover the framework from the site.
- **Advisory live prompt validation**: Maintainers can run the public
  website prompts through fresh coding-agent workspaces and Docker Desktop
  when promoting new agent examples.
- **Storage groundwork**: Speech sessions can now link to first-class audio
  assets, expose storage stats, and return `/api/v1/voice-sessions/{id}`
  details for upcoming storage workflows.

### Changed

- **SpeechKit Server responses fail closed for empty mode output**:
  Dictation returns a validation error when speech recognition completes
  without text, and Assist reports provider or pipeline unavailability instead
  of returning an empty successful result.
- **OpenAI realtime Voice Agent sessions use the current GA session shape**:
  The OpenAI realtime adapter now defaults to the current realtime model,
  sends the GA audio session format, and maps audio transcript events into the
  same Voice Agent transcript stream as other realtime providers.

### Fixed

- **Older local role databases are repaired automatically**: Existing
  installs with older role tables now receive missing columns at startup
  instead of failing the first admin override save.
- **First-run dashboard open no longer trips a WebView2 focus panic**:
  The local setup dashboard still opens automatically for new installs,
  but the first automatic show no longer forces focus while WebView2 is
  embedding the window. Manual tray and second-instance opens still bring
  the dashboard to the foreground.

## [0.32.2] - 2026-05-13

v0.32.2 is a Windows desktop client stability patch. No public API
change — `pkg/speechkit/**` remains backward-compatible.

### Fixed

- **Duplicate text injection when two SpeechKit instances were active
  at once.** If a second SpeechKit process was launched while one was
  already running, both processes registered the same global Windows
  hotkey and a single key press got handled twice — the transcript
  was typed into the focused application twice. The desktop client
  now claims a per-user-session Windows named mutex before any global
  resource is acquired and exits cleanly if another instance already
  holds it.
- **A crash in any Wails event-loop callback no longer takes down the
  whole desktop client.** Window close, window move, the application
  startup hook, and the voice-agent custom-event hooks are now
  guarded so an unexpected error in one is recorded in the log and
  isolated, instead of tearing down the event loop.

### Added

- **Structured startup, window, and tray telemetry in the local log
  file.** The desktop client now records timestamped events for each
  startup stage, every tray menu interaction, and every window show
  or close transition, so runtime issues can be reconstructed from
  the log file without a live repro.

## [0.32.1] - 2026-05-13

v0.32.1 fixes a Windows-client first-run UX bug. Before this release the
desktop binary launched silently into the system tray and the global
mode hotkeys (`Win+Alt+D`, etc.) were live immediately — so a user could
trigger a transcription before noticing the app was even running, with
no setup completed.

### Fixed

- **First-run onboarding now opens the dashboard automatically.** When
  the app launches with `InstallMode=local` and `SetupDone=false` the
  dashboard window is shown as soon as it is wired up (polled briefly
  during early startup). The setup wizard inside the dashboard is the
  intended first surface for a new install.
- **Mode hotkeys are gated until onboarding is complete.** Pressing the
  Dictate / Assist / Voice Agent hotkey before finishing the setup
  wizard no longer starts capture or activates a session. Instead the
  controller surfaces a one-line hint ("SpeechKit setup is not
  finished. Complete the onboarding before activating modes.") and
  re-opens the dashboard so the user can finish the wizard.
- The quick-capture auto-start tick honours the same gate, so an armed
  quick-capture cannot fire before onboarding either.

### Changed

- Existing test `TestDesktopInputControllerDictationHotkeyBlocksWhenLocalSetupPending`
  renamed to `…BlocksWhenOnboardingPending` and updated to assert the
  new onboarding-pending hint plus the `setup-required` dashboard-open
  call. Three new tests cover the same gate for the Assist and Voice
  Agent hotkeys and a positive control (gate must not fire once
  `SetupDone=true`).



## [0.32.0] - 2026-05-13

v0.32.0 is a hardening release for the SpeechKit framework and the
Linux server. No public API change — the SDK surface remains
backward-compatible and is now enforced by an automated stability
gate.

### Highlights

- **Public API stability gate**: every change to the SpeechKit SDK
  surface is now automatically checked for backward incompatibilities
  before it can land, so downstream embedders can rely on a documented
  no-breaking-change contract.
- **Voice Agent SDK on a stable public path**: the realtime Voice
  Agent runtime — sessions, idle timers, and workflow primitives — is
  now importable from the public SDK package instead of an
  internal-only path.
- **Server reliability**: three latent crash paths in the SpeechKit
  Server's rate limiter were replaced with graceful recovery, so a
  poisoned request can no longer take the server down.
- **Lighter desktop bundle**: a focused dead-code cleanup removed 27
  unused UI files, four unused dependencies, and roughly 5 MB of
  duplicated assets, with a strict check now blocking regressions.

### Security

- Replaced three `panic("bucketStore corruption: ...")` sites in the
  Linux server's `internal/server/middleware/ratelimit.go` with a typed
  `entryOf()` helper that logs via `slog` and recovers. A poisoned bucket
  entry can no longer crash the whole server process on the HTTP hot path.

### Added

- **Public API**: `pkg/speechkit/voiceagent/live` now exports `Session`,
  `IdleTimer`, and the realtime workflow types lifted out of
  `internal/voiceagent` (Phase C1, commits `2337339` + `b0a84b9`). The
  internal package retains alias-bridge declarations for backwards
  compatibility.
- **CI gate**: `Public API Stability` workflow job runs `apidiff` against
  the previous merge base for every PR touching `pkg/speechkit/**`,
  failing the build on incompatible changes (Phase C5, commit `fa85c24`).
- **New internal subpackages from `cmd/speechkit` decomposition**
  (Phase C2):
  - `cmd/speechkit/internal/transcription` — pure vocabulary-dictionary
    helpers and STT model-selection hints (PR 1, commit `59fc52f`).
  - `cmd/speechkit/internal/profiles` — pure model-profile selection
    helpers and local-provider validation, decoupled from `*appState`
    (PR 2, commit `7ad3078`). Path renamed from the original
    `internal/models` plan to avoid clash with the repo-root catalog
    package.
- **Per-package coverage gates** for the two new subpackages
  (`transcription` at floor 30%, `profiles` at floor 20%), and for the
  Linux server adapter packages (`internal/server/middleware` 65%,
  `internal/server/voiceagent` 60%, `internal/server/core` 40%).
- **Package-level documentation** for the public SDK surface
  (`pkg/speechkit/doc.go`, `pkg/speechkit/client/doc.go`) plus enriched
  package comments for `pkg/speechkit/{assist,dictation,voiceagent}` and
  the seven previously-undocumented kernel packages
  (`internal/{ai,dictation,models,router,shortcuts,stt,tts}`).
- The historical decomposition and audit plan files from this release line
  are retained in Git history; current follow-up scope is tracked in Beads.
- `npm run deadcode:strict` is now a CI step in the Frontend Checks job
  and gates merges on unused-code regressions.

### Changed

- **`cmd/speechkit/model_selection_helpers.go` slimmed from 351 LOC to
  192 LOC** — 9 pure helpers moved to `internal/profiles`. The four
  remaining helpers (`validateModeSelection`, `selectedModelSpecsForMode`,
  `applySelectedVoiceAgentProfile`, `syncConfiguredSTTRouter`) still
  depend on main-package state and stay in main until later C2 PRs.
- Test DSN in `cmd/speechkit/main_test.go` switched from a scanner-bypass
  string-concat workaround to a proper placeholder with
  `SPEECHKIT_TEST_POSTGRES_DSN` env override.
- `CONTRIBUTING.md` now documents the `dist/tools/` output convention
  for ad-hoc developer builds of `speechkit-mcp`, `speechkit-cli`, and
  `sk-e2e` so 20 MB binaries do not accidentally land at the repo root.
- `STATUS.md` and `ROADMAP.md` carry an "Audit 2026-05-13" section that
  cross-links the Phase A/B commits and the six SK-004.6.x Phase C
  tracking issues.

### Fixed

- **Frontend Checks CI**: knip strict scan failed on every main commit
  since `7391d0b` (the knip-strict introduction PR) because the
  Frontend Checks runner did not install `Website/` npm dependencies
  before running knip from the repo root, so `Website/vite.config.ts`
  could not resolve `vite`. Added a dedicated `npm ci` step inside
  `Website/` before the knip step (commit `4986d4a`). Main was first
  fully green again 2026-05-13.

### Removed

- 27 dead UI files (3,632 LOC) from the abandoned 2026-03-26 LiveKit
  agent iteration plus the unused shadcn/ui boilerplate components
  (calendar, command, dialog, input-group, input, popover, select,
  textarea, toggle), the `overlay-app.tsx` re-export wrapper, the
  `hooks/index.ts` barrel, and the orphan
  `use-{agent-control-bar,error-polling,logs}` hooks.
- Four unused npm dependencies: `@base-ui-components/react`, `cmdk`,
  `date-fns`, `react-day-picker`.
- Two orphan large assets (~5.5 MB freed):
  `assets/kombify_speechkit_logo.png` (zero references anywhere) and
  `frontend/app/public/bubble-icon.png` (Go runtime uses the embedded
  `assets/Bubble_Icon.png`; no second copy needed for the React app).
- Orphan stub `frontend/app/go.mod` (speechkit-ui module, no Go files,
  no script or `go.work` referenced it).

## [0.31.1] - 2026-05-12

v0.31.1 prepares the post-audit release line after the GitHub alert
remediation pass. It keeps the v0.31 production-readiness surface intact while
closing the remaining GitHub security, dependency, and contract-test warnings.

### Security

- Bumped `github.com/go-git/go-git/v5` to the patched release line so the
  default-branch Dependabot alert for GHSA-389r-gv7p-r3rp / CVE-2026-45022
  closes after merge.
- Kept CodeQL clean by documenting transparent response forwarding and
  smoke-test JWT/HMAC signing as non-password cryptographic use.

### Changed

- Updated GitHub Actions Node runners to Node 24 and moved pinned
  `actions/setup-node`, `actions/setup-go`, and `actions/setup-python` usage to
  the current v6 SHAs.
- Declared Node `>=24.0.0` for the root release tooling, frontend app, and
  Website package surfaces.

### Fixed

- Restored Schemathesis contract fuzzing with the current v4 CLI options and a
  no-auth loopback profile that excludes auth-only deployment probes.
- Tightened server API validation for catalog `mode` and transcript `limit`
  query parameters, and documented missing 404 responses for persisted Voice
  Agent reads.
- Synchronized the Website OpenAPI copy with the canonical server contract.

## [0.31.0] - 2026-05-11

v0.31.0 hardens SpeechKit Server for real hosted deployments. It separates
liveness from strict provider diagnostics, gives Assist and Voice Agent clearer
production smoke paths, and removes managed gateway assumptions from the local
client setup flow.

### Highlights

- **Production readiness contract**: `/healthz` remains a public liveness check,
  `/readyz` now reports only blocking startup dependencies, and
  `/readyz?strict=true` plus `/readyz/strict` expose full provider diagnostics.
- **Assist and provider diagnostics**: Assist now has a self-test endpoint and
  structured pipeline errors, while Gemini/Google AI generation uses
  provider-aware Genkit configuration.
- **Server-owned mode targets**: the Windows client now stores explicit
  operator-registered SpeechKit Server targets instead of managed gateway
  presets, with smoke coverage for the connection contract.
- **Voice Agent media readiness**: Voice Agent session metadata now reports
  `media_transport`, and Linux server builds can bridge LiveKit media when the
  native dependencies are present.

### Added

- Dedicated Google STT credential resolution via
  `SPEECHKIT_GOOGLE_STT_API_KEY`, with legacy STT variables still supported and
  `GOOGLE_AI_API_KEY` reserved for Gemini/Google AI.
- Strict readiness metadata for component kind, blocking status, provider, and
  supported modes.
- Docker, CLI, and production smoke checks for strict readiness and the Assist
  self-test envelope.
- OpenAPI and AsyncAPI documentation for strict readiness, Assist self-test, and
  LiveKit-backed Voice Agent media transport.

### Changed

- Server setup now creates admin Basic Auth credentials during onboarding and no
  longer assumes Kombify Cloud SSO for self-hosted installs.
- Release-gated overlay options stay hidden by default unless the corresponding
  frontend feature flag is enabled.
- GitHub Actions now run deeper Linux server validation, including native audio
  dependencies and Docker compose smoke coverage.

### Fixed

- Google STT readiness no longer appears healthy just because a Gemini/Google AI
  key is present.
- Optional provider degradation no longer makes orchestrator readiness fail in
  normal mode.
- Assist diagnostics now distinguish provider configuration failures from other
  pipeline errors so production smoke tests can fail with useful context.

## [0.30.1] - 2026-05-08

v0.30.1 is a docs and tooling polish release on top of v0.30.0. The
v0.30 docs surface now describes itself as the released stable line
instead of a still-in-progress preview, and the public toolchain has
been brought current with the latest Go security patches.

### Highlights

- **Stable v0.30 docs**: dropped the "Preview, not yet released"
  framing across the public docs (Getting Started, agent llms.txt,
  install-server.sh guidance, server README/DEPLOY); v0.30.0 IS the
  released line.
- **Toolchain bump**: Go 1.26.3 + `golang.org/x/net` v0.53.0 across
  the build pipeline, closing 17 stdlib + transitive vulnerability
  reports surfaced by govulncheck and OSV.

### Changed

- `docs/agent/llms.txt`, `docs/agent/llms-full.txt`,
  `docs/server/README.md`, `docs/server/DEPLOY.md`, and the public
  Website Getting Started pages no longer describe v0.30 as a
  preview. The `--channel preview` install flag and
  `:v0.30-preview` GHCR tag references have been removed where
  they conflicted with the released v0.30.0.

### Security

- Bumped Go toolchain to 1.26.3 and `golang.org/x/net` to v0.53.0,
  resolving GO-2026-4918 / 4971 / 4976 / 4977 / 4980 / 4981 / 4982 /
  4986 reported against the previous baseline.

## [0.30.0] - 2026-05-07

v0.30.0 introduces the public agent and integrator-facing
documentation surface for SpeechKit. Existing v0.29.0 desktop
installs auto-update; the new SpeechKit Server image is
`ghcr.io/kombifyio/speechkit-server:v0.30.0`.

### Highlights

- **Getting Started, two tracks**: a concise track for human
  integrators and a longer track for coding agents and MCP tools,
  both linked from the public Website.
- **Crawler-friendly Markdown entrypoints**: `/llms.txt` and
  `/llms-full.txt` give documentation-aware agents a single URL
  to load before writing integrations.
- **One-line server install**: `install-server.sh` writes a
  self-contained Docker Compose deployment with a generated bearer
  token, pinned to a stable image.

### Added

- Two Getting Started tracks (general + technical) with links from
  the Website.
- `/llms.txt` and `/llms-full.txt` Markdown endpoints for coding
  agents.
- Static OpenAPI surface published at the public Website root.
- MCP guidance and `speechkit-mcp` prompt updates so docs-mode
  agents start from the public Markdown / OpenAPI surfaces before
  writing integrations.
- `install-server.sh` for one-line SpeechKit Server installs.
- Feature-detected read-only WebMCP documentation context for
  browsers that expose `navigator.modelContext`.

## [0.29.0] - 2026-05-06

v0.29.0 is the post-audit release. It ships the highest-priority
security and correctness fixes from a comprehensive code-base
review, plus a settings-page reorganisation and a kernel/adapter
boundary cleanup. Superseded by v0.30.0.

### Highlights

- **Bootstrap window is one-shot**: the SpeechKit Server's setup
  flow can be opened only on a fresh, unbootstrapped install; once
  closed it cannot be reopened by editing the settings file.
- **Rate limiter is bounded**: the in-memory limiter now caps its
  bucket map with LRU eviction and a background sweeper, closing
  a slow memory-exhaustion path.
- **Frontend correctness**: dashboard clipboard feedback awaits
  success, log entries use stable React keys, settings hotkey
  patches are typed end-to-end.

### Security

- Server bootstrap (setup) window is one-shot per process; the
  auth-bypass window cannot be reopened by mutating the settings
  file at runtime.
- The `/setup` wizard returns 404 after onboarding completes and
  is gated by `SPEECHKIT_SERVER_ONBOARDING_UI`.
- In-memory rate limiter is bounded with LRU eviction and a
  background sweeper.
- Bumped transitive npm `ip-address` to ≥10.1.1
  (GHSA-v2v4-37r5-5v8g).

### Changed

- Windows-specific whisper-binary discovery moved out of the
  `internal/stt` kernel into platform adapter files.
- Settings page reorganised into shell + per-page panels with
  separate quick-note and quick-capture surfaces.

### Fixed

- Dashboard clipboard feedback now awaits the clipboard write
  before showing the success state; log entries use stable React
  keys; row handlers are stable across re-renders.
- Settings hotkey patches are typed end-to-end so a misspelled
  field key fails to compile.

## [0.28.3] - 2026-05-05

v0.28.3 is the production-readiness remediation release. It hardens server and
desktop security boundaries, makes the canonical build reproducible, reduces
the largest desktop/runtime complexity hotspots, and expands regression
coverage for the security-sensitive paths.

### Security

- Hardened SpeechKit Server Voice Agent WebSocket upgrades with explicit Origin
  enforcement, sanitized fallback host handling, and trusted `server.public_url`
  support for generated `ws_url` values.
- Changed edge-HMAC auth to sign `user_id`, `org_id`, `plan`, and `role` so
  `X-Edge-Role` cannot be modified independently after signing.
- Switched desktop control-plane token checks to constant-time comparison and
  reject empty expected or presented tokens.
- Moved installer update jobs to verify downloaded installer signatures before
  reporting completion, while keeping verify-before-open as defense in depth.
- Removed insecure Docker Compose defaults for server and Postgres credentials;
  local dev values now have to be provided explicitly.

### Changed

- Split desktop startup, Voice Agent activation, QuickNote routing, Assist
  delivery, Voice Agent receive dispatch, and AI provider registration into
  smaller helpers without changing user-facing behavior.
- Added deterministic normalization for generated frontend assets after the
  canonical Windows build and added CI gates to reject dirty build output.
- Updated GitHub workflow token handling so private dependency and OSS publish
  jobs use temporary job-local authentication instead of persisted tokenized
  remotes or global URL rewrites.
- Release metadata is bumped to `0.28.3` across root package metadata,
  frontend package metadata, Windows manifest, and installer metadata.

### Tests

- Added regression coverage for WebSocket Origin enforcement, `public_url`
  websocket URL generation, ignored `X-Forwarded-Host`, edge-HMAC role
  tampering, installer signature failures, desktop control-plane token
  validation, and named secret storage behavior.
- Cleaned up React settings/setup wizard `act(...)` warnings and reduced
  `cmd/speechkit` gocyclo findings to zero, including test-only complexity.

## [0.28.2] - 2026-05-03

v0.28.2 closes the setup and Voice Agent polish tranche after the v0.28
server hardening work. The release focuses on first-run server usability,
managed API-token setup, safer dev defaults, and the next layer of structured
Voice Agent workflows.

### Highlights

- **Setup-managed server API tokens**: the server setup page can now generate a
  bearer token for Windows clients and API callers, show it once, load it into
  the running process, and keep the raw value out of `server-settings.json`.
- **Self-managed auth stays possible**: operators can turn setup-managed token
  generation off when bearer tokens, edge auth, or local-only access are handled
  outside the setup UI.
- **Voice Agent workflow behavior**: built-in Voice Agent behavior profiles,
  sequence defaults, step transitions, and transcript checks now share one
  catalog between the Windows runtime and the central server.
- **Safer dev-server defaults**: local development now routes modes to the dev
  server by default and keeps Whisper Large v3 Turbo selected as the standard
  local STT model.

### Added

- Added server settings support for `server_auth` with managed bearer mode,
  self-managed mode, write-only token generation, and runtime auth refresh.
- Added a bootstrap-only settings-write auth bypass so first-run setup can
  create the initial token when bearer auth is enabled but no token exists yet.
- Added setup UI controls for generating the server API token, choosing the
  bearer env var, copying the one-time token, and reviewing API-auth state.
- Added Voice Agent sequence/workflow state for local and server sessions,
  including entered/completed step frames and deterministic workflow transcript
  evaluation helpers.

### Changed

- Auth middleware now resolves mode and bearer token dynamically per request so
  setup-generated tokens are accepted without rebuilding the middleware chain.
- Server settings snapshots now expose safe auth metadata, including auth mode,
  bearer env var, and whether a token is configured, without returning secrets.
- Voice Agent persona/profile defaults now flow through the shared behavior
  catalog, including default sequences and step instructions.
- Release metadata is bumped to `0.28.2` across root package metadata,
  frontend package metadata, Windows manifest, and installer metadata.

### Fixed

- Fixed the first-run server setup loop where bearer auth could be enabled but
  no usable token had been generated for the Windows client yet.
- Fixed dev startup/configuration defaults that could leave the dev server
  unselected and fall back to the small local model instead of Turbo.
- Tightened overlay surface layout behavior and mode-source test coverage for
  the updated runtime routing experience.

## [0.28.1] - 2026-05-01

v0.28.1 cleans up the server deployment contract after the v0.28 hardening
release. SpeechKit now documents and ships one central server image, with Voice
Agent as a normal mode of that server rather than a second deployment variant.

### Highlights

- **One central server image**: `ghcr.io/kombifyio/speechkit-server` is the only
  published server image. Dictation, Assist, and Voice Agent all live behind the
  same HTTP/WebSocket adapter.
- **Simpler installs**: the server installer now offers `--onboarding` and
  `--ready` setup modes for the same stack instead of `server` versus `voice`
  profiles.
- **Deployment guardrails**: CI, release Docker publishing, dev deploy, and
  releaseguard checks now reject legacy split-server artifacts before they can
  be built or deployed.

### Changed

- Removed the `cmd/speechkit-voice` binary, the `speechkit-voice` Docker target,
  voice-only Compose files, voice image environment contract, and GHCR release
  matrix entry.
- Rewrote server docs, OpenAPI naming, website copy, and deployment contract
  language around the central SpeechKit Server architecture.
- Kept `pkg/speechkit/*` unchanged; the reusable framework boundary remains the
  public Go surface, while the server stays a thin HTTP/WebSocket adapter and
  the Windows app stays a UI/client.
- Bumped Windows/frontend release metadata to `0.28.1`.

### Fixed

- Hardened the Windows control-plane origin guard so mutating requests reject
  non-HTTP origins.

## [0.28.0] - 2026-04-30

v0.28.0 is the production-hardening release. It tightens the release
gates around security scanning, CI coverage, server authentication, and
Windows release verification before the next public rollout.

### Added

- Added a dedicated security workflow covering secret scanning,
  dependency vulnerability checks, container scanning, Go vulnerability
  scanning, static analysis, and gosec.
- Added releaseguard checks for the security workflow, pinned GitHub
  Actions, branch-protection documentation, Website CI coverage, and
  production-safe server auth defaults.
- Added a 60% focused coverage gate for the release-critical Go package
  set.

### Changed

- Server examples and deployment docs now default production deployments
  to bearer or edge-HMAC authentication; `auth_mode = "none"` is
  documented only as an explicit local-development choice.
- Website dependency locking was refreshed to remove the PostCSS audit
  finding and keep Website checks reproducible.
- CI now runs the Website check/test path explicitly and keeps
  Dependabot coverage aligned with existing package roots.

### Fixed

- Fixed the hotkey nil-error path and tightened F-key lookup behavior.
- Hardened audited Windows, subprocess, path, and secret-store code
  paths so lint and gosec findings are either fixed or documented with
  scoped justifications.
- Stabilized the generated frontend asset set for the updated dashboard
  bundle.

## [0.27.0] - 2026-04-27

v0.27.0 is the public release that makes the large v0.26 server work
visible and usable. The main story is the Server-Target release
surface, realtime Voice Agent WebSocket support, clearer deployment
options, and documentation that explains the new modular architecture
without requiring private project context.

### Highlights

- **Realtime voice is part of Server-Target**: SpeechKit now includes a
  server deployment profile for realtime Voice Agent conversations.
  Teams can run voice workloads through the same Server-Target contract
  and deploy them with their own scaling path.
- **Server deployment docs are first-class**: The public docs now
  explain the Windows app, Go framework, and Server-Target, with setup
  and API guidance for operators.
- **Modular architecture is clearer**: Dictation, Assist, and Voice
  Agent share one framework core while staying cleanly separated across
  local, desktop, embedded, and server deployments.

### Added

- **Realtime voice on the central server.** The release now documents
  Voice Agent WebSocket workloads as part of the central
  `speechkit-server` contract.
- **Full server release surface.** SpeechKit can now be deployed as a
  server for HTTP Dictation, HTTP Assist, realtime Voice Agent
  WebSocket, health/readiness checks, auth middleware, rate limiting,
  personas/catalogs, and persistence-backed operation.
- **Split deployment routing.** Desktop and device builds can route
  Dictation, Assist, and Voice Agent independently to local providers or
  remote SpeechKit servers, so teams can keep light work local and move
  heavier workloads to server infrastructure.
- **Self-hosted Voice Agent option.** The cascaded Voice Agent path is
  documented as a CPU-friendly alternative that combines speech-to-text,
  model orchestration, and text-to-speech when Gemini Live is not the
  desired realtime provider.
- **Server smoke testing.** The release includes documented smoke
  scenarios for health, dictation, assist, and voice-agent flows so
  operators can validate a running server stack.

### Changed

- The public README now starts with the actual product variants:
  Windows app, embeddable Go framework, and Server-Target. It keeps the
  start page concise and links to deeper docs instead of embedding every
  server/API detail.
- Website release copy now tells the same story as the release notes and
  surfaces realtime voice as part of the Server-Target.
- The release workflow now verifies the public release, the server image
  workflow, and the website deploy before a release is treated as
  complete.

### Docs

- Reworked the public README into a concise overview with a deployment
  variant matrix, key feature list, quick-start paths, and links to the
  deeper framework, server, API, examples, release, and trust docs.
- Restored and linked the server documentation, including deployment,
  migration, and API guidance for the Server-Target.
- Sanitized the Server-Target deploy guide so public docs no longer
  reference private infrastructure details.

### Fixed

- Added public-surface checks so future exports fail before server
  documentation can disappear from a release.
- Fixed the website/release pipeline path that allowed stale website
  release content to remain visible after the public source and assets
  were already updated.

## [0.26.1] - 2026-04-27

### Fixed

- Hardened the internal dev rollout pipeline:
  `auto-deploy-dev.yml` now streams runtime env + compose payloads over
  SSH with retries, avoiding `scp` transport fragility on short-lived
  runner/network interruptions.
- Kept v0.26 release automation unblocked while the newly-opened
  `internal/server` lint-hardening backlog is addressed in follow-up
  PRs, by scoping temporary golangci exclusions for that package tree.

## [0.26.0] - 2026-04-27

The first OSS release of SpeechKit's Server-Target. SpeechKit now
ships in three deployment shapes — Device-Target (Windows reference
UI), Local-Target (library/CLI), and Server-Target (containerized
HTTP/WebSocket service) — all backed by the same Framework kernel.
The Server-Target publishes one central image, `speechkit-server`.

### Added

- **Server-Target (first OSS release).** `cmd/speechkit-server` plus
  `internal/server/{core,middleware,dictation,assist,voiceagent,
  persona,audio,httpx,cli}`, `deploy/docker/Dockerfile.server`, the
  dev/test docker-compose stacks under `deploy/docker/`, and the
  operator guide + OpenAPI 3.1 reference under `docs/server/`.
  Single Linux binary exposes the same Framework kernel as the
  device/local-targets over HTTP + WebSocket. Three modes
  (Dictation, Assist, Voice Agent), `/healthz` / `/readyz`,
  bearer + Cloudflare-edge-HMAC auth, rate limiting, persona/role/
  sequence catalog with TOML seeds + DB overrides, and a SQLite
  persister behind a clean `Persister` interface for Postgres
  to follow.

- **Self-hosted Voice Agent provider — Cascaded.** `cascaded`
  provider in `internal/server/voiceagent` runs whisper.cpp + Genkit
  + TTS as a CPU-only realtime alternative to Gemini Live. Selected
  via `[voice_agent].provider = "cascaded"` in `config.toml`.

- **End-to-end harness.** `cmd/sk-e2e` driving the running server
  with four scenarios (`health`, `dictation`, `assist`, `voiceagent`),
  plus `scripts/test-e2e-local.sh` and `scripts/test-e2e-local.ps1`
  helpers that bring the dev docker-compose stack up, wait for
  `/healthz`, run the smoke client, and tear the stack down again.

- **ModeSource architecture (foundation for v0.26.1).** Optional
  per-mode toggle that lets the device-target run any subset of
  Dictation, Assist, and Voice Agent against a remote SpeechKit
  Server-Target instead of the in-process kernel. Ships as four
  layers:
  - `[server_connection]` config section + `mode_source` field on
    each `[model_selection.<mode>]`. Both default to "off"/"local",
    so existing configs keep behaving exactly as before.
  - `internal/serverclient` transport adapter — typed HTTP/WS client
    that satisfies the same Go interfaces (`stt.STTProvider`,
    `assistpkg.Processor`, `voiceagent.LiveProvider`) the kernel
    already uses. Drop-in replacement at construction time.
  - Routing in `cmd/speechkit/server_delegates.go` with a
    `compositeTranscriber` that does server-first with optional
    fallback to local on transport-class errors. Application errors
    (typed `*ServerError` with a code) propagate as-is.
  - Settings UI in the General tab: `ServerConnectionCard` for the
    connection metadata + `ModeSourceSection` with three per-mode
    toggles. Server pill auto-disables (with hint) when the
    connection is off or the bearer-token env var is missing.
  - Per-mode `ModeSource` lets end users choose which modes run
    locally and which modes call the central SpeechKit Server.

### Changed

- **`cmd/speechkit-server/main.go` is now a thin wrapper** around
  `internal/server/cli.Run()`. The CLI handles flag parsing, config
  loading, logger setup, and the lifecycle handoff to
  `internal/server/core`.

- **`pkg/speechkit.ModeSetting`** gains an optional `modeSource`
  field; `ModeSettings` adds a `serverConnection` block that strips
  the bearer-token value before crossing the API boundary (only the
  env var name + a `bearerTokenSet` boolean travel).

- **CI runner policy.** `Go Analysis` moved from Windows to Linux
  (it does pure Go analysis — vet, fmt, golangci-lint, race,
  staticcheck, govulncheck — none of which need Windows). The Wails
  Windows bundle build moved out of `ci.yml` into its own
  `windows-build.yml`, gated by a guard that prevents
  GitHub-hosted `windows-2025` from being a silent fallback on the
  private dev repo. Tag-release workflows
  (`release-server-docker.yml`, `release.yml`) are now repo-pinned
  to `kombifyio/SpeechKit` so a misplaced tag in the dev repo
  cannot trigger a publish.

### OSS notes

- The v0.25 sync exclusions for `cmd/speechkit-server`,
  `internal/server`, `deploy/`, `docs/server`, and the docker
  publish workflow are removed for this release. External users can
  pull `ghcr.io/kombifyio/speechkit-server:v0.26.0` once the tag ships.
- See `docs/server/MIGRATION-v0.25-to-v0.26.md` for the upgrade path
  and central server onboarding guide.
- ModeSource defaults to "local" everywhere, so OSS users see no
  behaviour change after upgrade until they explicitly enable
  `[server_connection]`.

## [0.25.0] - 2026-04-26

### Added

- Same-provider Gemini Live fallback. `LiveConfig.FallbackModel` is honored
  by `GeminiLive.Connect` — when the primary `Model` returns a connect
  error, the kernel automatically retries with the fallback before
  surfacing the error. Decision logic isolated in `shouldTryFallback`
  for direct unit testing.

### Changed

- Default Voice Agent realtime model bumped to
  `gemini-3.1-flash-live-preview`. Google released the 3.1 Flash Live
  preview endpoint on April 15, 2026 and recommends it for new
  integrations. The previous `gemini-2.5-flash-native-audio-preview-12-2025`
  model is now the default `FallbackModel`, so deployments
  automatically degrade to the last GA Gemini Live model when the
  preview endpoint has transient issues.
- `cmd/speechkit` reference UI fallback string aligned with the
  Framework default; the device-target host now requests 3.1 Flash
  Live by default when no explicit model is configured.

### Notes

- v0.25 is a preparation release ahead of v0.26's first OSS Server-
  Target. Server-Target source (`cmd/speechkit-server`,
  `internal/server`, `deploy/`, `docs/server`) and the
  `release-server-docker.yml` workflow are intentionally excluded from
  the v0.25 OSS export and become public in v0.26.

## [0.24.0] - 2026-04-23

### Added

- Embeddable Dictation-only runtime under `pkg/speechkit/dictation` for host products that only need strict STT.
- Public Assist and Voice Agent service constructors under `pkg/speechkit/assist` and `pkg/speechkit/voiceagent`.
- `RuntimePolicy` for constraining enabled modes, allowed provider profiles, fixed profiles, fallback behavior, and Clean vs Intelligence mode behavior.
- Public framework modularity direction moved into `ROADMAP.md` and `docs/speechkit-framework-api.md`.

### Changed

- `pkg/speechkit` no longer exposes `internal/*` types through the public Dictation recording and segmenting contracts.
- The library example now uses the new Dictation runtime builder with a fixed provider profile policy.
- Framework API docs now document the embeddable mode constructors and policy-based provider/model constraints.

## [0.23.2] - 2026-04-23

### Added

- Mode-scoped public service contracts for Dictation, Assist, and Voice Agent in `pkg/speechkit`.
- Assist utility registry with explicit utility IDs, input requirements, capability gates, and surface defaults.
- Voice Agent session persistence with stored turns, transcript, summaries, and `/api/v1/voice-sessions`.
- Dictionary import/export API with usage counts at `/api/v1/dictionary`.

### Changed

- Voice Agent GoAway reconnects now surface a visible `recovering` state in the session FSM and prompter UI.
- Assist tool routing now uses the registry contract for exact codeword utilities before LLM routing.
- Assist and Voice Agent overlay feedback controls now live in their dedicated mode settings instead of the general appearance settings.

### Fixed

- Assist no longer routes direct replies to the SpeechKit-managed local LLM endpoint when no downloaded GGUF model is selected and present.
- Local LLM connection failures now surface actionable Assist/Voice Agent guidance instead of raw loopback `connectex` errors.
- Restored the missing Beads parent issues `SK-002`, `SK-003`, and `SK-004` so the local backlog can import again.

## [0.23.1] - 2026-04-21

### Added

- **Versioned provider artifacts API**: `/api/v1/providers/artifacts`, `/api/v1/providers/artifacts/jobs`, and `/api/v1/providers/artifacts/{artifactId}/download|select` now expose Local Built-in model downloads and provider pulls through the same control plane as provider activation.
- **Structured provider readiness**: `/api/v1/providers/readiness` now includes `schemaVersion`, active/default flags, execution metadata, structured requirements, setup actions, and local model artifacts for external integrations.
- **Local Built-in model artifacts**: Dictation exposes Whisper.cpp downloads, while Assist and Voice Agent expose GGUF model artifacts for SpeechKit's bundled OpenAI-compatible llama.cpp runtime.

### Changed

- **Framework-owned provider catalog**: `pkg/speechkit` now owns the reusable three-mode provider catalog, while the Windows runtime adapts it and appends host-only support profiles.
- **Modular API v1 control plane**: mode settings, provider readiness, and artifact actions are split out of route registration into focused backend modules.
- **Separated artifact status resolution**: downloadable artifact metadata is now static by default, with file, runtime, and Ollama availability resolved through an explicit status layer.
- **Local Built-in Voice Agent selection**: pipeline fallback can now use a concrete downloaded local GGUF model while still reporting the stable `realtime.builtin.pipeline` profile.

### Fixed

- **Local Built-in profile selection**: selecting downloaded Dictation, Assist, or Voice Agent artifacts now persists the matching per-mode `ModelSelection`, so Voice Agent no longer remains active on the Gemini default after choosing the local pipeline.

### Security

- **Resolved HTTP target validation**: provider, download, and update requests now validate the resolved network target before dialing, closing SSRF paths that could bypass URL-only validation.
- **Plaintext secret fallback disabled**: non-Windows builds no longer fall back to a plaintext file secret store; unsupported secret-store access now fails closed.
- **Control-plane and provider error hardening**: frontend API calls attach the local control-plane token consistently, and provider-facing errors are redacted before surfacing to UI or logs.

### Removed

- **Frozen local artifacts**: removed the obsolete `frontend/app-v2` scaffold, tracked coverage/audio/install artifacts, and old static HTML prototypes from the release surface.

## [0.23.0] - 2026-04-21

### Highlights

- **API-first mode framework**: Dictation, Assist, and Voice Agent now expose reusable v23 mode contracts, provider profiles, provider groups, readiness models, and per-mode settings for SDK and API consumers.
- **Local control plane**: External tools can configure providers, inspect readiness, patch mode settings, and start or stop the three modes through the versioned `/api/v1` API.
- **Open-source framework boundary**: Public-safe framework API documentation and export coverage prepare the v23 surface for the `kombifyio/SpeechKit` release repository.
- **Versioned API contract**: The local `/api/v1` control plane now ships with a public OpenAPI contract for external integrations.

### Added

- **V23 API-first framework boundary**: `pkg/speechkit` now exposes strict Dictation, Assist, and Voice Agent contracts, reusable provider profiles, provider groups, mode settings, readiness models, and profile validation for host applications.
- **Versioned local control API**: The desktop host now exposes `/api/v1/modes`, `/api/v1/modes/{mode}/settings`, `/api/v1/modes/{mode}/start|stop`, `/api/v1/providers/profiles`, `/api/v1/providers/readiness`, and `/api/v1/providers/{profileId}/activate` for external tool integration.
- **Mode command bus controls**: Runtime commands can now start and stop Dictation, Assist, and Voice Agent explicitly through `mode.start` and `mode.stop`.
- **Public framework API documentation**: `docs/speechkit-framework-api.md` documents the v23 SDK and local API boundary for the public OSS export.
- **OpenAPI contract**: `docs/api/openapi.v1.yaml` describes the `/api/v1` mode and provider control-plane endpoints.

### Changed

- **Provider catalog contract tests**: The public framework catalog now verifies that every main mode exposes the four V23 provider groups and that Dictation profiles remain text-only without LLM or tool-calling capabilities.
- **OSS export boundary**: The public export allowlist now includes the public v23 framework API documentation without exposing internal architecture notes.

## [0.22.4] - 2026-04-20

### Fixed

- **Compact overlay placement**: Small Feedback now defaults to the lower screen edge, sits closer to the edge, and keeps the pill, dot, and dot menu correctly centered.
- **Voice Agent Small Feedback**: Voice Agent state changes now drive the compact feedback overlay for listening, processing, speaking, and final summary states.
- **Compact panel clipping**: Assist and Voice Agent feedback panels now reserve native host space so the compact panel is fully visible instead of being cut off.

## [0.22.1] - 2026-04-20

### Highlights

- **Clearer mode separation**: Dictation, Assist, and Voice Agent now follow stricter runtime boundaries so each mode behaves more predictably.
- **Voice Agent reliability**: Voice Agent now has a compact live panel, speaker selection, bounded mic streaming, echo suppression, and more stable listening/processing/speaking transitions.
- **Assist result handling**: Assist now routes one-shot utilities, visible result panels, and unsupported-provider guidance through clearer result contracts.

### Added

- **Voice Agent speaker selection**: the live Voice Agent panel can now list and switch output devices, with the selected speaker persisted.
- **Local provider split**: Assist and utility LLM profiles now distinguish built-in local models from externally managed local providers such as Ollama.
- **Assist result metadata**: Assist now models result surface and result kind, making panel-vs-action routing explicit.

### Changed

- **Voice Agent panel UX**: the live panel is more compact, shows the latest user and agent turns, and keeps longer sessions more responsive.
- **Mode boundaries**: Dictation stays passthrough-only, Assist uses its own result surfaces, and Voice Agent no longer falls back into Assist/capture surfaces.
- **Local STT timeout headroom**: local whisper transcription now scales its processing timeout with captured audio duration.

### Fixed

- **Voice Agent streaming stability**: mic streaming, follow-up turns, echo handling, and turn completion are more robust.
- **Voice Agent panel behavior**: transcript folding, activity feedback, and close/stop flows are more stable.
- **Assist error guidance**: unsupported model/provider failures now show more actionable Assist panel feedback.
- **Ollama local-provider downloads**: downloaded Ollama models can now activate their matching Assist or utility profile.

## [0.21.1] - 2026-04-18

### Highlights

- **Slimmer Windows release**: The installer and portable bundle now ship the verified whisper.cpp runtime without prebundled model weights, cutting the Windows download size far below the old ~500 MB package while keeping local model downloads available inside the app

### Added

- **Layered Voice Agent prompts**: Voice Agent sessions now combine a durable `framework_prompt` with an optional user-level `refinement_prompt`, so product teams can define fixed behavior while individual users still sharpen tone and working style
- **Voice Agent prompt settings**: The desktop Settings UI now exposes both prompt layers directly on the Voice Agent tab, and the runtime persists them through the normal settings/config flow

### Changed

- **Prompt composition contract**: Gemini Live prompt assembly now treats the framework prompt as the primary instruction layer, appends the refinement prompt with explicit precedence guidance, and still merges vocabulary plus locale steering on top
- **Legacy compatibility**: `[voice_agent].instruction` now acts as a compatibility alias for `framework_prompt`, so older configs continue to load cleanly while new installs use the explicit layered fields

## [0.19.1] - 2026-04-15

### Fixed

- **Overlay clipping**: The pill panel host window now matches the actual tri-mode control width, so the hover bubble no longer clips on the right edge
- **Idle overlay centering**: Pill and dot idle states now anchor against the dedicated host window bounds instead of viewport-sized roots, eliminating the visible right-shift in compact mode
- **Dedicated overlay switching**: The Wails overlay runtime now uses shared host metrics and simplified show/hide routing so anchor, panel, radial menu, and legacy overlay windows stay consistent

### Tests

- **Overlay geometry regression coverage**: Added backend tests for dedicated host metrics, anchored positions, and active window switching plus frontend tests that pin the compact overlay roots to the actual host window size

## [0.19.0] - 2026-04-15

### Added

- **Tri-mode hotkeys**: Dictation, Assist, and Voice Agent now persist independent hotkeys, while `active_mode` also supports `none` as an explicit deactivated state
- **Per-monitor overlay memory**: The movable pill overlay now stores a free position per monitor and restores the saved position when the active display changes
- **Voice Agent HF fallback profile**: The Voice Agent model catalog now exposes a Hugging Face pipeline-fallback profile so HF-backed models can be selected from the Voice Agent tab as well

### Changed

- **Overlay controls**: Bubble hover and dot context menu now show one icon per configured mode, and clicking the active mode deactivates it back to `none`
- **Recording status badge**: The pill now shows the active mode icon on the right edge while recording, processing, or speaking
- **Settings contract**: `assist_hotkey` and `voice_agent_hotkey` are now the canonical settings fields; legacy `agent_hotkey` and `agent_mode` remain compatibility inputs only

### Fixed

- **Hugging Face token setup**: HF credential management is available again through the model-card settings flow, including Voice Agent fallback profiles
- **Settings hotkey UX**: The General settings page no longer exposes a second runtime mode selector, and the built-in defaults are aligned again to `Win+Alt`, `Ctrl+Shift+J`, and `Ctrl+Shift+K`

## [0.18.0] - 2026-04-14

### Highlights

- **Local onboarding**: First-run setup now lets users choose Whisper Small or Whisper Large v3 Turbo, continue while downloads run in the background, and jump straight into Transcribe token setup instead of getting stuck in the wizard.
- **Recommended local model**: Whisper Large v3 Turbo is now the recommended local Whisper.cpp model, while fresh local installs no longer depend on a prebundled Small model.
- **Release surface automation**: The website homepage now derives its latest version and release highlights directly from `CHANGELOG.md`, and website deploys also trigger when the changelog changes.

### Added

- **Model download onboarding**: First-run setup now exposes Small and Turbo local model choices, a persistent in-app download progress dock, and cloud-provider escape hatches for Hugging Face or OpenAI setup
- **Starter model selection**: Users can choose which local Whisper model SpeechKit should use after setup even before the download has completed
- **StreamPlayer**: New `audio.StreamPlayer` type with `streamPipe` (sync.Cond-based io.Reader) for continuous buffered audio output — replaces per-chunk `PlayPCM` goroutine spawning that caused choppy/broken playback
- **Prompter stop button**: Voice Agent prompter window now shows a stop button (visible when agent is active) that emits a `voiceagent:stop` Wails event to deactivate the session from the UI
- **Session error lifecycle**: `cleanupOnError()` method on `voiceagent.Session` handles idle timer, context cancellation, provider close, state transition to Inactive, and `OnSessionEnd` callback
- **OnSessionEnd callback**: New callback in `voiceagent.Callbacks` fires on unexpected session termination (receive errors, GoAway without reconnect) — distinct from manual `Stop()` which does not fire it
- **Nil message guard**: Receive loop now handles nil messages from the provider (prevents panic on closed channels)
- **Integration tests**: 13 new tests covering error cleanup, GoAway-without-reconnect, manual stop semantics, streamPipe I/O (write/read, blocking, close, draining, idempotent close), and controller toggle/mic wiring

### Fixed

- **Onboarding flow**: The local-model step stays usable on smaller windows via more compact model cards and a sticky action footer, so Continue and token-setup actions remain visible during downloads
- **Local model routing**: Switching between downloaded local Whisper models no longer falls through to Hugging Face or other cloud STT routes
- **Overlay centering**: The compact pill and dot overlay positioning is corrected on scaled Windows displays so the anchor no longer drifts off-screen
- **Mic ownership**: `audioCapturer` is now wired to the `desktopInputController` — voice agent actually receives mic audio frames instead of silently getting nothing
- **Audio playback**: Voice agent audio output uses StreamPlayer with continuous buffering instead of spawning a new `PlayPCM` goroutine per chunk (which called `Stop()` on each invocation, killing previous audio)
- **Barge-in handling**: `OnInterrupted` now drains and restarts the StreamPlayer instead of just calling `audioPlayer.Stop()`
- **Deactivation cleanup**: Toggling off the voice agent now clears the PCM handler (`SetPCMHandler(nil)`) before stopping the session, stops the StreamPlayer, and updates prompter state
- **Error state cleanup**: Receive errors and GoAway-without-reconnect now transition session to Inactive, fire OnSessionEnd, stop the stream player, and hide the prompter — previously they left the session in a stale state
- **Capture start**: Voice agent activation now calls `audioCapturer.Start()` to begin the capture session, not just setting the handler

## [0.17.0] - 2026-04-12

### Highlights

Complete UI overhaul of the desktop application — the Dashboard, Settings, Quick Note, and overlay surfaces have been redesigned with a Material Design 3 dark theme featuring a purple accent palette. Business logic has been extracted into reusable headless hooks, and a new public marketing site ships alongside the release.

### Added

- **Marketing site**: Cloudflare Pages site in `Website/` with release-aware download links, Getting Started guide, Architecture overview, and Integrations page
- **Headless hooks architecture**: Extracted all Dashboard, Settings, and Library business logic into reusable hooks (`useSettings`, `useDashboardStats`, `useLibrary`, `useSetupWizard`, `useToast`, `useLogs`, `useErrorPolling`) with full test coverage
- **Quick Note window**: Standalone floating editor with Save, Record (arms next hotkey for dictation), LLM Summary, and Email Draft actions — includes recording indicator, word counter, and draft auto-save
- **Pinned notes on Dashboard**: Dashboard now highlights up to 3 pinned notes in a dedicated card; unpinned notes sorted by recency
- **Overlay entry points**: Separate CSS-isolated entry points for each overlay surface (pill anchor, pill panel, dot anchor, dot radial, assist bubble, quick capture, quick note) with transparent backgrounds
- **Credential source visibility**: Settings now shows whether active credentials come from a user token, install token, or environment fallback
- **Provider credential UI**: Save/clear/test buttons for HuggingFace, OpenAI, Google, and Groq API keys directly in Settings
- **Stable download URLs**: Release artifacts use fixed filenames (`SpeechKit-Setup.exe`, `SpeechKit-Portable.zip`) without version suffixes, enabling permanent download links

### Changed

- **Dashboard redesign**: Clean KPI row (Total Recordings, Avg WPM, Total Words, Recorded Minutes), Latest Transcription card with provider badge, Pinned Notes card, conditional update banner, and Welcome/Quick Start empty state
- **Settings redesign**: Two-column General tab with organized sections (Mode, Hotkeys, Microphone, Overlay, Storage, Vocabulary), streamlined STT/Assist/Voice Agent tabs with inline model setup and credential management
- **Design system**: Material Design 3 dark theme with purple accent (#cabeff / #947dff), surface hierarchy (#131318 → #1f1f25 → #35343a), Segoe UI Variable / Geist Variable font stack, consistent 0.625rem radius, thin subtle scrollbars, and signature gradient buttons
- **Overlay style options**: Pill (default) or Circle (focus) styles, Default or Kombify design variants for pill mode, position selector (Top/Bottom/Left/Right), movable toggle with drag instructions
- **Hotkey options expanded**: Ctrl+Win and Ctrl+Shift+[D/J/K/Space] now available alongside Windows+Alt
- **Audio retention controls**: Configurable auto-deletion (1/7/30/90 days) and max storage limit (MB)
- **Vocabulary input**: Bias transcription with custom term corrections using `spoken => canonical` mappings
- **Asset filenames**: Windows installer and portable bundle no longer contain version suffixes — `SpeechKit-Setup.exe` and `SpeechKit-Portable.zip` are now stable across releases
- Rebuilt all embedded frontend assets shipped with the desktop binary
- Updated website copy to match the current local-first, provider-agnostic architecture

### Fixed

- Local whisper.cpp startup now verifies that the runtime binary and model file are actually present before retrying startup, surfacing broken installs earlier instead of looping on a bad state
- Whisper model downloads now verify SHA256 checksums before activation so corrupt downloads are rejected instead of silently persisting
- OSS publish workflow now strips all private-repo workflows from the export and preserves the public repo's own workflow files during sync
- Website `package-lock.json` regenerated for npm 11 compatibility (missing `@emnapi/core` and `@emnapi/runtime` peer dependencies)
- Vitest config separated from Vite config to prevent `tsc` build errors from test-only type imports

## [0.16.0] - 2026-04-11

### Fixed

- Local whisper.cpp server startup: `Transcribe()` now blocks and waits for the server to finish loading instead of returning "not ready" immediately — hotkey presses during the first ~60 seconds after launch no longer silently fail

### Changed

- Fresh local installs now default to whisper.cpp (local-only routing) with HuggingFace disabled — users get an offline, zero-config experience out of the box without requiring a cloud token
- Added regression tests covering the startup-wait behavior: all three paths (success, failed startup, context cancellation) are now verified in CI on Windows with the race detector

## [0.15.2] - 2026-04-11

### Changed

- Renamed the internal `ModalityAgent` modality to `ModalityAssist` across backend and frontend to match the three user-facing modes: Dictate, Assist, Voice Agent
- Replaced outdated catalog models: Qwen 2.5 7B/32B → Qwen 3.5 9B/27B, GPT-4o/GPT-4o mini → GPT-5.4/GPT-5.4 mini
- Removed "Utility" from user-visible model setup tabs — utility models remain internal but are no longer a selectable category in the UI
- Updated OpenAI provider defaults in config from gpt-4o-mini/gpt-4o to gpt-5.4-mini/gpt-5.4
- Frontend mode button and hotkey label renamed from "Agent" to "Assist"

## [0.15.1] - 2026-04-10

### Fixed

- Simplified model setup to a maximum of four visible options per mode, with direct inline API key entry or local download actions on each model card
- Removed stale Settings copy and dead Hugging Face credential helpers left behind by the provider UI redesign
- Improved Windows installer metadata so setup and uninstall surfaces present clearer product information during the interim unsigned release

### Changed

- Moved the public OSS release path to `kombifyio/SpeechKit`, with GitHub-hosted workflows and SignPath-ready Windows release wiring prepared for the next signed release
- Published the OSS code signing policy and public-repo operating model used for the upcoming SignPath Foundation onboarding

## [0.15.0] - 2026-04-10

### Added

- Production readiness audit and hardening for multi-user test rollout
- Malformed `config.toml` now falls back to sane defaults instead of crashing on startup
- UI log notifications when AI providers or TTS audio player fail to initialize (degradation visibility)
- Test coverage for malformed config recovery path

### Changed

- Centralized data/config path resolution via `internal/runtimepath/` in store packages (SQLite, Postgres)
- Audit phases 1–6: real model names in catalog, crypto/rand usage, error leakage guards, MaxBytesReader on HTTP handlers, AppVersion via ldflags, downloads extraction to `internal/downloads/`, saveSettings decomposition, randHex error handling, overlay_position validation, CI coverage alignment

## [0.14.9] - 2026-04-09

### Fixed

- Credential saves were silently failing — frontend was sending `secret` but the backend `saveProviderCredential` and `testProviderCredential` routes expect `credential`; corrected both URLSearchParams calls
- API Keys section in Settings → Providers was hidden when no keys were configured (filtered by `available: true`); changed to show all providers unconditionally
- TypeScript TS2538 error: `executionMode` is optional on `ModelProfile`; guarded before using it as a `PROVIDER_FOR_EXECUTION_MODE` index

### Changed

- Settings → Providers tab restructured: Models section now appears first, API Keys section below (was reversed)
- Each model profile row shows an inline amber cue ("API key required — configure below ↓") when the required provider key is missing
- Added **Test** button to each API key row to validate a key before saving

## [0.14.8] - 2026-04-09

### Added

- Generic provider credential UI — users can now save, clear, and test API keys per provider (HuggingFace, OpenAI, Google, Groq) directly in Settings → Providers

### Fixed

- Replaced GitHub App auth in the OSS publish workflow with a direct `OSS_PUBLISH_TOKEN` PAT to eliminate intermittent token issuance failures
- Removed the unused `providerCredentialProvider` helper that was blocking staticcheck
- Updated releaseguard test to reflect the new OSS publish auth mechanism

## [0.14.7] - 2026-04-09

### Fixed

- Restored the OSS publish auth fallback so cross-repo source mirroring and release creation keep working without mandatory GitHub App credentials
- Made OSS tag sync idempotent and exported the Windows runtime preparation scripts so public `kombifyio/SpeechKit` tags can build Windows release artifacts again

## [0.14.6] - 2026-04-09

### Fixed

- Switched the OSS mirror workflow to dedicated `OSS_PUBLISH_TOKEN` HTTPS auth via `GIT_ASKPASS`, avoiding the failing inline credential path on the CI runner
- Updated OpenTelemetry dependencies to `v1.40.0` so `govulncheck` no longer blocks CI on the current release line
- Removed the zero-duration timing assumption from the STT HTTP provider tests so Windows release builds no longer fail on fast local test servers

## [0.14.5] - 2026-04-09

### Fixed

- Switched the Windows build script to call `npm.cmd` directly so GitHub Actions no longer routes frontend steps through the broken PowerShell wrapper path
- Moved CI and release workflows to Go `1.25.9` and updated `github.com/go-git/go-git/v5` to `v5.17.1` to clear the current `govulncheck` failures
- Normalized the OSS publish token before git access and removed the stale duplicate release block that would have broken the release workflow after a successful build

## [0.14.4] - 2026-04-09

### Fixed

- Switched Windows build entry points to `pwsh` so CI and tag builds no longer fall back to Windows PowerShell 5.1
- Switched OSS mirroring to explicit git-over-HTTPS token auth instead of relying on the checkout action's failing cross-repo auth path
- Cleared the current CI blockers in Staticcheck and Android lint for the `main` release path

## [0.14.3] - 2026-04-09

### Fixed

- Replaced the strict-mode-unsafe PowerShell release build invocation so tagged Windows releases can build again
- Hardened the OSS publish flow to validate mirror token access before checkout and reuse the same token source across mirror checkout and release upload

### Changed

- Bumped release identifiers across desktop, Android, installer metadata, and frontend artifacts to 0.14.3

## [0.14.1] - 2026-04-03

### Fixed

- Made the release workflow build from the requested Git tag during manual dispatch so published installers match the tagged source
- Fixed the OSS publish workflow to use workspace-safe checkout paths, remove the legacy `.public-export-v8` gitlink blocker, and mirror installer assets into the public `kombifyio/SpeechKit` release

## [0.14.0] - 2026-03-31

### Added

- Self-contained Windows release packaging that bundles `whisper-server`, required runtime DLLs, and the `ggml-small.bin` local model for installer and portable distributions
- Changesets-based versioning workflow for future release PRs

### Changed

- Switched the canonical Windows install layout to `%LOCALAPPDATA%\\SpeechKit` so the installer, bundled local runtime, and default config paths resolve consistently
- Updated shipped defaults and first-run local install behavior to prefer the bundled local runtime with dynamic routing
- Bumped release identifiers across desktop, Android, installer metadata, and frontend artifacts to 0.14.0

### Fixed

- Hardened Android release readiness around assistant wiring, secure token storage, deep links, onboarding checks, and CI coverage
- Replaced placeholder quick-note summary and email actions in the Windows host with working backend handlers

## [0.1.3] - 2026-03-30

### Fixed

- Removed deprecated `oto` player Close call (staticcheck SA1019)
- Removed unused `hideAssistBubble` method (staticcheck U1000)

### Changed

- Bumped version identifiers across all platforms to 0.1.3

## [0.1.0] - 2026-03-30

First public release of SpeechKit as an open-source speech framework.

### Added

- **Framework core** (`pkg/speechkit/`): interface-driven orchestration for recording, transcription, and output delivery — usable as a standalone Go library
- **Three operating modes**: Dictation (STT only), Assist (STT + LLM + TTS), Voice Agent (real-time audio-to-audio)
- **Six STT providers**: local whisper.cpp, Hugging Face, OpenAI, Groq, Google Cloud Speech, self-hosted VPS
- **TTS providers**: OpenAI TTS, Google Cloud TTS, local Kokoro
- **LLM integration** via Firebase Genkit with multi-provider support (Gemini, OpenAI, Groq, Ollama, Hugging Face)
- **Voice Agent mode** with Gemini Live WebSocket for real-time audio conversations
- **Windows desktop host** (Wails v3) with push-to-talk dictation, overlay feedback, system tray, and global hotkeys
- **Audio capture** via WASAPI (malgo) with voice activity detection (Silero ONNX)
- **Settings UI** for provider, overlay, hotkey, and storage preferences
- **Local SQLite storage** for transcription history with optional PostgreSQL backend
- **Provider-agnostic credential model**: tokenless framework core, host-managed secret storage via Windows Credential Manager
- **Canonical Windows build** producing both portable bundle and NSIS installer
- **CI/CD pipeline** with GitHub Actions (frontend tests, Go analysis, Windows build, automated releases)
- **Library usage example** (`examples/library/`) demonstrating framework integration without UI
- **First-run onboarding wizard** with microphone selection, hotkey configuration, and quick start guide
- **Error toast notifications** surfacing provider errors as user-visible messages
- **Automatic update check** against GitHub Releases with in-app notification banner
- **Feedback links** in system tray menu and welcome tab (GitHub Issues, Discussions)
- **Privacy policy** covering audio processing, local storage, and cloud provider data flows
- **Android app** with custom keyboard (HeliBoard), voice assistant service, live dashboard stats, and library UI
- **Android release build** configuration with environment-based signing
- **OSS governance**: Apache-2.0 license, contribution guidelines, security policy, export boundary enforcement
