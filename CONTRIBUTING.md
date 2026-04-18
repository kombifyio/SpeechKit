# Contributing to SpeechKit

SpeechKit is a Windows-first speech-to-text framework and desktop host. The repo is developed in a private upstream first and mirrored into a separate release repository through an explicit OSS release boundary. Contributions should keep that split intact.

## Before You Start

- Read [`README.md`](./README.md) for product scope and build prerequisites.
- Read [`docs/deployment-standards.md`](./docs/deployment-standards.md) for the canonical build and release contract.
- Read [`docs/oss-release-boundary.md`](./docs/oss-release-boundary.md) before changing build scripts, secrets, repo metadata, or release automation.

## Development Setup

1. Install Go `1.25+`.
2. Install Node.js `22+`.
3. Install MinGW-w64 and make sure `C:\msys64\mingw64\bin` is on `PATH` (the `vad` package uses CGo + ONNX Runtime).
4. Install NSIS if you want the canonical Windows build to also emit the installer.
5. Optional: install Doppler CLI for internal development flows. Doppler must never be required for OSS users.
6. Optional but recommended: install [lefthook](https://lefthook.dev/installation/) for local pre-commit / pre-push hooks (see *Local Git Hooks* below).

## Repository Layout

A quick orientation — details in [`docs/speechkit-architecture-v2.md`](./docs/speechkit-architecture-v2.md).

| Path | Purpose |
|---|---|
| `cmd/speechkit/` | Wails v3 desktop host (tray, overlay, settings UI wiring) |
| `pkg/speechkit/` | Public-facing framework surface |
| `internal/audio/` | WASAPI capture (malgo) + playback (oto) |
| `internal/vad/` | Silero ONNX voice-activity detection (requires CGo) |
| `internal/stt/` | STT providers (whisper.cpp, HuggingFace, OpenAI, Groq, Google, VPS) |
| `internal/tts/` | TTS providers (OpenAI, Google, ElevenLabs, HuggingFace) |
| `internal/ai/` | Genkit runtime + LLM flows (assist, agent, summarize) |
| `internal/voiceagent/` | Gemini Live real-time voice agent |
| `internal/assist/` | STT → codeword/LLM → TTS pipeline |
| `internal/netsec/` | SSRF-safe HTTP client and URL validation (use for every outbound provider call) |
| `internal/secrets/` | Token store (User > Install > Env hierarchy), DPAPI-wrapped on Windows |
| `frontend/app/` | Canonical React UI (wired into `scripts/build.*`, CI, release) |
| `frontend/app-v2/` | **Frozen** — see [`frontend/app-v2/FROZEN.md`](./frontend/app-v2/FROZEN.md) |
| `android/` | Companion app (separate lifecycle, own CI job) |
| `scripts/` | Build, release, public-export, runtime-prep scripts |
| `docs/` | Architecture docs, audit plan, release policy |

## Testing and Quality Gates

### Canonical Windows build (source of truth)

```powershell
powershell -ExecutionPolicy Bypass -File scripts/build.ps1
```

Produces and verifies:

- frontend tests + lint + production build
- `go vet ./...`
- `go test ./...`
- `dist/windows/SpeechKit/SpeechKit.exe`
- `dist/windows/SpeechKit-Setup.exe` (if NSIS is installed)

### Targeted Go workflows

```powershell
# Fast feedback loop
go test ./...

# Race-detector run (matches CI `Run go race tests`; excludes CGo-heavy vad)
go test -race ./cmd/... ./pkg/... ./internal/...

# Short mode — what lefthook pre-push runs
go test -short -timeout=60s ./...

# Coverage (drops coverage.out; same package set CI uploads to Codecov)
go test -coverprofile coverage.out -covermode atomic ./cmd/... ./pkg/... ./internal/...
go tool cover -func coverage.out | Select-String 'total:'
go tool cover -html coverage.out   # open browser report
```

### Build tags

- **default**: everything builds and tests on Windows with CGo.
- `!cgo`: compiles without ONNX Runtime. Used by cross-compile targets and by the `vad` stub tests (`internal/vad/silero_stub_test.go`) so signature drift between stub and real VAD is caught.
- `integration`: gates provider/E2E tests that hit real services (HuggingFace STT, OpenAI TTS, Gemini/Groq Genkit flows). Tests live in `*_integration_test.go` files and skip cleanly when their required credential env vars (`HF_TOKEN`, `OPENAI_API_KEY`, `GOOGLE_AI_API_KEY`, `GROQ_API_KEY`) are absent. Run with `go test -tags=integration ./...`.

### Linters and vulnerability checks

The Go CI job (`go-analysis`) runs these — run them locally before pushing:

```powershell
gofmt -s -l .                        # should print nothing
golangci-lint run --timeout=5m ./... # pinned to v1.64.8 in CI
staticcheck ./...
govulncheck ./...
```

### Frontend

```powershell
cd frontend/app
npm ci
npm test
npm run lint
npm run build
```

### Playwright E2E (browser smoke tests)

E2E tests run the Vite dev server and drive real Chromium against three frontend surfaces
(overlay, settings, quick-note) with all Go backend routes mocked via `page.route()`.

```powershell
cd frontend/app

# First-time only — download Chromium (~200 MB, stored in PLAYWRIGHT_BROWSERS_PATH)
npm run e2e:install

# Run all 17 E2E specs
npm run e2e

# Interactive UI mode (replay, trace, screenshots)
npm run e2e:ui
```

The Go process is **not** required — the helper in `e2e/helpers.ts` mocks every
`/overlay/state`, `/settings/state`, `/models/*`, and `/quicknotes/*` fetch.
E2E artifacts (`test-results/`, `playwright-report/`) are git-ignored.

## Local Git Hooks

Lefthook enforces the same gates as CI before code leaves your machine.

```bash
# one-time install
go install github.com/evilmartians/lefthook@latest
lefthook install
```

What runs when:

| Hook | Command | What it does |
|---|---|---|
| `pre-commit` | `gofmt` | rejects staged `.go` files that aren't `gofmt -s` clean |
| `pre-commit` | `golangci-lint` | `--fast --new-from-rev=HEAD~1` — only flags new findings |
| `pre-commit` | `eslint-frontend-app` | lints staged frontend files (max-warnings=0) |
| `pre-push` | `go-test-short` | `go test -short -timeout=60s ./...` |

Skip once (don't make a habit of it): `LEFTHOOK=0 git commit ...`
Run on demand: `lefthook run pre-commit`

## Pull Request Expectations

- Keep changes scoped. Separate feature work, refactors, and repo hygiene where possible.
- Add or update tests for behavior changes.
- Update docs when user-visible behavior, release steps, or contributor expectations change.
- Do not commit secrets, personal config, captured audio, or local runtime artifacts.
- Do not reintroduce private repo references, private Doppler defaults, or internal-only docs into the public surface.

## Issues and Feature Requests

- Use the issue templates for bugs and feature requests.
- Include reproduction steps, expected behavior, and platform details.
- For framework-facing changes, call out API or contract changes explicitly.

## Release Process (maintainers)

Releases cut from the public OSS mirror (`kombifyio/SpeechKit`). The private upstream does not publish user-facing binaries.

1. **Sync versions** across manifests:

   ```bash
   node scripts/sync-version.mjs <new-version>   # e.g. 0.20.0
   ```

2. **Regenerate changelog + release notes** from commit history:

   ```bash
   node scripts/release/changelog.mjs
   node scripts/release/render-release-notes.mjs
   ```

3. **Commit the version bump** on `main` and get explicit owner approval before pushing.

4. **Tag and push**:

   ```bash
   git tag v0.20.0
   git push origin v0.20.0
   ```

5. **CI does the rest**:
   - `.github/workflows/release.yml` triggers on `v*` tags against the OSS mirror.
   - Builds the portable Windows bundle (`.zip`) and NSIS installer (`.exe`).
   - Generates a CycloneDX SBOM (`SpeechKit.sbom.json`) via `cyclonedx-gomod app`.
   - Attaches SLSA provenance attestations (`attestations: write` + `id-token: write`) over assets + SBOM + `SHA256SUMS.txt`.
   - Uploads assets to the GitHub Release.

6. **Manual override**: the workflow also accepts `workflow_dispatch` with a tag and toggles for portable / installer artifacts — useful for hotfix reruns.

Authenticode signing of the Windows `.exe` is currently deferred pending the code-signing certificate (see [`docs/code-signing-policy.md`](./docs/code-signing-policy.md) and `AUDIT_PLAN.md` §4.1).

## Release Boundary Rules

- The OSS mirror/export path is allowlist-based — see [`docs/oss-release-boundary.md`](./docs/oss-release-boundary.md).
- Files such as `AGENTS.md`, `CLAUDE.md`, personal notes, internal planning scraps, and runtime binaries are not public artifacts.
- The framework core must stay provider-agnostic and tokenless. Host apps own credential injection and secret storage policy.

## Code Style

- Prefer small, testable units.
- Keep comments short and high-signal.
- Preserve the existing Wails v3, Go, and React patterns unless there is a clear reason to change them.
- Default to ASCII unless the file already uses non-ASCII for a real reason.

## Security

If you discover a vulnerability, do not open a public issue. Follow [`SECURITY.md`](./SECURITY.md).

**Outbound HTTP rule:** every provider integration must route through `internal/netsec` (`NewSafeHTTPClient` + `ValidateProviderURL`). The default zero-value `ValidationOptions` rejects loopback, `http://`, and private IP ranges — test servers opt in explicitly via `AllowLoopback: true, AllowHTTP: true`. Do not introduce a new `net/http.Client` elsewhere.

## Troubleshooting

| Symptom | Likely cause / fix |
|---|---|
| `cgo: C compiler "gcc" not found` | MinGW-w64 not on PATH. Add `C:\msys64\mingw64\bin`. |
| `onnxruntime.dll not found` at runtime | Missing VAD runtime. Run `scripts/prepare-whisper-runtime.ps1` or copy `onnxruntime.dll` next to the exe. |
| WebView2 missing on fresh Windows | Run `scripts/prepare-webview2-runtime.ps1`. |
| `go test ./internal/vad/...` fails without CGo | Expected — the CGo-backed tests need the ONNX DLL. The `!cgo` stub tests still run. |
| lefthook pre-commit rejects unrelated code | `golangci-lint` runs `--new-from-rev=HEAD~1`; rebase so only your own changes are diffed. |
| Frontend build passes locally, fails in CI | Ensure `frontend/app-v2/` is not re-activated — it is frozen. Check [`frontend/app-v2/FROZEN.md`](./frontend/app-v2/FROZEN.md). |
