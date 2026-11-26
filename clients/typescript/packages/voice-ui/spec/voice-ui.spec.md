# SpeechKit Voice UI Kit — Behavior Spec (`speechkit.voice_ui.v1`)

Normative behavior for every implementation of the kit's primitives — the web
custom elements in this package and the native (Compose) parity
implementation. The workspace-level policy source is
`AI-VOICE-SPEECHKIT-TARGET.md` (§Voice UI Kit, §Voice Chat UI Standard).

## Inputs

Implementations render the canonical `speechkit.voice_surface.v1` event stream
through a host-injected controller. The kit owns no session FSM, transport,
provider keys, tickets, or authorization decisions.

## Session status → visual state

| Session status | Visualizer state | Notes |
| --- | --- | --- |
| `idle` | `idle` | Also after `voice.tts_finished`. |
| `capturing` | `listening` | Live dot + "Listening". |
| `processing` | `processing` | Dimmed breathing. |
| `speaking` | `speaking` | Pulse; tap-to-interrupt affordance active. |
| `cancelled` | `idle` | |
| `denied` | `error` | Denial guidance rendered from the envelope. |

`connecting` is a kit-local optimistic state between `start()` and the first
`voice.capture_started`/`voice.denied`. The server-FSM `recovering` state is
rendered as `ended` + reconnect affordance in v1.

## Teleprompter turn rules

Defined by `reduceVoiceAgentTurns` (`src/core/turns.ts`, doc comment is
normative) and replayed by `spec/fixtures/voice-ui-turns.v1.json`:

- Drafts carry CUMULATIVE text and replace the open turn of their role.
- Finals close the open turn; empty final text keeps the draft text.
- `voice.barge_in` closes AND marks the open agent turn (`interrupted`).
- Unrelated events change nothing (same-reference return).
- Render: drafts dimmed, finals solid; user turns secondary, agent turns
  primary; autoscroll pinned to live output; user scrolling unpins and shows a
  jump-to-live affordance (re-pin threshold: within 24px of the bottom).

## Voice Assistant element (`speechkit-voice-assistant`)

The Web Component uses the Opal visual by default. The separately registered
`speechkit-voice-orb` exposes the same Opal, Resonance and Rosette visuals to
hosts with their own controls. It accepts `variant`, `state`, `opal-core` and
`setLevel(value, "input" | "output")`. The conversation surface additionally
supports the existing Aura and Glass Waveform motifs.

Attribute contract (all optional):

| Attribute | Values | Meaning |
| --- | --- | --- |
| `size` | `orb` \| `compact` (default) \| `expanded` | `orb` renders the bare orb for hosts that own their chrome (Device-Target prompter, Android overlay, box); `compact` the pill/bar/watch face; `expanded` the hero orb above the teleprompter, with a visually hidden status announcement. |
| `frame` | `overlay` \| `keyboard` \| `watch` \| `phone` \| `panel` (default) | Host surface. Compact `keyboard` renders the full-width inline bar; compact `watch` the round watch face; other frames the pill. |
| `transcript` | boolean attribute | Off = animation/status only. Compact shows at most the last sentence of the newest turn; expanded shows the configured recent context. Ignored for `size="orb"`. |
| `transcript-turns` | `1` \| `2` (default) \| `3` | Expanded entry count including the current turn; invalid or absent values resolve to 2. The reflected property is `transcriptTurns`. |
| `variant` | `opal` (default), `resonance`, `rosette`, `aura`, `waveform` | Visual used in the conversation window and compact surface. |
| `opal-core` / `opalCore` | `original` (default), `small` | Core-to-mist ratio for Opal. |
| `agent-name` / `agentName` | Text | Agent speaker label in the transcript and compact bar. A missing or blank name uses the localized **Agent** label. |
| `mark-src` | URL | Host-provided brand image rendered in the orb centre; absent = pure orb. The kit accepts a host-provided brand mark — kombify hosts pass the AI-teal rosette (standard) or the k monogram, mapped from the semantic ids in the `./marks` subpath (`rosette \| k \| none`). The waveform variant has no mark slot. |
| `aura-state` | one of the 8 orb states below | Host override for the orb visual. Absent = derived from the session status. |
| `locale` | BCP 47 | Inherited catalog resolution (`sk.voice.*`). |

