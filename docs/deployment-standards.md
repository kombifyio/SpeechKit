# SpeechKit Deployment Standards

Status: aligned to kombify Core runtime governance

SpeechKit follows the kombify platform split between development orchestration and release delivery:

- GitHub Actions is the canonical CI/build path for this repo.
- Windows desktop artifacts are produced as bundle and installer outputs.
- Aspire is not the production deployment engine for SpeechKit.
- Aspire is only relevant later for local or integration topologies around supporting services, for example a local VPS-like STT sidecar or GHCR-backed integration verification.
- Doppler-backed managed integrations are allowed for private development and internal kombify builds, but must not be baked into the public OSS release output.

Current repo contract:

1. `CI` validates the repo on Windows with the canonical bundle build path.
2. `Build` runs after successful `CI` on `main` or manually and produces:
   - `dist/windows/SpeechKit/`
   - `dist/windows/SpeechKit-Setup.exe`
3. Local development should use the same canonical script as CI:
   - `powershell -ExecutionPolicy Bypass -File scripts/build.ps1`

4. Public publication is allowlist-based:
   - `scripts/public/export-manifest.txt` defines what may enter the release repo
   - `AGENTS.md`, `CLAUDE.md`, personal notes, and runtime artifacts are excluded from the public surface

5. Release selection is surface-based, not all-or-nothing:
   - framework/source
   - Windows portable client
   - Windows installer
   - Android artifacts

This keeps SpeechKit aligned with the kombify rule from `kombify Core/internal-docs`:

- GHCR / deploy workflows are the release artifact path for deployed services.
- Aspire remains the development and integration control plane, not the primary deployment engine.

## Release Boundary

SpeechKit has to support two release modes:

1. Private kombify development and internal testing
2. Public OSS framework release

The boundary is:

- Core app code stays in this repo
- Managed kombify integration may rely on environment variables or Doppler at build and runtime
- Public OSS releases must not ship private repo assumptions, private project names, or managed secrets
- Public OSS releases must document required variables explicitly, not embed them

## Canonical Build Rules

- `scripts/build.ps1` is the canonical Windows bundle build
- GitHub Actions should call the same build path instead of maintaining a divergent release script
- The staged output is `dist/windows/SpeechKit/`
- The installer output is `dist/windows/SpeechKit-Setup.exe`
- The default build path produces both outputs from the same source tree
- `scripts/build.ps1 -SkipInstaller` is allowed for portable-only release flows, but it must still build the same staged Windows bundle

## Release Surfaces

- Shared source and framework releases are always tag-driven and may ship without Windows binaries.
- Windows portable and Windows installer are independent release attachments controlled per workflow run.
- Android is a separate release surface and must not be version-bumped implicitly by the shared source release flow.
- Release validation must match the selected surface, not a generic full-product checklist.

## Secret Handling

For private and internal kombify work:

- `HF_TOKEN` may be resolved from environment variables first
- Doppler is the approved fallback for local development and internal builds
- Managed defaults may enable Hugging Face in staged internal builds when `HF_TOKEN` is available

For public OSS releases:

- No secret value is committed
- No secret value is embedded in the binary
- No release artifact should depend on private Doppler project names to function
- Documentation must list expected variables, for example `HF_TOKEN` and `VPS_API_KEY`
- Public builds must not rely on implicit managed defaults

## Packaging Hygiene

- Bundle directories must use stable product naming, not temporary milestone names
- Release artifacts must not contain captured user audio or other incidental runtime data
- Installer and bundle paths must refer to `SpeechKit.exe`, not legacy binary names
- Public artifacts must be generated from the mirrored public tree, not from an unfiltered private worktree

## Runtime Expectations

- The app runs as a tray-resident Windows desktop tool
- Overlay is the primary runtime feedback surface
- Logs are written under `dist/windows/SpeechKit/logs/` in staged local bundles
- Settings and local data are treated as runtime state, not source artifacts
