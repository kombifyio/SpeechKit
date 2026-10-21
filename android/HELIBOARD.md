# HeliBoard fork

`android/heliboard` is a submodule of [kombifyio/heliboard](https://github.com/kombifyio/heliboard),
our fork of [Helium314/HeliBoard](https://github.com/Helium314/HeliBoard). It
carries the full key layout that the voice-only IME in `android/ime` does not
have: SpeechKit's own keyboard can dictate and hold a conversation, but you
cannot type on it.

The submodule tracks the `speechkit` branch, not `main`. `main` stays a clean
mirror of upstream so a rebase is always a rebase of our patches onto it,
never a merge of two diverged histories.

## The licence boundary decides the packaging

Upstream is GPL-3.0 (`LICENSE`), with Apache-2.0 and CC-BY-SA-4.0 covering
parts of the tree (`LICENSE-Apache-2.0`, `LICENSE-CC-BY-SA-4.0`). Verified
against the repository itself rather than taken from the plan, because the
whole packaging decision rests on it.

That compatibility runs one way. Apache-2.0 code may be combined into a
GPL-3.0 work; GPL-3.0 code may not be combined into an Apache-2.0
distribution. So:

- **The shipped `io.kombify.speechkit` APK is GPL-3.0 as a whole.** It
  links `:heliboard` (included from `android/settings.gradle.kts`) together
  with SpeechKit's Apache-2.0 modules (`:core`, `:net`, `:ime`,
  `:voice-ui-compose`, `:assistant`).
- **It must not include proprietary Kombify code.** Nothing from a private
  module, no vendored closed component. Companion integration stays IPC-only.
- **The SpeechKit modules themselves stay Apache-2.0.** The GPL applies to
  the assembled APK because of the HeliBoard link, not to those libraries
  in isolation.

## Where the keyboard APK is built, and why

The plan asked for two things that pull against each other: convert the
fork's `app` into a library module, and keep the patch series small enough
that rebasing stays cheap. A library conversion edits `app/build.gradle.kts`,
which is exactly the file upstream touches most.

Resolved in favour of the conversion, because the alternative is worse. The
keyboard needs SpeechKit's dictation panel, not just an interface, and that
panel sits on `:core`, `:net` and `:voice-ui-compose` and is wired with Hilt.
Reaching it from a standalone fork would mean publishing four Maven
artifacts, pinning versions across two repositories, and making the fork's
build apply Hilt — a far deeper intrusion, and a permanent one.

So the keyboard APK is assembled from this repository, where those modules
are siblings in one Gradle build and none of that machinery is needed. The
cost is one upstream build file in the patch series, and the drift job
watches it.

### What the conversion cost

Done in `c9e31d8f`. A library module generates no `VERSION_NAME`,
`VERSION_CODE` or `APPLICATION_ID` in `BuildConfig`, and the fork read all
three across nine call sites.

`VERSION_NAME` and `VERSION_CODE` came back as `buildConfigField`s pinned to
the upstream release this fork tracks. That is what they mean here --
`AppUpgrade` keys its settings migrations off `VERSION_CODE`, and the other
readers report the keyboard engine's version, not the product's -- so all
seven of their call sites stayed untouched.

`APPLICATION_ID` deliberately got no `buildConfigField`: a hardcoded value
would compile and then resolve to an id that does not exist at runtime. Its
two users were fixed instead, and neither needed the consuming app's id after
all:

- `latin/utils/JniUtils.java` used it only for a fallback path that the very
  next lines overwrite whenever an `Application` is reachable. Without one
  there is no user-supplied library to find, so it now says so rather than
  guessing a directory.
- `latin/utils/GestureDataGathering.kt` used it to namespace a private IME
  option that only ever gets compared against itself -- `GestureDataScreen`
  sets it, `GestureDataGatheringSettings` reads it back -- so a stable literal
  is equivalent.

One non-obvious keeper: `targetSdk` stays at upstream's value even though a
library ignores it for packaging. Robolectric reads it to pick the emulated
SDK level, and `ContextCompat.registerReceiver` branches on that; below 34 it
emulates `RECEIVER_NOT_EXPORTED` through a runtime permission check that
`LatinIME.onCreate` cannot satisfy under test. Dropping it failed 138 tests
that had nothing to do with the conversion.

The rest was mechanical: swap the plugin, drop
`applicationId`/`versionCode`/`versionName`, drop the `ApplicationVariant`
block and `dependenciesInfo` (both application-only), drop
`applicationIdSuffix`, `signingConfig` and `isShrinkResources` from the build
types, and turn `proguardFiles` into `consumerProguardFiles` so the rules ship
to whichever app minifies.

`:app:assembleRelease` produces an AAR carrying `classes.jar`, `proguard.txt`
and `libjni_latinime.so` for all four ABIs. `:app:testRunTestsUnitTest` fails
10 of 178 (`InputTest` 6, `ParserTest` 4) -- the same 10 the unmodified fork
fails, so the conversion introduced no regression. Those 10 are upstream's own
and are why upstream reserves the `runTests` variant; our build must run that
task rather than a blanket `test`, which would also sweep the variants
upstream expects to fail.

Upstream moved to AGP 9 (built-in Kotlin, `compileSdk`/`targetSdk` 37, Kotlin
plugins 2.4.0) in the rebase of 2026-10-21. SpeechKit is still on AGP 8.13, so
the fork's `app/build.gradle.kts` keeps `kotlin("android")` and the explicit
`jvmTarget` that upstream dropped; remove both when SpeechKit moves to AGP 9.
`compileSdk`/`targetSdk` follow upstream (37) and build under AGP 8.13 from
this build. Rebased state: `:heliboard:assembleRelease`, `:app:assembleOssDebug`,
`:app:assembleOssRelease` and `:app:testOssDebugUnitTest` pass;
`:heliboard:testRunTestsUnitTest` fails 12 of 193 (`InputTest` 6, `ParserTest`
4, plus 2 `XLinkTest` cases that probe the third-party
`roccobot.github.io/HeliBoard-RLM` link, a 404 independent of the patch).

## The seam is the patch surface

The fork does not learn about SpeechKit. It calls
`helium314.keyboard.latin.SpeechKitVoiceBridge` — `showPanel`, `hidePanel`,
`showShortcuts`, toolbar actions, and the `isDictationBlocked` guard that
keeps voice input out of password fields. The `:app` host
(`InlineVoicePanel`) answers those calls. Everything else stays on our side
of the line, which is what keeps the patch small enough to rebase.

Target patch surface: the voice-key dispatch, the suggestion-strip layout,
the input-view slot for the inline panel, a settings hook, and branding.

Rebase cost comes from the upstream files the patch modifies. Files the patch
only adds (`SpeechKitVoiceBridge.kt`, the `ic_speechkit_*` drawables) never
conflict. The Gboard-shaped strip, the toolbar action keys and the library
build (13 patches as of 2026-10-21) modify these reviewed upstream files; the
weekly `heliboard-drift.yml` fails when the patch modifies any other upstream
file. Review a new one against the seam above and add it here in the same pull
request, or move the change to the host side.

<!-- reviewed-upstream-files:start -->
- `app/build.gradle.kts`: library build, plugin versions from the consuming build
- `app/src/main/java/helium314/keyboard/keyboard/KeyboardSwitcher.java`: inline panel mount
- `app/src/main/java/helium314/keyboard/keyboard/internal/KeyboardIconsSet.kt`: host-chosen toolbar glyphs
- `app/src/main/java/helium314/keyboard/latin/App.kt`: consuming application class
- `app/src/main/java/helium314/keyboard/latin/InputAttributes.java`: dictation guard for password fields
- `app/src/main/java/helium314/keyboard/latin/LatinIME.java`: voice-key dispatch, service hand-off
- `app/src/main/java/helium314/keyboard/latin/suggestions/SuggestionStripView.kt`: strip layout
- `app/src/main/java/helium314/keyboard/latin/utils/GestureDataGathering.kt`: library build
- `app/src/main/java/helium314/keyboard/latin/utils/JniUtils.java`: library build
- `app/src/main/java/helium314/keyboard/latin/utils/ToolbarUtils.kt`: SpeechKit toolbar keys
- `app/src/main/res/layout/input_view.xml`: input-view slot for the inline panel
- `app/src/main/res/values/strings.xml`: toolbar key labels
<!-- reviewed-upstream-files:end -->

## Rebase cadence

Upstream is active. Keeping `main` a clean mirror and `speechkit` a thin
patch series on top is what makes catching up cheap; letting the patch series
grow into a rewrite is the failure mode to avoid.

```bash
git -C android/heliboard fetch upstream
git -C android/heliboard rebase upstream/main speechkit
```
