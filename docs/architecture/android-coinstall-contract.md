---
title: speechkit.coinstall.v1 — Android co-install contract
last_verified: 2026-08-12
status: active
---

# `speechkit.coinstall.v1`

The contract between two separately installed Android apps: the SpeechKit
keyboard/assistant APK and the kombify Companion. It is an **AIDL bound
service**, defined in its own Apache-2.0 artifact that both apps compile
against.

This supersedes the original design of a `PROVISION`
intent, an `ASK_AI` intent and capability `ContentProvider`s. That shape cannot
carry a streaming answer turn, which the "one AI, one context" routing decision
requires: the answer is spoken by SpeechKit's own overlay while the Companion's
session, memory, agents and entitlements produce it. An intent is a fire-and-
forget delivery with no channel to stream partial results back over.

## Why a separate contract artifact

The assembled SpeechKit APK is **GPL-3.0**, because it is a HeliBoard fork. The
Companion is proprietary. Neither app may link the other's code.

An AIDL interface is the seam that survives that boundary: both sides compile
the same `.aidl` sources from a third artifact licensed Apache-2.0, and the
generated stubs contain no code from either app. The wire contract is shared;
no implementation is.

This is a licensing constraint, not a preference. Do not "simplify" it later by
having one app depend on the other's module.

The artifact is `android/coinstall`, published from this repository:

```kotlin
// https://maven.pkg.github.com/kombifyio/SpeechKit
implementation("io.kombify.speechkit:coinstall-contract:<version>")
```

The version is this repository's release version (`.kombify/VERSION`). The AAR
carries the generated stubs in `classes.jar` and the `.aidl` sources under
`aidl/`, so a consumer can both call the interface and import these types from
`.aidl` of its own. Vendoring the sources instead defeats the point: the two
apps agree because they compile the same artifact, not because someone kept two
copies in step.

## Transport choice per interaction

| Interaction | Mechanism | Why |
|---|---|---|
| One AI turn, streaming | AIDL bound service | Only mechanism here that can stream partial results back to a live caller |
| Device-bound provisioning | AIDL bound service | Needs a request/response with a result the caller can act on |
| Cheap status / capability read | `ContentProvider` | Callable without binding; safe to poll from a keyboard's input path |
| Visible navigation ("open Companion settings") | `Intent` | The user sees the transition; fire-and-forget is correct |

No open port, no localhost socket, no foreground service. `bindService` starts
the Companion on demand and it is torn down when the last client unbinds, so the
co-install costs nothing while idle.

## Security model

Both checks are mandatory and both run on the **callee** side. A caller-supplied
package name is not an identity.

1. **`Binder.getCallingUid()`** — resolve the calling UID and map it to package
   names via `PackageManager.getPackagesForUid()`. Never trust a package name
   passed as a parameter; any app can pass any string.
2. **Signing-certificate pinning** — check the resolved package's signing
   certificate against the pinned expected certificate
   (`PackageManager.hasSigningCertificate()`). Package names are not unique
   across app stores and sideloads; a signature is.

Reject with a typed error rather than a silent no-op, so a genuine mismatch —
for example a debug build talking to a release build — is diagnosable instead of
looking like an absent feature.

The v1 surface is deliberately narrow. It carries **provisioning, one AI turn,
and capability/status**. It does not carry notification access, agent
invocation, or approvals. Each of those is a separate consent decision and would
need its own review; adding them to this interface later must be a version bump,
not a silent extension.

## Interface sketch

```aidl
// Apache-2.0. Compiled by both apps; implemented only by the Companion.
package io.kombify.speechkit.coinstall.v1;

import io.kombify.speechkit.coinstall.v1.ICoinstallCallback;
import io.kombify.speechkit.coinstall.v1.CapabilityStatus;
import io.kombify.speechkit.coinstall.v1.ProvisionRequest;
import io.kombify.speechkit.coinstall.v1.ProvisionResult;
import io.kombify.speechkit.coinstall.v1.TurnRequest;

interface ICoinstallService {
    /** Contract version the callee implements. Callers must not assume. */
    int getContractVersion();

    /** Cheap, synchronous. Mirrors what the status ContentProvider exposes. */
    CapabilityStatus getCapabilityStatus();

    /** Device-bound provisioning. Idempotent for a given device identity. */
    ProvisionResult provision(in ProvisionRequest request);

    /**
     * One AI turn. Partial results arrive on the callback; the turn ends with
     * exactly one terminal callback (complete or error).
     */
    void startTurn(in TurnRequest request, in ICoinstallCallback callback);

    /** Cancel an in-flight turn. Safe to call after it has already ended. */
    void cancelTurn(String turnId);
}
```

```aidl
package io.kombify.speechkit.coinstall.v1;

oneway interface ICoinstallCallback {
    void onPartial(String turnId, String text);
    void onComplete(String turnId, String text);
    void onError(String turnId, int code, String message);
}
```

`oneway` on the callback matters: the Companion must not block on the keyboard's
IPC thread while streaming, and the keyboard's input path must never wait on the
Companion.

## Golden fixtures

[`fixtures/speechkit-coinstall.v1.json`](fixtures/speechkit-coinstall.v1.json)
in the style of `speechkit-voice-surface.v1.json`: a machine-readable statement
of the surface both sides must agree on, so a consumer drift check can fail on a
diff rather than on a production call.

Install and cloud-connect UX is specified in
[android-connect-distribution-standard.md](../android-connect-distribution-standard.md).
Companion offers install; SpeechKit finishes Connect on `speechkit://connect/kombify`.

## Open points

- The concrete `TurnRequest` fields follow the assistant-session shape rather
  than being invented here; they are pinned when B-M5's session contract
  settles.
- Pinned signing certificates differ per build channel. The pin set is
  configuration, not a constant, and both debug and release pins must be
  representable.
