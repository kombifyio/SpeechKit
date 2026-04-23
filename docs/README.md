# SpeechKit Docs

## Repo-Level

- [../README.md](../README.md) — Produkt-, Framework-, Build- und Runtime-Uebersicht.
- [../CHANGELOG.md](../CHANGELOG.md) — Keep-a-Changelog pro Release.

## Architektur

- [speechkit-framework-api.md](./speechkit-framework-api.md) — V23 API-first Framework Boundary fuer SDK und lokale Control API.
- [api/openapi.v1.yaml](./api/openapi.v1.yaml) — OpenAPI-Vertrag fuer die lokale `/api/v1` Control Plane.
- [speechkit-storage-architecture.md](./speechkit-storage-architecture.md) — Storage-Modell (SQLite-Default, Postgres BYODB, Cloud-Hybrid geplant).

## Release & OSS-Betrieb

- [deployment-standards.md](./deployment-standards.md) — Kanonische Build-, CI-, Packaging- und Release-Regeln.
- [oss-release-boundary.md](./oss-release-boundary.md) — Was geht nach public, was bleibt privat.
- [oss-release-checklist.md](./oss-release-checklist.md) — Release-Gate vor Mirror/Tag.
- [public-repo-operating-model.md](./public-repo-operating-model.md) — Operating-Model fuer Release-Repo.

## Windows Artifact Trust

- [code-signing-policy.md](./code-signing-policy.md) — Trust-Policy fuer Public Windows Releases.
- [signpath-oss-setup.md](./signpath-oss-setup.md) — Optionales SignPath-Setup fuer OSS-Builds.

## Runbooks

- [runbooks/README.md](./runbooks/README.md) — Incident playbooks (model download, STT outage, VAD init, Voice Agent reconnect).
