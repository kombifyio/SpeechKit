# Public Repo Operating Model

This document defines which repository owns which part of the SpeechKit release flow.

## Repositories

### Private Upstream

Repository: `Soulcreek/kombify-SpeechKit`

Purpose:

- daily development
- internal planning and maintainer-only files
- private experiments and staging work
- curated export into the public OSS tree

What stays here:

- `AGENTS.md`, `CLAUDE.md`, local notes, ad-hoc scratch files
- private remotes, internal credentials, and maintainer-only scripts
- local binaries, temporary exports, and workstation artifacts

### Public OSS Repository

Repository: `kombifyio/SpeechKit`

Purpose:

- public source of truth for open-source consumers
- public issues and pull requests
- public tags and GitHub releases
- GitHub-hosted CI for OSS release validation
- SignPath OSS code signing integration

What must live here:

- sanitized source exported from the private upstream
- public governance docs and release docs
- public workflows
- public tags and signed release artifacts

## Source Flow

The private upstream is not published directly.

The public repository is produced from the allowlist export flow:

- manifest: `scripts/public/export-manifest.txt`
- export script: `scripts/public/export-public.ps1`
- public surface validation: `scripts/public/check-public-surface.ps1`

Release source artifacts must be built from the exported public tree, not from the mixed private worktree.

## Workflow Ownership

### Private Upstream Workflows

Keep only workflows that are useful for private development and review.

Examples:

- internal CI
- private branch validation
- mirror/export automation if desired

Do not treat the private repo as the release origin for public Windows binaries.

### Public OSS Workflows

The following workflows belong in `kombifyio/SpeechKit`:

- public CI
- Windows build workflow
- release workflow
- SignPath signing workflow steps
- post-signing verification

This is required so that:

- public releases are reproducible from the public source tree
- SignPath OSS validation can use the public GitHub repository as the trusted build origin
- GitHub-hosted runners can be used for the release path

## Signing Ownership

Windows code signing belongs in the public repository release flow.

Expected sequence:

1. Build unsigned Windows artifacts in the public repository.
2. Upload the unsigned artifact with `actions/upload-artifact@v4`.
3. Submit the GitHub Actions artifact to SignPath from the public workflow.
4. Download the signed artifact back into the workflow.
5. Run `scripts/validate-windows-signing.ps1`.
6. Attach only the signed artifacts to the GitHub release.

## Tagging And Releases

Public tags must be created in `kombifyio/SpeechKit`.

Recommended model:

- private repo: development commits only
- public repo: exported commits intended for OSS publication
- release tags: only in the public repo
- GitHub Releases: only in the public repo

Do not cut a public release tag in the private upstream and then attempt to publish matching binaries elsewhere.

## Practical Decision

For SpeechKit, the clean target model is:

- private repo remains the internal upstream
- public repo becomes the only OSS release surface
- public repo owns GitHub-hosted release workflows
- public repo owns SignPath OSS integration
- signed Windows installer and portable bundle are published only from the public repo

