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
| `<speechkit-voice-button>` | The standard split button: primary segment starts/stops Dictation; the secondary segment (`agent` attribute; chevron click or long-press) starts the Voice Agent. Without the `voice_agent` capability it renders locked and shows the denial guidance instead of disappearing. |
| `<speechkit-voice-overlay>` | Compact ephemeral glass voice-agent overlay: teleprompter turn list (drafts dimmed, finals solid), autoscroll + jump-to-live, tap-to-interrupt orb, consent gate, ended/reconnect stage, Escape exit. |
| `<speechkit-voice-visualizer>` | Orb/pill/dot audio-state indicator (`state`, `level`), honoring `prefers-reduced-motion`. |
| `<speechkit-voice-consent>` | Fail-closed voice consent gate (`one_shot` / `continuous` scopes). |
| `<speechkit-voice-provider>` | Context provider distributing one controller to a subtree. |

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
  // stay host-owned). kombify surfaces wrap @kombify/ai-sdk/voice; OSS hosts
  // use the ready-made adapter from ./voiceagent-adapter (see below) instead
  // of writing their own.
  document.getElementById("voice").controller = myController;
</script>
```

Framework hosts assign the `controller` property directly on each element
(React 19 and Svelte 5 both set non-primitive props as element properties).
SSR hosts must import `./define` client-side only.

## Controller boundary

The kit renders the canonical `speechkit.voice_surface.v1` event stream and
owns no session FSM, provider keys, tickets, or entitlement authority. See
`VoiceUiController` in the typed API: `start/stop/cancel/subscribe/getState`
plus optional `interrupt()` (barge-in) and `subscribeLevel()` (visualizer).

### Ready-made voice-agent adapter

For a SpeechKit server you do not need to implement that interface yourself:

```ts
import { createVoiceAgentUiController } from "@kombifyio/speechkit-voice-ui/voiceagent-adapter";

const controller = createVoiceAgentUiController({
  serverUrl: "https://speechkit.example.com",
  token: sessionToken,
  start: { provider: "gemini", locale: "en-US" },
});
```

It owns microphone capture, the ticket WebSocket, playback with barge-in
flushing, and input/output level metering, and emits the canonical event
stream. `@kombifyio/speechkit-voiceagent-client` is an **optional peer
dependency** required only for this subpath — the main entry stays
dependency-free.

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
