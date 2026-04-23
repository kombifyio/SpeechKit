# SignPath OSS Setup

This document is the optional operating checklist for enabling SignPath Foundation signing in the public SpeechKit repository.

SignPath is not required for the current no-cost public release path. When SignPath is unavailable, SpeechKit publishes unsigned Windows artifacts with `UNSIGNED-WINDOWS-RELEASE.txt`, `SHA256SUMS.txt`, `SpeechKit.sbom.json`, and GitHub provenance when enabled.

## What Must Be True Before Applying

- the public repository exists at `kombifyio/SpeechKit`
- the repository is public
- the project license is OSS-compatible
- the public repository contains a published code signing policy
- public releases are intended to come from the public repository, not the private upstream

For SpeechKit, those conditions are satisfied once the exported public repository contains:

- [`README.md`](../README.md)
- [`docs/code-signing-policy.md`](./code-signing-policy.md)
- public GitHub workflows with GitHub-hosted runners

## What To Enter In The SignPath Foundation Apply Form

Use these values as the base:

- Project name: `SpeechKit`
- Project website / home page: `https://github.com/kombifyio/SpeechKit`
- Source repository: `https://github.com/kombifyio/SpeechKit`
- License: `Apache-2.0`
- Primary maintainer: `Soulcreek`
- Code signing policy URL: `https://github.com/kombifyio/SpeechKit/blob/main/docs/code-signing-policy.md`

## Public Repository Configuration After Approval

Configure these in `kombifyio/SpeechKit`:

### Repository Secret

- `SIGNPATH_API_TOKEN`

### Repository Variables

- `SIGNPATH_ORGANIZATION_ID`
- `SIGNPATH_PROJECT_SLUG`
- `SIGNPATH_SIGNING_POLICY_SLUG`
- `SIGNPATH_ARTIFACT_CONFIGURATION_SLUG`
- `SIGNPATH_PUBLISHER_NAME`

## Workflow Behavior When SignPath Is Configured

The public release workflow is prepared to:

1. build Windows artifacts on GitHub-hosted runners
2. upload unsigned `SpeechKit.exe`
3. submit it to SignPath
4. upload unsigned `SpeechKit-Setup.exe`
5. submit it to SignPath
6. replace the unsigned binaries with the signed outputs
7. verify signatures with `scripts/validate-windows-signing.ps1`
8. publish the signed artifacts to the GitHub Release

When SignPath configuration is incomplete, the same release workflow skips signing and publishes the documented unsigned asset set instead.

## Runner Requirement

For the SignPath GitHub trusted-build flow used by OSS projects, the public repository must run the pre-signing jobs on GitHub-hosted runners.

This is why the public release path defaults to:

- `ubuntu-24.04`
- `windows-2025`

Private upstream automation may still use different runners, but public release signing must happen in the public repository workflow.

## Recommended Activation Sequence

1. export the current source into `kombifyio/SpeechKit`
2. verify the public repo contains the updated workflows and docs
3. submit the SignPath Foundation application if/when signing becomes available
4. wait for approval and project setup details
5. add the SignPath secret and repository variables to `kombifyio/SpeechKit`
6. set `REQUIRE_SIGNED_WINDOWS_RELEASES=true` only after signed releases are mandatory
7. trigger a public tag-based release

## When The Apply Form Is Ready

The apply form is ready once the public repository has:

- the updated public workflows
- the published code signing policy
- the repository visible to the public

The SignPath secret and project slugs are not required before submitting the application; they are added after approval.
