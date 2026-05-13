# Changelog

All notable changes to SpeechKit should be documented in this file.

The format is based on Keep a Changelog and this project is intended to ship under Apache-2.0.

## [Unreleased]

## [0.32.2] - 2026-05-13

v0.32.2 closes the runtime-stability findings from Beads SK-004.5.10:
the duplicate-text-injection symptom reported in live testing is
root-caused (Wails v3 alpha's `SingleInstance` activation fires inside
`app.Run()`, which is the last bootstrap stage — by then a racing
second instance has already registered global Windows hotkeys via
`RegisterHotKey`). The fix is belt-and-suspenders: claim a session-
scoped Windows named mutex in `runDesktopApp` before any global
resource is acquired. The release also adds structured startup /
window / tray telemetry so future field reports of the same class of
bug can be diagnosed from log alone, and a `defer recoverHook` guard
around every Wails event-loop callback so a panic in one hook no
longer crashes the entire desktop event loop. No public API change.

### Fixed

- **Duplicate text injection eliminated.** A new
  `Local\com.kombify.speechkit.singleton-belt` Windows named mutex is
  acquired as the first action in `runDesktopApp`. If the mutex is
  already held, the second instance logs
  `desktop.singleton.duplicate_instance` and exits cleanly before any
  hotkey is registered, audio capturer is opened, or Wails event loop
  is started. Wails' own `SingleInstanceOptions` remains as a
  redundant second layer.
- **Wails event-loop callback panics no longer crash the desktop
  client.** A `defer recoverHook(name)` is installed in every
  registered callback (`WindowClosing` on prompter and dashboard,
  `WindowDidMove` on the pill panel, `ApplicationStarted`,
  `voiceagent:start` / `:stop` / `:close`). A panic now lands in
  `slog.Error("desktop.hook.panic", ...)` with hook name and full
  stack trace, and the remaining hooks keep working.

### Added

- **Structured startup / window / tray telemetry.** A new
  `startupTracker` emits ten `desktop.startup` log events with stage
  name, `elapsed_ms`, and `delta_ms` across the full `runDesktopApp`
  bootstrap (`entered`, `singleton_acquired`, `config_loaded`,
  `state_init`, `router_runtime`, `audio_runtime`, `runtime_services`,
  `wails_app_created`, `windows_configured`, `hotkeys_started`,
  `input_runtime_ready`, `app_run_begin`, `app_run_returned`). Tray
  click events (`tray.action`), tray-driven dashboard / quit requests
  (`desktop.tray.dashboard_requested`, `desktop.tray.quit_requested`),
  and window lifecycle (`desktop.window.closing`, `.shown`) now log
  structured fields so close-reason and visibility transitions are
  reconstructable from the log file.

### Notes

- The 2026-05-13 audit Phase C2 PR 3 (`internal/quicknote`) was
  attempted in the same session but deferred: both
  `quicknote_desktop_service.go` and `quick_actions.go` are
  significantly more entangled with `*appState` than the
  decomposition plan estimated. Recorded as a follow-up in Beads
  SK-004.6.5; no behaviour or release impact in v0.32.2.

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



v0.32.0 lands the 2026-05-13 senior-architect audit-driven hardening pass:
Phase A and B from the improvement plan, the first slices of Phase C
(public-API stability gate, voice-agent runtime extraction to a stable
public path, and the start of the `cmd/speechkit/` decomposition), plus a
CI fix for Frontend Checks that had been red on main since v0.31.1. No
public API change — `pkg/speechkit/**` remains backward-compatible and is
now enforced by an automated apidiff gate. Internal package boundaries
became significantly clearer.

### Highlights

- **Public API stability gate**: every PR is now automatically gated on
  incompatible changes to `pkg/speechkit/**` via a new `apidiff` CI job.
  Downstream embedders can rely on the stability contract.
- **Voice Agent runtime moved into `pkg/speechkit/voiceagent/live`**:
  `Session`, `IdleTimer`, and the realtime workflow primitives are now
  importable from a stable public path. The internal mirror at
  `internal/voiceagent` keeps alias-bridge declarations to avoid churn
  for in-tree consumers.
- **`cmd/speechkit` decomposition started**: two new internal subpackages
  carved out of the 128-file `package main` god-package —
  `cmd/speechkit/internal/transcription` (vocabulary + STT model hints)
  and `cmd/speechkit/internal/profiles` (model-profile selection +
  local-provider validation). Mechanical moves, no behaviour change.
- **Audit-driven dead-code purge**: 27 dead UI files (3,632 LOC), 4 unused
  npm deps, 2 orphan large assets, and the orphan `frontend/app/go.mod`
  stub removed. Strict knip dead-code gate now blocks regressions.
- **Server hardening**: three poisoned-bucket panic sites in the rate
  limiter replaced with `slog`-based recovery on the HTTP hot path.

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
