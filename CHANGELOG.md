# Changelog

All notable changes to SpeechKit should be documented in this file.

The format is based on Keep a Changelog and this project is intended
to ship under Apache-2.0. Entries are the public-facing summary that
lands on the GitHub Release page — write them for end users, not for
maintainers. See [`docs/changelog-style.md`](docs/changelog-style.md)
for the style guide and template. The linter
(`npm run release:lint -- --version vX.Y.Z`) refuses internal tracker
IDs, source paths, and other maintainer-only vocabulary.

## [Unreleased]

## [0.38.10] - 2026-05-25

Windows local onboarding hotfix. No public API change.

### Fixed

- **Local setup now starts a speech-model download automatically.**
  Choosing Local starts the smallest Dictation-ready model by default,
  and choosing a larger model starts that download immediately in the
  background.
- **Duplicate model-download clicks no longer start parallel downloads
  for the same model.** SpeechKit reuses an in-progress download job so
  fresh setup stays predictable.

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
[docs/release-notes/v0.37.0.md](docs/release-notes/v0.37.0.md).

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
[docs/release-notes/v0.36.0.md](docs/release-notes/v0.36.0.md).

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
  [docs/server/local-only-guarantee.md](docs/server/local-only-guarantee.md).
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
  [`kombify-SpeechKit-x3w`](.beads/issues.jsonl) is the next step.

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
- `docs/cmd-speechkit/DECOMPOSE-PLAN.md` — self-contained 11-PR
  decomposition roadmap for `cmd/speechkit/`. Resequenced 2026-05-13 to
  put structural-leaf packages first and `internal/state` last after
  discovering that `appState` has 65+ methods spread across 12 files.
- `docs/audits/2026-05-13/improvement-plan.md` — the in-repo copy of
  the senior-architect audit plan that drove this release line.
- `docs/mcp/SPLIT-PLAN.md` — five-PR decomposition roadmap for the
  700-LOC `cmd/speechkit-mcp/main.go`. Execution is tracked as
  SK-004.6.6.
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
