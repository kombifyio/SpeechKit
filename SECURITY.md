# Security Policy

## Supported Scope

Security reports are welcome for:

- the desktop app runtime
- the Go framework and provider integrations
- installer packaging
- build and release scripts
- secret handling and credential resolution

## Reporting a Vulnerability

Do not open a public GitHub issue for a suspected vulnerability.

Use one of these private paths instead:

1. GitHub private vulnerability reporting on the release repository, when enabled.
2. Direct contact with the repository maintainers through the private maintainer channel used for SpeechKit.

Please include:

- affected version or commit
- impact summary
- reproduction steps or proof of concept
- any suggested mitigation

## Response Expectations

- We will acknowledge valid reports privately.
- We will confirm scope and severity before public disclosure.
- We will coordinate a fix and publish release notes once a patch is available.

## Disclosure Guidance

- Give maintainers time to validate and fix the issue before public disclosure.
- Avoid including live secrets, user data, or exploit material in public channels.

## Maintainer Security Gates

- Go dependency exposure is checked with `govulncheck`; repository and container surfaces are checked with OSV, Trivy, and TruffleHog in GitHub Actions.
- `gosec` intentionally excludes `G101` because SpeechKit stores environment variable names such as `OPENAI_API_KEY`, not credential values, in committed config defaults. It also excludes `G104` because unchecked Windows API return values are covered by `errcheck`/local review where those calls are meaningful.
- Windows DPAPI and Win32 `unsafe` usage in `internal/secrets`, `internal/voiceagent`, `internal/hotkey`, and `internal/output` is treated as audited native interop. Changes to those files should keep comments explaining why each unsafe call is required and should include Windows-specific tests when practical.

## Long-Lived Credentials & Rotation

SpeechKit's automation surface depends on one long-lived credential that
deserves explicit policy because Dependabot cannot rotate it: the
`WORKFLOWS_SYNC_PAT` repository secret used by
`.github/workflows/publish-oss.yml` to push the OSS export to
`github.com/kombifyio/SpeechKit`.

**Why it exists.** The release GitHub App's token (the rest of the
release path) has `Contents:write` but not `Workflows:write`. When the
publish-oss workflow touches files under the mirror's
`.github/workflows/`, GitHub rejects the push with
`refusing to allow a GitHub App to create or update workflow`. The
classic PAT carries `workflow` scope and is the documented escape
hatch for this exact case.

**Rotation cadence: every 90 days.** GitHub silently rejects pushes
once a classic PAT expires; the publish-oss run will fail with
`403 forbidden` and the OSS release will stall until the secret is
replaced. The rotation procedure:

1. Mint a new classic PAT under the maintainer's GitHub account.
   Scopes: only `workflow`. Expiration: 90 days from today.
2. Update the `WORKFLOWS_SYNC_PAT` secret in the SpeechKit
   repository settings (`Secrets and variables → Actions`).
3. Revoke the old PAT in the GitHub account's developer settings.
4. Trigger `publish-oss.yml` manually (dispatch) or wait for the
   next tag push and confirm the workflow's `Commit and push` step
   completes green.

**Migration plan (post-Sprint-6 follow-up).** The long-term fix is a
dedicated GitHub App that the release pipeline mints a short-lived
token from. Outline:

- Create a `kombify-speechkit-publisher` GitHub App with the minimum
  permissions: `Contents:write` and `Workflows:write`. Scope the
  installation to the `kombifyio/SpeechKit` repository only.
- Wire the publish-oss workflow to mint an installation token via
  `actions/create-github-app-token` (the same action `publish-oss.yml`
  already uses for the release App's token).
- Run one publish-oss with both credentials available, confirm the
  App-minted token succeeds for the workflows path, then remove
  `WORKFLOWS_SYNC_PAT` from repository secrets and revoke the PAT.

**Detection signal.** Any 90-day-old PAT is a hygiene finding; the
`workflows-sync-pat-rotation` bd label tracks the next due date. CI
itself flags a hard failure once the PAT expires (publish-oss fails
with `Bad credentials` / `403`).
