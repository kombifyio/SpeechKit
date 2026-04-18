# SpeechKit Docs

## Repo-Level

- [../README.md](../README.md) — Produkt-, Framework-, Build- und Runtime-Uebersicht.
- [../STATUS.md](../STATUS.md) — Ist-Zustand (Features, Production-Readiness, Tests).
- [../ROADMAP.md](../ROADMAP.md) — Offene Arbeit, deferred Sprints, Cloud-Integration-Roadmap.
- [../CHANGELOG.md](../CHANGELOG.md) — Keep-a-Changelog pro Release.

## Architektur

- [speechkit-architecture-v2.md](./speechkit-architecture-v2.md) — Drei-Modi-Framework (Dictation, Assist, Voice Agent).
- [speechkit-storage-architecture.md](./speechkit-storage-architecture.md) — Storage-Modell (SQLite-Default, Postgres BYODB, Cloud-Hybrid geplant).
- [voiceagent-session-fsm.md](./voiceagent-session-fsm.md) — Voice-Agent Session-FSM (States, Transitions, Error-Paths).

## Integration (teilweise implementiert)

- [speechkit-integration-plan.md](./speechkit-integration-plan.md) — DB + Auth + Install-Mode Integration (Cloud-Teil noch offen, siehe ROADMAP).
- [kombify-toolauth-integration.md](./kombify-toolauth-integration.md) — `toolauth` Integrations-Referenz (Import pending, siehe ROADMAP).

## Release & OSS-Betrieb

- [deployment-standards.md](./deployment-standards.md) — Kanonische Build-, CI-, Packaging- und Release-Regeln.
- [oss-release-boundary.md](./oss-release-boundary.md) — Was geht nach public, was bleibt privat.
- [oss-release-checklist.md](./oss-release-checklist.md) — Release-Gate vor Mirror/Tag.
- [release-matrix.md](./release-matrix.md) — Release-Surfaces, Workflow-Auswahl, Validierungs-Matrix.
- [public-repo-operating-model.md](./public-repo-operating-model.md) — Operating-Model fuer Release-Repo.

## GitHub App & Code-Signing

- [github-app-bootstrap.md](./github-app-bootstrap.md) — Bootstrap-Flow fuer Release-GitHub-App.
- [github-app-release-architecture.md](./github-app-release-architecture.md) — Cross-Org-Release-Architektur mit einer GitHub-App.
- [code-signing-policy.md](./code-signing-policy.md) — Signing-Policy fuer Public-Releases.
- [signpath-oss-setup.md](./signpath-oss-setup.md) — SignPath-Setup fuer OSS-Builds.
