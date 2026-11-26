# @kombifyio/speechkit-voice-ui

Framework-neutral SpeechKit Voice UI Kit: custom elements, design tokens, and
the `speechkit.voice_ui.v1` behavior spec for voice surfaces (Voice Chat UI
Standard). No React, Svelte, or LiveKit dependency — the elements work in plain
HTML, Svelte 5, React 19, and any other host that can render custom elements.
Native surfaces (Compose) implement spec parity from the shipped
`spec/` + `src/tokens/tokens.json` + `locales/*.json` artifacts.

## Elements

| Element | Purpose |
| --- | --- |
| `<speechkit-voice-assistant>` | The standard conversation surface with an Opal visual. One element serves every frame: `size="orb"` (bare visual for hosts with their own chrome), `compact` (pill / keyboard bar / watch face), `expanded` (hero orb above the teleprompter, with screen-reader status announcements). See below. |
| `<speechkit-voice-button>` | The standard split button: primary segment starts/stops Dictation; the secondary segment (`agent` attribute; chevron click or long-press) starts the Voice Agent. Without the `voice_agent` capability it renders locked and shows the denial guidance instead of disappearing. |
| `<speechkit-voice-overlay>` | Compact ephemeral glass voice-agent overlay: teleprompter turn list (drafts dimmed, finals solid), autoscroll + jump-to-live, tap-to-interrupt orb, consent gate, ended/reconnect stage, Escape exit. |
| `<speechkit-voice-visualizer>` | Orb/pill/dot audio-state indicator (`state`, `level`), honoring `prefers-reduced-motion`. |
| `<speechkit-voice-consent>` | Fail-closed voice consent gate (`one_shot` / `continuous` scopes). |
| `<speechkit-voice-provider>` | Context provider distributing one controller to a subtree. |
| `<speechkit-voice-dialog>` | Frameless, slightly translucent card that streams the voice-agent dialogue (newest turn at the bottom, top fade). `anchor="above\|below\|start\|end"` places it next to a launcher, `auto-open` shows it when a session starts; a slim bar holds the recording indicator, an end-session icon and a hide icon. |
| `<speechkit-live-transcript>` | Bounded 1–2 line streaming transcript (`lines`, `source`, `placeholder`); only final segments reach the polite live region. |
| `<speechkit-voice-pill>` | In-Composer voice pill: recording dot, level bars and the elapsed time while dictating (`mode="dictation"`), or the Voice Agent phase (`mode="voice_agent"`). Controller-driven or host-driven (`phase`, `level`, `started-at`); hides while idle; `static` or reduced motion rests the bars. |
| `<speechkit-voice-notice>` | One-line error/status chip localized by reason code, with Retry, ✕ and an on-demand details disclosure. Never shows raw codes or URLs in the default view. |

The split button carries the recording indicator: idle, requesting (microphone
prompt / session setup), listening (red dot plus level-reactive bars),
processing, speaking and error. Motion stays inside the control; under
`prefers-reduced-motion` the bars rest at fixed heights and the visible state
label keeps "being recorded" obvious. Any denial renders as a compact notice
above the button (`no-notice` opts out, e.g. when a dialog shows it).

## Embedding a voice agent

Register the Web Components once on the client. The conversation surface accepts a SpeechKit controller or host-supplied session status, transcript turns and input/output levels.

```html
<script type="module">
  import "@kombifyio/speechkit-voice-ui/define";
</script>
<speechkit-voice-assistant id="conversation" size="expanded" transcript></speechkit-voice-assistant>
```

Set `conversation.controller` to your existing voice controller. The default uses Opal and shows two contributions. Use `agent-name="Mira"` for your own speaker name. Use `size="compact" frame="keyboard"` to put the same conversation on a single line.

For a product with its own composer or controls, embed the separate visual. The three motifs are Opal, LiveKit-inspired radial Resonance and the kombify braided Rosette. They share the same state and level inputs. Supply accessible status text through the surrounding controls.

