# Security Policy

## Supported Scope

Security reports are welcome for:

- the Windows client runtime and release assets
- the Go framework and provider integrations
- the self-host server, CLI, and MCP tools
- installer packaging and release metadata
- secret handling and credential resolution

## Reporting a Vulnerability

Do not open a public GitHub issue for a suspected vulnerability.

Use GitHub private vulnerability reporting on this repository when available.
If that is unavailable, contact the maintainers through the official channels
listed on the repository or organization profile.

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

## Release Integrity

Download Windows assets only from the official
[kombifyio/SpeechKit releases](https://github.com/kombifyio/SpeechKit/releases).
Each release publishes checksums and, while the unsigned release path is active,
an `UNSIGNED-WINDOWS-RELEASE.txt` notice. Verify SHA-256 checksums before
installing.
