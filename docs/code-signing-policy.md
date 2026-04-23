# Windows Artifact Trust Policy

This policy applies to public Windows release artifacts for SpeechKit published from `kombifyio/SpeechKit`.

## Scope

The policy covers:

- `SpeechKit.exe`
- `SpeechKit-Setup.exe`
- `SpeechKit-Portable.zip`
- `SHA256SUMS.txt`
- `SpeechKit.sbom.json`
- `UNSIGNED-WINDOWS-RELEASE.txt` when the release is unsigned

## Release Origin

Public Windows artifacts must originate from:

- repository: `https://github.com/kombifyio/SpeechKit`
- release tags in the public repository
- GitHub-hosted runners in the public repository release workflow

The private upstream repository may prepare and export source, but it is not a public Windows binary origin.

## Signing Policy

SpeechKit signs Windows artifacts when a trusted no-cost signing path is configured for the public repository.

If SignPath or another trusted free signing provider is unavailable, SpeechKit may publish unsigned Windows artifacts only through the documented no-cost path:

- the build runs in `kombifyio/SpeechKit`
- the release includes `UNSIGNED-WINDOWS-RELEASE.txt`
- the release includes `SHA256SUMS.txt`
- the release includes `SpeechKit.sbom.json`
- GitHub build provenance is attached unless `ENABLE_BUILD_ATTESTATIONS=false` is set intentionally
- the release remains a draft until the private publisher verifies the public workflow and asset set

Unsigned Windows artifacts are expected to trigger Windows SmartScreen or installer trust warnings. That is acceptable only when the unsigned notice and verification artifacts are attached.

## Required Verification

Signed releases must pass:

```powershell
pwsh ./scripts/validate-windows-signing.ps1 -RequireInstaller -RequireTimestamp -ExpectedPublisher "<publisher>"
```

Unsigned releases must verify:

- the installer and portable bundle are built by the public release workflow
- `UNSIGNED-WINDOWS-RELEASE.txt` is attached
- `SHA256SUMS.txt` contains every release asset hash
- `SpeechKit.sbom.json` is attached
- provenance attestations exist unless disabled intentionally

## Roles

Current project roles:

- Committer and maintainer: `@Soulcreek`
- Release approver: `@Soulcreek`
- Signing approval authority, when signing is available: `@Soulcreek`

Additional maintainers may be added later through the public repository permission model and corresponding signing provider roles.

## Secrets

- Private signing keys are not stored in this repository.
- Signing credentials are managed by the public repository's signing provider configuration.
- No contributor may commit raw certificate files, exported private keys, or signing secrets to source control.

## Incident Handling

If a suspicious artifact, bad signature, wrong hash, or missing provenance is detected:

1. stop the release
2. keep or return the GitHub Release to draft
3. investigate the affected workflow run and artifact lineage
4. rebuild from the public tag
5. publish corrected artifacts only after the issue is understood

## Privacy

SpeechKit release trust uses repository metadata, release tags, workflow run context, and artifact metadata.

No end-user audio or runtime data is intentionally submitted for signing, hashing, SBOM generation, or provenance.
