# Architecture and kombify integration

## Roles

| Component | Role | Technology / boundary |
|---|---|---|
| kombify box (ESP32-S3) | Microphone, speaker, and status display | USB UAC (48 kHz), LVGL UI; Wi-Fi is not required for the USB profile. |
| `speechkit-device-agent` | Paired satellite host for wake, capture, local bridge calls, TTS playback, and lifecycle events | `pkg/speechkit/deviceagent`; only local SpeechKit URL and pairing material. |
| Local `speechkit-server` | Device/room policy, durable action ledger, Home Assistant and TTS credential custody | Four independently paired `/v1/device-agent/*` POST routes. |
| Home Assistant | Sole smart-home semantic authority and state source | Local Conversation API plus REST state readback. |
| kombify AI Gateway | Optional LLM/STT/TTS backend for other SpeechKit modes | Never part of the G0 local smart-home action path. |

## SpeechKit contract alignment

- Hands-Free is an activation layer, not a fourth mode. A desktop Companion can
  use `companion.NewHandsFree` with `TargetAssist` for a one-shot voice
  interaction.
- The host owns microphone capture (malgo), playback, device selection, and UI
  state.
- The framework owns the `DetectionEvent` contract, Assist/TTS routing, and
  events such as `wake.fired`, `processing.started`, `skill.executed`,
  `tts.started`, and `tts.finished`.
- A USB satellite that calls `ProcessAssist` directly still needs a result
  callback because `HandleWake` currently discards the `AssistResult`,
  including TTS audio. An upstream `Options.OnResult func(AssistResult)` hook
  remains a useful framework improvement.

## Upstream candidates for kombify-SpeechKit

1. `examples/kombify-box-satellite/` as the reference for a USB satellite plus
   Hands-Free integration (`go-assist-usb-satellite` template candidate).
2. An `Options.OnResult` hook in `companion`.
3. The wake-word hardening priorities in `wakeword-robustness.md`.
4. A `kombify` single-word phrase next to `hey_kombify`, with a documented
   higher false-accept risk and a stricter recommended threshold.

## G0 smart-home path

The G0 box/satellite flow is deliberately local and narrow:

```text
box / device agent
  -> paired local SpeechKit POST route
  -> exact static local light rule
  -> durable at-most-once claim
  -> Home Assistant Conversation API
  -> exact target validation + REST state readback
  -> claim-bound local SpeechKit TTS of persisted HA speech/language
  -> box playback
```

Home Assistant owns smart-home semantics. The server, not the box or device
agent, holds the Home Assistant origin/token and TTS configuration. Only an
exact device, room, rule ID, phrase, locale, active time window, action, and
`light.*` entity match can dispatch. G0 supports only `turn_on` and `turn_off`.

This path does not traverse Gateway, MCP, cloud federation, the general Assist
pipeline, or an LLM. It has no implicit cloud fallback. The static rule is not
a Workbench approval or Cloud standing grant; future governed grants require a
separate receipt and replication contract.

An action is reported as executed only after Home Assistant identifies exactly
the authorized successful entity, reports no failed target, and its REST state
matches the expected `on` or `off` value. A dispatch whose result cannot be
proven remains indeterminate and is never automatically repeated with the same
UUIDv7 request ID.

Longer-lived Voice Agent conversations and central multi-room arbitration are
separate future capabilities. They must not weaken or replace this local G0
authority boundary.

## Security

- Pairing tokens and Home Assistant tokens are loaded from environment-backed
  secret storage and never committed to TOML or returned over MCP.
- Each device has an independent token and non-recycled pairing epoch. It also
  pins the stable local server instance ID.
- HTTP is allowed only on loopback. LAN IP and DNS origins require HTTPS for
  both device-to-SpeechKit and SpeechKit-to-Home Assistant traffic.
- The bridge checks the direct peer IP against explicit local CIDRs and does
  not trust forwarded headers for device authorization.
- The `/api/v1` alias, Gateway, MCP, and federation surfaces do not expose the
  four local device-agent routes.
- Wake-word capture is opt-in, and the box visibly reports listening state.
- The durable ledger provides at-most-once dispatch. An ambiguous outcome
  requires manual Home Assistant state inspection before a fresh command.

See [`device-agent.md`](device-agent.md) for configuration and protocol details.
