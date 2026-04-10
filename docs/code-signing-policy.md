# Code Signing Policy

This policy applies to public Windows release artifacts for SpeechKit published from `kombifyio/SpeechKit`.

## Scope

The following public Windows artifacts must be Authenticode-signed before publication:

- `SpeechKit.exe`
- `SpeechKit-Setup.exe`

Portable archives may only contain signed Windows binaries.

Unsigned Windows binaries must not be attached to public GitHub Releases.

## Signing Service

SpeechKit uses the SignPath Foundation service for open-source code signing once the project is approved.

Public Windows releases are expected to be built in GitHub Actions from the public repository and submitted to SignPath from that trusted build.

## Build Origin

Public signed binaries must originate from:

- repository: `https://github.com/kombifyio/SpeechKit`
- release tags in the public repository
- GitHub-hosted runners in the public repository release workflow

The private upstream repository is not a public release origin.

## Roles

Current project roles:

- Committer and maintainer: `@Soulcreek`
- Release approver: `@Soulcreek`
- SignPath approval authority: `@Soulcreek`

Additional maintainers may be added later through the public repository permission model and corresponding SignPath project roles.

## Approval Rules

- Signing requests for public releases require explicit approval in SignPath.
- Only maintainers assigned as release approvers may approve a signing request.
- The signed output must match the public tag and the public release workflow run that produced it.

## Verification

Before public publication, Windows artifacts must pass:

```powershell
pwsh ./scripts/validate-windows-signing.ps1 -RequireInstaller -RequireTimestamp -ExpectedPublisher "<publisher>"
```

At minimum, verification must confirm:

- valid Authenticode signature
- trusted timestamp
- expected publisher name

## Private Keys And Secrets

- Private signing keys are not stored in this repository.
- Signing credentials are managed in SignPath and public-repo secrets/configuration.
- No contributor may commit raw certificate files, exported private keys, or signing secrets to source control.

## Incident Handling

If an incorrect or suspicious signature is detected:

1. stop the release
2. revoke or disable the signing configuration if necessary
3. investigate the affected workflow run and artifact lineage
4. publish corrected signed binaries only after the issue is understood

## Privacy

SpeechKit release signing uses repository metadata, release tags, workflow run context, and artifact metadata required for trusted-build signing.

No end-user audio or runtime data is intentionally submitted for code signing.

