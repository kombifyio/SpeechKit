# SpeechKit Hybrid

Prepared on 2026-11-26 for #BuildHybridAI 2026. This is the source for the Devpost
project description and the remaining submission materials. It is a development
entry with open device acceptance, not a completed contest submission.

The description is saved on the public [SpeechKit Hybrid project page](https://devpost.com/software/speechkit-hybrid).
Devpost published the page on 2026-11-26; the hackathon entry remains unsubmitted.
The gallery contains four actual development screenshots with English captions.

## One-line summary

An Android voice keyboard and modular SDK for dictation, text assistance, and voice agents, with explicit cloud and on-device control.

## Project description

### Inspiration

People dictate messages, shorten notes, and correct text while working inside
other apps. They should be able to choose where that processing happens without
reconfiguring their keyboard for every action. An unreliable connection should
not erase a recording, and a private request should stay local when local
processing is selected.

### What it does

SpeechKit Hybrid is a native Android voice keyboard and reusable SDK. The
development build combines dictation, text assistance, and voice-agent controls
in the keyboard's shortcut panel.

- Choose local use, your own API keys or endpoints, or a Kombify account.
- Select a voice provider and model from the existing SpeechKit catalog through
  dynamic dropdowns. The selection belongs to the active connection.
- Dictate, summarize, rewrite, or correct text. Text actions preview a result
  before insertion and check that the original editor is still active.
- Use Cloud First or Local First, or restrict processing to Local Only or Cloud
  Only. In either preferred mode, a long hold selects the opposite destination
  for that invocation only. Exclusive modes use the hold to communicate at their
  fixed destination. A ring and lock or cloud symbol show the armed destination.
- Keep keyboard shortcuts visible while idle. An active voice session brings
  Opal into the compact strip. Expanded voice mode is implemented but still
  needs successful device acceptance.
- Reuse a bounded recording across compatible local recognition adapters,
  including installed Phonon/Parakeet models and ready Google ML Kit recognition.
  Availability and language support determine which adapter can run.

### How we built it

Firebase AI Logic handles hybrid text inference through its official Android
SDK. SpeechKit maps its four execution policies to the SDK's inference modes and
records the reported processing location. App Check protects the Firebase path.

Firebase Live and cloud audio transcription use their official SDK APIs.
Transcription on the device uses separate native or ML Kit speech adapters.
Firebase's hybrid text fallback does not provide an automatic offline replacement
for a Live session. Own-key Gemini access is an explicit direct adapter.

The Android modules separate task policy, bounded audio handling, adapter
selection, provider connections, and native UI. Applications can reuse those
contracts without embedding the reference keyboard. The optional hybrid module
builds as an Android library with source and dependency metadata.

Local fallback respects the invocation's destination permission. Local-only work
never becomes a cloud request because a model is missing. Adapters do not silently
download a model or substitute the phone's language for a multi-language request.
Firebase cloud-preferred text fallback follows the SDK's offline behavior; it
does not retry every provider denial.

### Existing foundation and hackathon work

SpeechKit predates this event. Its native HeliBoard keyboard, dictation
controllers, downloaded recognizers, remote voice protocol, and Room storage
provide the foundation. Opal is an existing SpeechKit voice visual.

The work started on November 24 adds Firebase hybrid integration, native
connection setup, policy-aware gestures, keyboard text actions, Opal keyboard
integration, shared local speech adapters, local tools, and receipt handling.
Android is the entered platform. We do not claim a new Web or iOS build.

### Challenges and what we learned

Hybrid behavior has two distinct choices: whether a request may leave the device,
and which available local engine can handle it. Separating those choices avoids
turning a local model failure into unintended cloud processing.

SDK availability also differs by feature. A device can run the keyboard while
the required on-device model remains unavailable. Classic Android recognition
owns its microphone, whereas buffered adapters can reuse captured audio. The
implementation exposes those differences rather than promising identical
fallback for every provider.

### Accomplishments and current evidence

Both Android app variants and their instrumentation APKs build. The hybrid
library artifacts build, and 15 existing authentication-related tests pass.
Installed-device journeys verified setup and local audio decoding/readiness.
The keyboard opened, accepted typing, and displayed compact Opal.

An earlier physical-device candidate produced a Firebase cloud text response
with App Check enforced. A separate earlier direct-key Gemini candidate verified
audio response, interruption, an audio follow-up, and stale-profile rejection.
Those results have separate scope: they do not establish the latest Firebase
Live integration or offline Nano inference.

### What is next

The remaining acceptance work is actual Nano 4 generation through Firebase on
a supported, provisioned phone, the final Firebase SDK voice/audio journey,
expanded-panel and gesture verification, and the complete offline workflow.
The latest combined device rerun did not execute because the test quota was
exhausted. Ordinary builds keep the new features disabled while acceptance is
open. A public judge build and the English demonstration video are still pending.

## Why this matters

The intended audience is people who write by voice across Android apps and
developers who need the same destination controls in their own applications.
The keyboard puts the choice at the action, while the SDK gives hosts one set
of policy and adapter contracts. No adoption or performance metrics are claimed.

## How Codex was used

Codex inspected the existing SpeechKit contracts, implemented the Android
adapters and keyboard integration, compared official SDK documentation,
performed source review, built both variants, and ran device journeys. Test
failures and unavailable models remain recorded as acceptance gaps.

## Architecture and testing instructions

The [feature reference](hybrid-features.md) describes module ownership, gestures,
local adapter behavior, and feature evidence. The [build guide](build-hybrid-ai-2026.md)
covers the pinned toolchain, flags, Firebase configuration, App Check, device
readiness, and the acceptance sequence. It distinguishes earlier tested APKs
from the current candidate.

After the judge build is qualified, the demonstration must show the following:

1. Identify the phone, installed model, and observed inference source.
2. Choose a connection and show the dynamic voice selection.
3. Dictate and preview a text action from the keyboard.
4. Show tap and hold under both preferred policies and both exclusive policies.
5. Demonstrate permitted cloud processing and actual local processing, including
   offline Firebase Nano 4 text inference. Show the observed source each time.
6. Interrupt a voice turn, then show that uncertain remote actions are not replayed.

Provision models explicitly before the offline demonstration. Keep credentials
out of public instructions and screenshots. Judge access must remain free and
available through the judging period.

## Public repository and demo

- Public project source: [kombifyio/SpeechKit](https://github.com/kombifyio/SpeechKit).
- Framework license: [Apache-2.0](https://github.com/kombifyio/SpeechKit/blob/main/LICENSE).
  The Android app and HeliBoard have a separate GPL boundary described in the
  [Android GPL notice](../../android/GPL-NOTICE.md).
- The public mirror exists. The final exported hackathon revision and a build
  from that revision still need verification before formal submission.
- Public judge build: pending qualification and publication.
- Public YouTube demo: pending recording after device acceptance. Maximum three
  minutes, with English narration or English subtitles.

## Screenshot shot list

Four installed-app screenshots are available: the three setup choices, Firebase
provider/model dropdowns, the idle keyboard, and compact Opal. Captions must not
present configuration or a setup error as a successful cloud voice session.
Expanded Opal and the completed local-inference result still need successful
captures. No synthetic mockup is used as runtime evidence.

## Demo video outline

| Time | Scene after acceptance passes |
| --- | --- |
| 0:00–0:20 | Dictate into another app and explain destination control. |
| 0:20–0:45 | Show connection setup, model readiness, and the actual phone. |
| 0:45–1:30 | Show cloud text assistance and the one-action local hold gesture. |
| 1:30–2:10 | Disable connectivity, run local dictation and Firebase Nano 4 text assistance, and show their sources. |
| 2:10–2:40 | Show voice interruption and compact/expanded behavior once verified. |
| 2:40–3:00 | Explain the reusable modules and identify the new hackathon work. |

## Official form fields still to complete

The Devpost connector supplied the current fields on 2026-11-26. This table
prepares factual copy; it does not assert eligibility or accept legal terms.

| Field | Prepared answer or remaining input |
| --- | --- |
| 29971, submitter type | Confirm individual or team for final submission. |
| 29972, residence | Confirm the submitter's country for final submission. |
| 29973, organization | Kombify, subject to the chosen entrant identity. |
| 29974, project start date | The hybrid extension began 11-24-26. SpeechKit existed before the event; confirm the date interpretation for the existing project. |
| 29975, new or existing | Existing. |
| 29976, existing versus new work | Use the corresponding section of the project description. |
| 29977, platform | Android. |
| 29981, technology requirement | Firebase AI Logic runtime text inference, Firebase Live and audio adapters, and App Check. Actual Android Nano 4 inference remains a prerequisite. |
| 29978, demo/test build | Pending qualified public build. |
| 29980, testing instructions | Use the build guide and qualified device procedure; arrange private judge access separately. |
| 29979, license URL | Public Apache-2.0 license above, with the Android GPL boundary disclosed. |
| 29982–29984, eligibility attestations | Require the entrant's confirmation. No boxes accepted by this draft. |

The submission deadline is November 30, 2026, 23:00 Europe/Berlin. Judging ends
December 16, 2026, 23:00 Europe/Berlin. Recheck the live form before final entry.
Sources: [submission requirements](https://firebasehybridaihackathon.devpost.com/)
and [official rules](https://firebasehybridaihackathon.devpost.com/rules).
