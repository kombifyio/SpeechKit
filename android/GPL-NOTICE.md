# The Android app is distributed under GPL-3.0

The `io.kombify.speechkit` APK links the keyboard in
[`android/heliboard`](HELIBOARD.md), a fork of
[HeliBoard](https://github.com/Helium314/HeliBoard) that is licensed
GPL-3.0-only. Linking it makes the assembled APK a GPL-3.0 work as a whole, so
that is how it is conveyed.

## What that does and does not cover

The SpeechKit modules under `android/` — `core`, `domain`, `net`, `ime`,
`assistant`, `voice-ui-compose`, `app` — remain **Apache-2.0 as sources**. We
hold their copyright, and licensing one assembled artifact under GPL-3.0 does
not relicense them. They stay Apache-2.0 for every other consumer, which is
why `:ime` must never import the fork: it is consumed by clients that cannot
take GPL code, and the one class that bridges the two
(`app/.../keyboard/InlineVoicePanel.kt`) sits in `:app`, on this side of the
line.

The consequence runs the other way too, and it is permanent: **no proprietary
Kombify code may be linked into this APK.** Companion integration is IPC-only
by design, not by convenience.

## Corresponding source

Everything needed to build the APK is published:

- The SpeechKit half is this `android/` tree.
- The keyboard half is <https://github.com/kombifyio/heliboard>, branch
  `speechkit`. The exact revision this build was assembled from is recorded in
  `android/heliboard.rev`, written at export time from the submodule pointer
  so it cannot drift from what was actually built.

`main` in that fork is kept as a clean mirror of upstream, so the SpeechKit
patch series is exactly `git log main..speechkit`.
