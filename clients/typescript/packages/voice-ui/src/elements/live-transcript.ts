import { SpeechKitElement } from "../core/element.js";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../core/turns.js";
import type { SpeechKitVoiceSessionState } from "../core/voice-surface.js";

const CSS = `
:host {
  display: block;
  min-width: 0;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size, 13px);
  --sk-transcript-line: 1.4em;
}
:host([data-empty]) { display: none; }
.view {
  display: flex;
  flex-direction: column;
  justify-content: flex-end;
  max-height: calc(var(--sk-transcript-line) * var(--sk-transcript-lines, 2));
  overflow: hidden;
  line-height: var(--sk-transcript-line);
  mask-image: linear-gradient(to bottom, transparent 0, #000 0.9em);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 0.9em);
}
:host([lines="1"]) .view {
  --sk-transcript-lines: 1;
  /* Row + flex-end: an over-long line overflows at the start edge, so the
     newest words stay visible in both LTR and RTL. */
  flex-direction: row;
  align-items: center;
  mask-image: linear-gradient(to right, transparent 0, #000 2.5em);
  -webkit-mask-image: linear-gradient(to right, transparent 0, #000 2.5em);
}
:host([lines="1"]:dir(rtl)) .view {
  mask-image: linear-gradient(to left, transparent 0, #000 2.5em);
  -webkit-mask-image: linear-gradient(to left, transparent 0, #000 2.5em);
}
.text {
  margin: 0;
  overflow-wrap: anywhere;
  transition: opacity var(--sk-motion-base, 180ms) var(--sk-motion-ease, ease);
}
:host([lines="1"]) .text {
  flex: none;
  white-space: nowrap;
}
.text[data-draft] { opacity: 0.62; }
.text[data-role="agent"] { font-weight: 500; }
.placeholder { color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent)); }
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
@media (prefers-reduced-motion: reduce) {
  .text { transition: none; }
}
`;

/**
 * `<speechkit-live-transcript>` — bounded 1–2 line streaming transcript for
 * dictation and voice-agent sessions. The newest words stay visible (older
 * text fades out at the top, or the leading edge with `lines="1"`); drafts
 * render dimmed. Only finalized segments reach the polite live region, so
 * screen readers are not flooded with partials.
 *
 * Attributes: `lines` (`1` | `2`, default 2), `source` (`user` | `agent` |
 * `any`, default `any` — which role's latest turn to show), `placeholder`
 * (show the localized "Listening" while capturing without text).
 * Host-driven alternative to the controller: the `text` / `final` properties.
 */
export class SpeechKitLiveTranscriptElement extends SpeechKitElement {
  static readonly tagName = "speechkit-live-transcript";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "lines", "source", "placeholder"];
  }

  #turns: VoiceAgentTurn[] = [];
  #seenEvents = 0;
  #hostText: string | undefined;
  #hostFinal = false;
  #lastAnnounced = "";
  readonly #view: HTMLDivElement;
  readonly #text: HTMLParagraphElement;
  readonly #live: HTMLSpanElement;

  constructor() {
    super(CSS);
    this.#view = document.createElement("div");
    this.#view.className = "view";
    this.#view.setAttribute("part", "transcript");
    this.#view.setAttribute("aria-hidden", "true");
    this.#text = document.createElement("p");
    this.#text.className = "text";
    this.#text.setAttribute("part", "text");
    this.#view.append(this.#text);
    this.#live = document.createElement("span");
    this.#live.className = "sr-only";
    this.#live.setAttribute("aria-live", "polite");
    this.#live.setAttribute("role", "status");
    this.root.append(this.#view, this.#live);
  }

  /** Host-driven text (overrides the controller stream while set). */
  get text(): string | undefined {
    return this.#hostText;
  }

  set text(value: string | undefined) {
    this.#hostText = value;
    this.requestUpdate();
  }

  /** Whether the host-driven `text` is a finalized segment. */
  get final(): boolean {
    return this.#hostFinal;
  }

  set final(value: boolean) {
    this.#hostFinal = value;
    this.requestUpdate();
  }

  /** Clears the controller-driven transcript (e.g. before a new dictation). */
  clear(): void {
    this.#turns = [];
    this.#lastAnnounced = "";
    this.#live.textContent = "";
    this.requestUpdate();
  }

  protected override onSessionState(state: SpeechKitVoiceSessionState): void {
    if (state.events.length < this.#seenEvents) {
      this.#turns = [];
      this.#seenEvents = 0;
    }
    for (let i = this.#seenEvents; i < state.events.length; i += 1) {
      const event = state.events[i];
      if (event === undefined) continue;
      if (event.type === "voice.capture_started" && state.mode === "dictation") this.#turns = [];
      this.#turns = reduceVoiceAgentTurns(this.#turns, event);
    }
    this.#seenEvents = state.events.length;
  }

  #latest(): { text: string; final: boolean; role: VoiceAgentTurn["role"] } | null {
    if (this.#hostText !== undefined) {
      return { text: this.#hostText, final: this.#hostFinal, role: "user" };
    }
    const source = this.getAttribute("source");
    for (let i = this.#turns.length - 1; i >= 0; i -= 1) {
      const turn = this.#turns[i];
      if (!turn || !turn.text) continue;
      if (source === "user" || source === "agent") {
        if (turn.role !== source) continue;
      }
      return { text: turn.text, final: turn.final, role: turn.role };
    }
    return null;
  }

  protected override render(): void {
    const messages = this.msgs();
    const latest = this.#latest();
    const capturing = this.state?.status === "capturing";
    const showPlaceholder = !latest && capturing && this.hasAttribute("placeholder");
    this.toggleAttribute("data-empty", !latest && !showPlaceholder);
    this.#live.setAttribute("aria-label", messages["sk.voice.transcript.label"]);

    if (showPlaceholder) {
      this.#text.textContent = messages["sk.voice.state.capturing"];
      this.#text.className = "text placeholder";
      delete this.#text.dataset["draft"];
      return;
    }
    this.#text.className = "text";
    if (!latest) {
      this.#text.textContent = "";
      return;
    }
    if (this.#text.textContent !== latest.text) this.#text.textContent = latest.text;
    this.#text.toggleAttribute("data-draft", !latest.final);
    this.#text.dataset["role"] = latest.role;
    if (latest.final && latest.text !== this.#lastAnnounced) {
      this.#lastAnnounced = latest.text;
      this.#live.textContent = latest.text;
    }
  }
}
