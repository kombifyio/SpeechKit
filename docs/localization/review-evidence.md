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
| en | catalogs/en.json | ccbc76cc2413d824e6d8f5f9c8064be27f58388e7608222500a19f4a67c175f0 | human-reviewed | Marcel K | 2026-09-03 | Source locale; every message ID authored and read in full. |
| de | catalogs/de.json | d36c34707f5ac3dce88617a61c4cae5fce1721015f29bbed22c62cb08814f904 | proposal | - | - | Machine translation proposal; native-speaker review outstanding (localization audit child bead). |
| es | catalogs/es.json | 91a94dd45369fba318d7e1d5dda7825dae481ea3b07eaad26e4979a85f03560b | proposal | - | - | Machine translation proposal; native-speaker review outstanding (localization audit child bead). |
| zh-Hans | catalogs/zh-Hans.json | da4525b2bad0058367f0759fa38a65d8175f55e8faa67bdb3547761c554cd82a | proposal | - | - | Machine translation proposal; native-speaker review outstanding (localization audit child bead). |
| hi | catalogs/hi.json | d53ed96c69709332c6daa057e4b089281ac95ba1fea4f36d2e0e4414110c83dd | proposal | - | - | Machine translation proposal; native-speaker review outstanding (localization audit child bead). |
| ar | catalogs/ar.json | 6506b52b64d845fe44c9714ef46a4650ab9074736c3ce861801a04dfa11c7ebc | proposal | - | - | Machine translation proposal; native-speaker review outstanding (localization audit child bead). |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is tracked as child beads of the localization
audit (v0.68): the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