Orb visual states — a superset of the session statuses, because host FSMs are
richer than the surface contract:

| Aura state | Reached from | Active |
| --- | --- | --- |
| `inactive` | `idle`, `cancelled` | no |
| `connecting` | kit-local `connecting` | yes |
| `listening` | `capturing` | yes |
| `processing` | `processing` | yes |
| `speaking` | `speaking` | yes |
| `recovering` | host override only | yes |
| `settling` | host override only | yes |
| `error` | `denied` | no |

For the Aura variant, the layer stack, per-state colour pairs, timings, and level formulas are
normative in `src/tokens/tokens.json` → `assistant` (the SSOT the Compose and
LVGL ports implement): breathing glow, 9 s aurora sweep, counter-rotating 13 s
inner sweep, level-reactive halo ring (`scale = 0.82 + level*0.3`,
`opacity = 0.35 + level*0.5`), glassy core, centre spark, brand mark at 34 %.
Inactive states stop every animation and desaturate the mark to a grey ghost.

Behavior:

- Status = session status plus the kit-local `connecting` (controller attached,
  no session state yet). Labels come from `sk.voice.state.*` /
  `sk.voice.agent.connecting`.
- Levels: controller `subscribeLevel` (input + output channels) smoothed by the
  shared envelope follower (`SmoothedLevel`); hosts with their own reducer or a
  richer FSM use the presentational overrides (`turns`, `status`, `auraState`,
  `level`/`setLevel`) exactly like the overlay's.
- Orb tap while `speaking` = barge-in (`interrupt()`, emits
  `speechkit-interrupt`); inert otherwise.
- The expanded transcript keeps the hero fixed above the context. It displays
  the full current turn and up to the configured number of entries; historical
  entries show their last sentence. One history slot prefers the most recent
  other speaker even across adjacent same-role ASR segments; a second slot
  takes the nearest remaining earlier turn. Entries remain chronological, and
  the `turns` property retains full history. New turns move context upward;
  reduced motion disables that animation. The visible area retains autoscroll
  pinning and jump-to-live; interrupted turns retain their flag.
- Reduced motion per §Motion & accessibility: per-state colours and the
  resting/active mark treatment remain, all animation and level transforms are
  disabled.

## Split button

- Primary segment: `idle`/`cancelled`/`denied` → `start("dictation")`;
  `capturing` → `stop()`; `processing` → `cancel()`.
- Secondary segment exists only when the host requests it (`agent`); it starts
  the Voice Agent and opens the overlay. Long-press (≥500 ms) on the primary
  is an equivalent enhancement; the chevron stays the accessible path.
- Without the `voice_agent` capability the secondary segment renders locked —
  never hidden, never dead: activation routes through the controller (which
  emits the canonical `voice.denied`) and the denial `user_guidance`
  (title/body/next_steps) is rendered adjacent to the control.

## Consent

- Scopes: `one_shot` (dictation/assist) and `continuous` (voice_agent).
  Granting `continuous` implies `one_shot`; the reverse never holds.
  Declining revokes all scopes. Fail-closed: unset/declined never captures.
- The overlay gates on `continuous` consent BEFORE `start("voice_agent")`.
- A decision is asked once and persisted on the device; hosts mirror it in
  the account (`onConsentChange`) and seed it back (`setConsentRecord`). A
  record with another `consent_version` reads as unset (asked again).

## Microphone permission

- Check permission (Permissions API, then device labels, optional probe)
  BEFORE minting a server session; `denied`/`unsupported` fail without a
  session. getUserMedia failures map to `microphone_permission_denied`,
  `microphone_unavailable`, `microphone_in_use`, `microphone_unsupported`.
