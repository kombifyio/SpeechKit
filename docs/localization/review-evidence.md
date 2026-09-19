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
| en | catalogs/en.json | f223af79d1d0c6e5e55ae3c65125422a857ef22c69eb9f4f83f9382679d3aad0 | proposal | - | - | catalog changed 2026-09-17; re-review required |
| de | catalogs/de.json | 2d596a9bf8ec7570fb36cac8ea62ab031016ad1b558c5f9eaa390748602204a5 | proposal | - | - | catalog changed 2026-09-17; re-review required |
| es | catalogs/es.json | 18f1c81143de9a3244fe87a282138af04e12dbb1df68643ca9cb1a372115ed83 | proposal | - | - | catalog changed 2026-09-17; re-review required |
| zh-Hans | catalogs/zh-Hans.json | 72141c27bd6fc41b3b89926f728b1716b9ec5d73390d155ff3a64a7d3a17c2db | proposal | - | - | catalog changed 2026-09-17; re-review required |
| hi | catalogs/hi.json | b92362a09f11c264f3f8a525e0191677800c1cde5fd10910d561cc3c37f09017 | proposal | - | - | catalog changed 2026-09-17; re-review required |
| ar | catalogs/ar.json | a64f8a0c845250189ab83221dc3bcd7142c2bc29fd925d020395e3d7be2b68f0 | proposal | - | - | catalog changed 2026-09-17; re-review required |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is tracked as child beads of the localization
audit (v0.68): the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
