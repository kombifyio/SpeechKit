# SpeechKit OSS Release Boundary

SpeechKit is developed in a private upstream and mirrored into a separate release repository. Public release content must be derived from an explicit allowlist, not from ad-hoc copying.

## What Ships in the OSS Release

- framework and desktop host source under `cmd/`, `internal/`, `pkg/`, `frontend/`, `assets/`, and `installer/`
- public-facing documentation under `README.md`, `docs/`, and governance files
- canonical build scripts under `scripts/`
- GitHub workflows and repo templates under `.github/`
- example configuration under `config.example.toml`

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
- sanitize docs and metadata before mirroring
- build the public release artifacts from the mirrored public tree, not from a mixed private worktree
