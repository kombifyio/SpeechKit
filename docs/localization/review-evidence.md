---
title: Localization Review Evidence
last_verified: 2026-09-03
status: active
---

# Localization Review Evidence

SpeechKit's shared catalog inventory below records historical normalized hashes.
It is not release evidence. The workspace design standard sections 11.4, 11.8
and 11.9 require automated second-model translation QA and a signed DSSE
attestation binding the exact source, catalogs and checks. Only locales passing
that evidence may be negotiated. Do not infer admission from a row here.

The legacy `localization:evidence` tool maintains these proposal rows for the
existing shared catalogs. A change to a catalog updates its normalized hash and
leaves it as a proposal. The legacy `human-reviewed` state does not substitute
for the current signed automated evidence.

## Catalogs

| locale | catalog | sha256 | review_state | reviewer | reviewed_on | notes |
| --- | --- | --- | --- | --- | --- | --- |
| en | catalogs/en.json | 2ec751d77139345031b32d56e4826077dfca8d468afba50e3f59cd2b5dc04c79 | proposal | - | - | catalog changed 2026-10-14; re-review required |
| de | catalogs/de.json | d597385bbebb6b83977439c5e43a88e4ca628349a7686a443a1b1193c1e8375d | proposal | - | - | catalog changed 2026-10-14; re-review required |
| es | catalogs/es.json | 047d47c643ce922f07c5694346fab05891ff2127fc277bbce28e199ba8f613b7 | proposal | - | - | catalog changed 2026-10-14; re-review required |
| zh-Hans | catalogs/zh-Hans.json | 1a5f56560baea93d8f49d75ebff9775304773d880e9d076a6124cce18059785d | proposal | - | - | catalog changed 2026-10-14; re-review required |
| hi | catalogs/hi.json | a70da8762683efb80d3f49ad9f00a8192e7a095773cb353a356e9d6e96f5eb9a | proposal | - | - | catalog changed 2026-10-14; re-review required |
| ar | catalogs/ar.json | 6eb618785a7a255bd3af350135da5010947c08b163f4c0ea7a562bc15f5c6eb1 | proposal | - | - | catalog changed 2026-10-14; re-review required |
| fr | catalogs/fr.json | ddccb20b4003d1893bcad59aa263ba3c5ffb73a4d76cbd86cef5be6725169a20 | proposal | - | - | prepared candidate, not admitted (not in supportedLocales); signed translation QA outstanding |

## Coverage beyond the catalogs

The catalogs carry the message IDs the framework resolves itself. User-facing
text that still bypasses them is known: the desktop notepad's snapshot status map, the Android
assistant's hardcoded result strings, the desktop activity log, and the
TypeScript voice-ui locales, which should join this evidence file.