```html
<speechkit-voice-orb id="visual" variant="resonance" state="listening" aria-hidden="true"></speechkit-voice-orb>
```

```ts
import { registerSpeechKitVoiceOrb, type SpeechKitVoiceOrbElement } from "@kombifyio/speechkit-voice-ui/orb";

registerSpeechKitVoiceOrb(); // Registers only the visual in the browser.

const visual = document.querySelector<SpeechKitVoiceOrbElement>("#visual")!;
visual.variant = "opal";
visual.opalCore = "small";
visual.state = "speaking";
visual.setLevel(0.62, "output"); // Feed normalized levels from your playback meter.
```

Place an optional `<img slot="mark" src="/brand.svg" alt="">` inside a standalone visual to display your own logo.

Set `--sk-voice-orb-size` on a standalone visual and `--sk-voice-orb-hero-size` on a conversation window to fit the available space.

## Voice Assistant (`<speechkit-voice-assistant>`)

The canonical Voice Assistant UI module. Attach a controller (or use the
ready-made adapter below), and the element renders the full experience:
state-coupled orb animation, status labels in 6 locales, live teleprompter
transcript, and tap-to-interrupt barge-in.

| Attribute | Values | Meaning |
| --- | --- | --- |
| `size` | `orb` \| `compact` (default) \| `expanded` | Bare visual / pill–bar–watch face / hero + transcript. |
| `frame` | `overlay` \| `keyboard` \| `watch` \| `phone` \| `panel` (default) | Host surface; `keyboard` renders the full-width inline bar, `watch` the round face. |
| `variant` | `opal` (default), `resonance`, `rosette`, `aura`, `waveform` | Visual used in the conversation window and compact surface. |
| `opal-core` / `opalCore` | `original` (default), `small` | Core-to-mist ratio for Opal. |
| `agent-name` / `agentName` | Text | Agent speaker label in the transcript and compact bar. A missing or blank name uses the localized **Agent** label. |
| `transcript` | boolean | Off = animation/status only; compact shows at most the last sentence. |
| `transcript-turns` / `transcriptTurns` | `1` \| `2` (default) \| `3` | Expanded display depth, including the current turn; invalid values use 2. |
| `mark-src` | URL | Host-provided brand image in the orb centre (optional for orb variants). |
| `aura-state` | 8 orb states | Host FSM override for the orb visual. |

In expanded mode the current turn is shown in full. Historical entries show
only their last sentence, in chronological order. When history is available,
one slot prefers the latest turn from the other speaker, even across adjacent
same-role ASR segments; depth 3 fills its other slot with the nearest remaining
earlier turn. The `turns` property retains the full input history. Changing
`transcriptTurns` also updates the `transcript-turns` attribute; compact and
keyboard displays are unaffected. New turns move the context upward, with
motion disabled for reduced-motion preferences.

Semantic mark ids (`rosette \| k \| none`) live in the `./marks` subpath:

```ts
import { resolveMarkSrc, semanticMarkRatio } from "@kombifyio/speechkit-voice-ui/marks";

const src = resolveMarkSrc(settings.assistantMark, { rosette: "/rosette.png", k: "/k.png" });
element.markSrc = src; // null removes the mark (pure orb)
```

Native parity ports (Compose, LVGL) implement the `assistant` and
`assistant-variants` blocks in `tokens.json` plus the spec section — never a
re-interpretation of the visuals.

## Quick start (plain HTML)

```html
<link rel="stylesheet" href="node_modules/@kombifyio/speechkit-voice-ui/src/tokens/tokens.css" />
<script type="module">
  import "@kombifyio/speechkit-voice-ui/define";
</script>

<speechkit-voice-provider id="voice">
  <speechkit-voice-button agent for="overlay">Dictate</speechkit-voice-button>
  <speechkit-voice-overlay id="overlay"></speechkit-voice-overlay>
</speechkit-voice-provider>

<script type="module">
  // The kit is UI-only: inject a VoiceUiController (sessions/transport/audio
  // stay host-owned). Use the ready-made adapter from ./voiceagent-adapter
  // (see below) or write your own.
  document.getElementById("voice").controller = myController;
</script>
```

