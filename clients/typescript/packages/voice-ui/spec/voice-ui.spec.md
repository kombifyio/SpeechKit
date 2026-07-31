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
