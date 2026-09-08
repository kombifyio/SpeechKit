---
title: Android SpeechKit connect and distribution
last_verified: 2026-08-25
status: active
---

# Android SpeechKit connect and distribution

Binding contract for how the SpeechKit Android APK gets a working backend, and
how kombify Companion offers install and cloud connect. SpeechKit owns the
IME, VoiceInteractionService, connection mode, AIDL caller, and the
`speechkit://connect/kombify` finish step. Companion owns the install offer
and the signed-in user's Gateway session. Neither app may link the other's
code; they meet at `speechkit.coinstall.v1`.

This supersedes the silent "Companion present replaces tester token" behavior
described in earlier pairing notes.

## Connection modes

Persist `connection_mode` in SpeechKit (`speechkit_config`). Companion being
installed must not change the mode by itself.

| Mode | Wire value | Backend | Who chooses it |
| --- | --- | --- | --- |
| On device | `on_device` | Platform `SpeechRecognizer` | OSS default; kombify fallback |
| SpeechKit origin | `speechkit_origin` | `https://speechkit.kombify.io` + baked `SPEECHKIT_SERVER_TOKEN` | Default for kombifyDebug, Firebase testers, later Play Internal |
| Self-host | `self_host` | User-typed SpeechKit URL + token | User saves a server in Settings |
| Kombify Cloud | `kombify_cloud` | `https://api.kombify.io/v1/speechkit` + Companion Auth0 access token | Explicit Connect, or Companion-driven install/connect |

Resolution:

- `kombify_cloud` uses the Companion `provision()` session. If there is no
  session, stay on-device. Do not fall back to the tester origin or a typed
  self-host — that would send Cloud-labelled traffic with a service bearer.
- `self_host` uses the stored URL/token only.
- `speechkit_origin` uses the shipped tester origin.
- `on_device` ignores servers.

Missing `connection_mode` infers `self_host` when a stored URL exists, else
`speechkit_origin` when a shipped origin exists, else `on_device`. It never
infers `kombify_cloud`.

## Two product paths

```text
Independent tester / self-host
  kombifyDebug or Firebase APK
  -> speechkit_origin (baked service token)
  -> speechkit.kombify.io
  -> server-held Deepgram / AssemblyAI / OpenAI
  no Gateway, no FGA, no product rate limit on the service bearer

Kombify Cloud
  Companion Settings "Install SpeechKit" or SpeechKit "Connect Kombify Cloud"
  -> speechkit://connect/kombify
  -> mode kombify_cloud
  -> Gateway /v1/speechkit + user JWT
  -> FGA and limits
```

Both paths must be testable on one device. Installing Companion must leave
the tester origin in place until the user connects.

## Testers and development builds

- `kombifyDebug` / Firebase App Distribution / later Play Internal bake a
  revocable `SPEECHKIT_SERVER_TOKEN` via Firebase or
  `scripts/android/assemble-tester.ps1`. Testers do not type a key. Keyboard,
  Assistant, and Voice Agent work on first launch. A kombify build with an
  origin URL and no token does not become `speechkit_origin`; it stays
  on-device until a token is baked or the user types a server.
- Local loop: `scripts/android/assemble-tester.ps1` reads the token from
  Doppler `kombify-io` / `prd_speechkit` and installs `kombifyDebug`.
- Firebase fails the kombify variant when the token is missing.
- Public/release kombify APKs never bake a token.
- Vendor keys (`DEEPGRAM_API_KEY`, and the rest) stay on the server or in a
  user's self-host. They are not compiled into the APK.
- Origin service-bearer traffic is exempt from SpeechKit server rate limits.
  Gateway JWT identities stay limited.

Play Internal Testing is the next distribution track. It is specified here
and not implemented in the first slice.

## Standalone and framework users

Developers integrating SpeechKit as a framework pass provider or SpeechKit
keys in host configuration. They do not go through kombify Gateway.

Users of the kombify SpeechKit APK who do not want Cloud paste their own
SpeechKit URL and token (`self_host`). That may be a homelab or any
speechkit-server they run. OSS stays the zero-cloud-key local-only floor.

Android BYOK that dials `api.deepgram.com` from the APK is a later slice
(B-M6). Voice Agent still requires a SpeechKit server.

## Companion install offer

Companion Settings exposes **SpeechKit** (Voice Agent + AI Assistant +
Keyboard):

- Not installed: offer Play (`market://details?id=io.kombify.speechkit`) and,
  while that listing is empty, Firebase App Distribution testers.
- Installed: launch `speechkit://connect/kombify` so SpeechKit can finish
  Connect (provision + persist `kombify_cloud`) and continue keyboard
  onboarding.

`provision()` still returns the current Auth0 access token and
`{apiBaseUrl}/v1/speechkit`. No AIDL version bump. `startTurn` stays unused.

## SpeechKit Settings

- Show the active mode.
- **Connect Kombify Cloud** binds Companion, persists `kombify_cloud`.
- **Disconnect** returns to `speechkit_origin` when a shipped origin exists,
  otherwise `on_device`.
- If Companion is missing, offer to open/install it.
- If Companion is present but signed out, open Companion so the user can sign
  in, then return.
- Self-host URL/token/LAN finder stay available.

Inbound finish URI: `speechkit://connect/kombify`. Optional https alias:
`https://speechkit.kombify.io/connect`.

## Secrets and local-only

- Tokens in tester APKs are a deliberate, revocable exposure. Rotate in
  Doppler and rebuild to cut them off.
- OSS and the documented fresh-install local-only path stay free of cloud
  keys.
- Customer copy uses Android string resources with `en` and `de`.

## Related

- [architecture/android-coinstall-contract.md](architecture/android-coinstall-contract.md)
- [android/CONTRACT.md](../android/CONTRACT.md)
- kombify-Mobile Companion Settings and `CoinstallService`