Framework hosts assign the `controller` property directly on each element
(React 19 and Svelte 5 both set non-primitive props as element properties).
SSR hosts must import `./define` client-side only.

## Controller boundary

The kit renders the canonical `speechkit.voice_surface.v1` event stream and
owns no session FSM, provider keys, tickets, or authorization decisions. See
`VoiceUiController` in the typed API: `start/stop/cancel/subscribe/getState`
plus optional `interrupt()` (barge-in) and `subscribeLevel()` (visualizer).

### Ready-made voice-agent adapter

For a SpeechKit server you do not need to implement that interface yourself:

```ts
import { createVoiceAgentUiController } from "@kombifyio/speechkit-voice-ui/voiceagent-adapter";

const controller = createVoiceAgentUiController({
  serverUrl: "https://speechkit.example.com",
  token: sessionToken,
  start: { provider: "deepgram", locale: "en-US" },
});
```

It owns microphone capture, the ticket WebSocket, playback with barge-in
flushing, and input/output level metering, and emits the canonical event
stream. `@kombifyio/speechkit-voiceagent-client` is an **optional peer
dependency** required only for this subpath — the main entry stays
dependency-free.

## Microphone permission and consent

Check the microphone before opening a server session, so a blocked or missing
microphone never mints a ticket:

```ts
import { checkMicrophonePermission, requestMicrophone, releaseMediaStream } from "@kombifyio/speechkit-voice-ui";

const permission = await checkMicrophonePermission(); // "granted" | "prompt" | "denied" | "unsupported"
if (permission === "denied") showNotice("microphone_permission_denied");
const mic = await requestMicrophone(); // { ok, stream } or { ok: false, reason: "microphone_*" }
// ...later: releaseMediaStream(mic.stream)
```

Consent is confirmed once and remembered: `createVoiceConsentStore` persists the
decision on the device (`localStorage`, key `speechkit.voice.consent.v1`) and
reports every user decision through `onConsentChange`, so the host can store it
in the user's account. On sign-in, seed it back with `setConsentRecord(record)`.
Raising `consentVersion` asks everyone again; `revoke()` clears it.

```ts
const consent = createVoiceConsentStore({
  surface: "web",
  consentVersion: "2026-10",
  onConsentChange: (record) => api.saveVoiceConsent(record)
});
consent.setConsentRecord(await api.loadVoiceConsent());
overlay.consentAdapter = consent; // also accepted by <speechkit-voice-consent>
```

## Theming

Every visual knob is a `--sk-*` CSS custom property (see
`src/tokens/tokens.json`, the machine-readable SSOT) and every internal
surface is exposed via `::part(...)`. Include `tokens.css` for the default
light/dark glass theme or set the properties yourself; `[data-sk-theme]`
forces a theme.

## Spec parity

`spec/voice-ui.spec.md` is the normative behavior spec;
`spec/fixtures/voice-ui-turns.v1.json` and
`spec/fixtures/speechkit-voice-surface.v1.json` are the shared replay
fixtures. Web and native implementations must produce identical outputs for
every fixture case.

## License

Apache-2.0.

## Localization adapter

`locales/*.json` is the canonical six-locale catalog source, including the
native interchange export. `pnpm run localization:compile` compiles its inlang
project with Paraglide JS 2 and generates the message-ID types. The build
checks that every compiled message preserves the source text. The public
`voiceUiMessages(locale, overrides)` API, catalog exports and per-element
locale isolation stay available; hosts need no Paraglide runtime dependency.
Build from the TypeScript workspace with
`pnpm --filter @kombifyio/speechkit-voice-ui... run build` so the optional
Voice Agent peer's declarations are current. Catalog presence alone is not
signed translation QA or a device acceptance result.
