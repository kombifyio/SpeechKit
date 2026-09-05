---
title: Localization Review Evidence
last_verified: 2026-09-03
status: active
---

# Localization Review Evidence

SpeechKit ships six message catalogs in `pkg/speechkit/localization/catalogs`
(`en`, `de`, `es`, `zh-Hans`, `hi`, `ar`). This file is the release evidence
the localization standard asks for: who reviewed which catalog, when, and the
exact content that review covered. A catalog cannot change without its row
changing with it.

## Rules

- `sha256` is the SHA-256 of the catalog file with line endings normalized to
  LF, so Windows and Linux checkouts agree. `go test ./pkg/speechkit/localization/`
  fails when a catalog's digest differs from its row.
- `review_state` is `human-reviewed` (a named person read every string in the
  catalog in this exact version) or `proposal` (machine translation or an
  unreviewed edit). A `human-reviewed` row needs `reviewer` and `reviewed_on`;
  a `proposal` row needs `notes` saying what is outstanding.
- Changing a catalog resets its row to `proposal`: run
  `mise run localization:evidence:write` (or `go run ./tools/localizationevidence -write`)
  to refresh the digest, then have the locale reviewed and upgrade the row by
  hand. `mise run localization:evidence` only checks and prints the rows that
  need attention.
- The English catalog is the source locale; its review is the authoring review.

## Catalogs

| locale | catalog | sha256 | review_state | reviewer | reviewed_on | notes |
| --- | --- | --- | --- | --- | --- | --- |
| en | catalogs/en.json | 3b3b2ce0eedc3c568b3829e5603cb1dd5cdcc9efa125165c14a0a6dd8e15bfb5 | proposal | - | - | catalog changed 2026-09-04; re-review required |
| de | catalogs/de.json | 16048c0dc1e1a57ee6a4fe43798d31af75186dddcd03de2143e9c89fb4abd1a7 | proposal | - | - | catalog changed 2026-09-04; re-review required |
| es | catalogs/es.json | 6d82339a1463f3391d484aa894df5bc6d2e5817ddb92a3f97edb898d671435a4 | proposal | - | - | catalog changed 2026-09-04; re-review required |
| zh-Hans | catalogs/zh-Hans.json | 0625a0fa500fd634683a8f5b242a7f58cb210818cb9fb6f015249d38014cd508 | proposal | - | - | catalog changed 2026-09-04; re-review required |
| hi | catalogs/hi.json | 9cb3e52956f131bde8db70166e8c0d28d0315da934fca9b4e428134469d2e5bc | proposal | - | - | catalog changed 2026-09-04; re-review required |
| ar | catalogs/ar.json | 6b0ff0f9692f3a64062a9c65ee2dcb4200dc72cb4f0381991a42871275d494a8 | proposal | - | - | catalog changed 2026-09-04; re-review required |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is tracked as child beads of the localization
audit (v0.68): the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
