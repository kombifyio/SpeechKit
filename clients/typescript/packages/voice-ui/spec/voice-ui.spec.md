# SpeechKit Voice UI Kit — Behavior Spec (`speechkit.voice_ui.v1`)

Normative behavior for every implementation of the kit's primitives — the web
custom elements in this package and the native (Compose) parity
implementation. The workspace-level policy source is
`AI-VOICE-SPEECHKIT-TARGET.md` (§Voice UI Kit, §Voice Chat UI Standard).

## Inputs

Implementations render the canonical `speechkit.voice_surface.v1` event stream
through a host-injected controller. The kit owns no session FSM, transport,
provider keys, tickets, or entitlement authority.

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

The default Voice Assistant surface — the "Aura Orb" visual language
(decision 2026-08-10; promoted from the voice-ui lab, where the Glass
Waveform and Ring variants remain as future customization presets).
One element serves every kit frame; native implementations (Compose, LVGL)
port this section plus the `assistant` block in `tokens.json` (SSOT for
layers, per-status tones, timings, and level formulas).

Attribute contract (all optional):

| Attribute | Values | Meaning |
| --- | --- | --- |
| `size` | `orb` \| `compact` (default) \| `expanded` | `orb` renders the bare orb for hosts that own their chrome (Device-Target prompter, Android overlay, box); `compact` the pill/bar/watch face; `expanded` the hero orb + status pill + teleprompter turn list. |
| `frame` | `overlay` \| `keyboard` \| `watch` \| `phone` \| `panel` (default) | Host surface. Compact `keyboard` renders the full-width inline bar; compact `watch` the round watch face; other frames the pill. |
| `transcript` | boolean attribute | Off = animation/status only. Compact shows at most the last sentence of the newest turn; expanded shows the full turn list. Ignored for `size="orb"`. |
| `mark-src` | URL | Host-provided brand image rendered in the orb centre; absent = pure orb. The kit ships no brand asset — kombify hosts pass the AI-teal rosette (standard) or the k monogram. |
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

The layer stack, per-state colour pairs, timings, and level formulas are
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
- The expanded turn list follows §Teleprompter turn rules including autoscroll
  pinning and jump-to-live; interrupted turns render the
  `sk.voice.agent.interrupted` flag.
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
- Fixtures: `spec/fixtures/voice-ui-turns.v1.json` (turn + status replay) and
  `spec/fixtures/speechkit-voice-surface.v1.json` (canonical contract
  fixture, drift-gated against the private canonical copy).
