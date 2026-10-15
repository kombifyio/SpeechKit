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
| en | catalogs/en.json | 2ec751d77139345031b32d56e4826077dfca8d468afba50e3f59cd2b5dc04c79 | proposal | - | - | catalog changed 2026-10-14; re-review required |
| de | catalogs/de.json | d597385bbebb6b83977439c5e43a88e4ca628349a7686a443a1b1193c1e8375d | proposal | - | - | catalog changed 2026-10-14; re-review required |
| es | catalogs/es.json | 047d47c643ce922f07c5694346fab05891ff2127fc277bbce28e199ba8f613b7 | proposal | - | - | catalog changed 2026-10-14; re-review required |
| zh-Hans | catalogs/zh-Hans.json | 1a5f56560baea93d8f49d75ebff9775304773d880e9d076a6124cce18059785d | proposal | - | - | catalog changed 2026-10-14; re-review required |
| hi | catalogs/hi.json | a70da8762683efb80d3f49ad9f00a8192e7a095773cb353a356e9d6e96f5eb9a | proposal | - | - | catalog changed 2026-10-14; re-review required |
| ar | catalogs/ar.json | 6eb618785a7a255bd3af350135da5010947c08b163f4c0ea7a562bc15f5c6eb1 | proposal | - | - | catalog changed 2026-10-14; re-review required |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is known: the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