- Every acquired stream is released (all tracks stopped) on stop, cancel,
  session error and connect failure.

## Recording indicator

- States per `tokens.json` → `indicator`: idle, requesting, listening,
  processing, speaking, error. Listening always shows the live colour (solid
  dot plus level bars) and a state label; error is a hollow ring, never the
  solid live dot.
- Motion lives inside the control only; reduced motion keeps static bars,
  the solid dot and the visible label.

## Voice pill (`speechkit-voice-pill`)

The in-Composer pill shown while a voice session runs (AI Composer: the pill
sits inside the Composer face; nothing opens above it). Hosts place it, wire
its state and pass message overrides; they never rebuild the dot, bars,
elapsed time or phase label. Normative block: `tokens.json` → `pill`; replay:
`spec/fixtures/voice-ui-pill.v1.json`.

- Content: the recording indicator (§Recording indicator) plus one text slot.
  `mode="dictation"` (default) shows the elapsed recording time `m:ss` while
  listening; `mode="voice_agent"` shows the phase label instead.
- Phases: `requesting | listening | processing | speaking | error`. Without a
  host `phase` the pill derives it from the session status through the
  indicator status mapping; `idle`/`cancelled` hide the pill. Labels:
  `sk.voice.state.requesting` (Voice Agent: `sk.voice.agent.connecting`),
  `sk.voice.state.capturing`, `sk.voice.state.processing`,
  `sk.voice.state.speaking`, `sk.voice.state.denied`.
- Elapsed time counts from `started-at` (epoch ms) or the first listening
  render of the activation, once per second, and resets when the pill hides.
- Levels: controller `subscribeLevel`, the `level` attribute or `setLevel()`.
- Accessibility: a polite status region announces only the phase label, once
  per change; the time is a `timer` labelled `sk.voice.pill.elapsed` and is
  never announced on its own; `aria-busy` while requesting or processing.
- Reduced motion (`prefers-reduced-motion` or the `static` attribute): the
  indicator's reduced-motion rules and no colour transition; the time keeps
  counting because it is text, not motion.

## Notices

- Errors render as one localized line chosen by reason code
  (`voiceNoticeKind`), plus Retry (retryable kinds or `retryable: true`) and
  dismiss. Raw codes, guidance free text and URLs never appear in the default
  view; the details disclosure shows the localized hint, URL-free guidance
  steps, the reason code and the support reference.

## Dialog and live transcript

- The dialog is frameless and translucent, newest turn at the bottom with a
  top fade, bounded height; end-session is an icon control. Only closed turns
  are announced (polite), drafts are not.
- The live transcript keeps the newest words visible within 1–2 lines and
  announces only final segments.

## Interrupt / exit / reconnect

- Orb tap while `speaking` = barge-in (`interrupt()`); inert otherwise.
- Explicit exit control stops the session and closes the overlay; Escape is
  equivalent. Focus returns to the invoking element.
- A session that settles to idle while the overlay is open renders `ended`
  with a reconnect affordance; a retryable denial also offers reconnect.

## Motion & accessibility

- `prefers-reduced-motion` (or platform equivalent) degrades every animation
  to a static state indicator; the level transform is disabled.
- Transcript region is a polite live region. All text uses the `sk.voice.*`
  catalogs (6 locales, `ar` rendered RTL via host `dir`).

## Parity artifacts

- Tokens: `src/tokens/tokens.json` (SSOT; version `speechkit.voice_ui.v1`).
- Strings: `locales/*.json` (byte-equal to the TS catalogs).
- Fixtures: `spec/fixtures/voice-ui-turns.v1.json` (turn + status replay),
  `spec/fixtures/voice-ui-pill.v1.json` (pill phases, elapsed time, reduced
  motion) and
  `spec/fixtures/speechkit-voice-surface.v1.json` (canonical contract
  fixture, drift-gated against the private canonical copy).
