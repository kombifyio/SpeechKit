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
