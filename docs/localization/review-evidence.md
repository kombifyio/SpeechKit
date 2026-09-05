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
| en | catalogs/en.json | d76071a9a955eea4c1e21aefc26388c66884b3465a491861185a68aa7836475f | proposal | - | - | catalog changed 2026-09-04; re-review required |
| de | catalogs/de.json | 29d0351c5ba0674099b5f7235f28460ea7071582b96369e8d12acfa3c6aa558c | proposal | - | - | catalog changed 2026-09-04; re-review required |
| es | catalogs/es.json | 51e4a53814167ae8c7186e92cd0bc0acb2f13a3ddbab4653065e6cec6beed55d | proposal | - | - | catalog changed 2026-09-04; re-review required |
| zh-Hans | catalogs/zh-Hans.json | a08ca4672614629b62bc44f7f77fdbb733147ba60623df659d9b1cacd6b78646 | proposal | - | - | catalog changed 2026-09-04; re-review required |
| hi | catalogs/hi.json | 0fe15d4560231bee96c224a4915aa70de3130c4c507e94d9330d1a70b5eedc2a | proposal | - | - | catalog changed 2026-09-04; re-review required |
| ar | catalogs/ar.json | 08f4b517c1d6af6d0512df4b898bbe4390d0e994d00a4a3f7b444ce4dd7ad497 | proposal | - | - | catalog changed 2026-09-04; re-review required |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is tracked as child beads of the localization
audit (v0.68): the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
