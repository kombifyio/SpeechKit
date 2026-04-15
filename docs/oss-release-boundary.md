# SpeechKit OSS Release Boundary

SpeechKit is developed in a private upstream and mirrored into a separate release repository. Public release content must be derived from an explicit allowlist, not from ad-hoc copying.

## What Ships in the OSS Release

- framework and desktop host source under `cmd/`, `internal/`, `pkg/`, `frontend/`, `assets/`, and `installer/`
- public-facing documentation under `README.md`, `docs/`, and governance files
- canonical build scripts under `scripts/`
- release metadata and workflow inputs under `package.json`, `package-lock.json`, `.changeset/`, and `scripts/sync-version.mjs`
- the curated public GitHub workflow set under `.github/workflows/{build,changesets,ci,release}.yml`
- example configuration under `config.example.toml`

## Intentional Extension Seams

Some `internal/` packages are intentionally present even in the OSS release because they are the abstraction boundary for optional private modules:

- `internal/kombify` is only a build-tag seam. In OSS builds it compiles to a no-op package.
- `internal/auth` exposes the auth provider registry that a private kombify module may register into.
- `internal/store` exposes backend registration so private or future public backends can plug in without forking the core.
- `internal/features` only reports which optional capabilities are available at runtime.

These packages are not evidence that private kombify runtime code is being shipped.

## What Does Not Ship

- private maintainer instructions such as `AGENTS.md` and `CLAUDE.md`
- personal planning scraps, local notes, and backups
- private or transient plan files that are not curated for the public release repo
- private repo URLs, private org names, or internal-only project names
- embedded secrets, release-time credentials, or private Doppler defaults
- generated runtime state such as captured audio, logs, coverage files, and local `.exe` scratch builds outside the staged release path

## Secret Boundary

- the framework core stays tokenless
- provider credentials are injected by the host app or caller
- public artifacts must not depend on private Doppler project names to function
- internal development may still use Doppler, but only through explicit runtime or build-time opt-in

## Mirror Contract

- use the allowlist in [`scripts/public/export-manifest.txt`](../scripts/public/export-manifest.txt)
- export only the curated public workflow subset; never copy private-only workflows such as website deploy or mirror automation into the OSS repo
- sanitize docs and metadata before mirroring
- build the public release artifacts from the mirrored public tree, not from a mixed private worktree
- public tags may be source-only or source plus selected Windows artifacts
- Android artifact publication is a separate release surface and must not be implied by an OSS mirror sync
