import { SpeechKitElement } from "../core/element.js";
import { isVoiceSessionActive, type VoiceUiController } from "../core/controller.js";
import { INDICATOR_CSS, RecordingIndicator, sessionStatusToIndicatorState } from "../core/indicator.js";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../core/turns.js";
import type {
  SpeechKitVoiceDenial,
  SpeechKitVoiceSessionState,
  SpeechKitVoiceSessionStatus
} from "../core/voice-surface.js";
import type { VoiceUiMessageCatalog } from "../i18n/index.js";
import { SpeechKitVoiceNoticeElement } from "./voice-notice.js";

export type VoiceDialogAnchor = "above" | "below" | "start" | "end";

const STOP_ICON =
  '<svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true"><rect x="3.5" y="3.5" width="9" height="9" rx="2" fill="currentColor"/></svg>';
const CLOSE_ICON =
  '<svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>';

const CSS = `
:host {
  display: none;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size, 13px);
  z-index: 2147482000;
}
:host([open]) { display: block; }
:host([anchor]) { position: absolute; }
:host([anchor="above"]) { bottom: calc(100% + 8px); inset-inline-start: 0; }
:host([anchor="below"]) { top: calc(100% + 8px); inset-inline-start: 0; }
:host([anchor="start"]) { inset-inline-end: calc(100% + 8px); bottom: 0; }
:host([anchor="end"]) { inset-inline-start: calc(100% + 8px); bottom: 0; }
.card {
  display: flex;
  flex-direction: column;
  width: min(92vw, var(--sk-dialog-width, 340px));
  max-height: var(--sk-dialog-max-height, 260px);
  box-sizing: border-box;
  border: 0;
  border-radius: var(--sk-dialog-radius, 18px);
  background: var(--sk-dialog-surface, color-mix(in srgb, #ffffff 58%, transparent));
  backdrop-filter: blur(var(--sk-dialog-blur, 22px)) saturate(1.2);
  -webkit-backdrop-filter: blur(var(--sk-dialog-blur, 22px)) saturate(1.2);
  box-shadow: var(--sk-dialog-shadow, 0 18px 48px -20px rgba(16, 20, 24, 0.35));
  overflow: hidden;
  animation: sk-dialog-in var(--sk-motion-base, 180ms) var(--sk-motion-ease, ease-out);
}
.turns {
  display: flex;
  flex: 1 1 auto;
  min-height: 0;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
  padding: 12px 14px 4px;
  scrollbar-width: none;
  mask-image: linear-gradient(to bottom, transparent 0, #000 var(--sk-dialog-fade, 28px));
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 var(--sk-dialog-fade, 28px));
}
.turns::-webkit-scrollbar { display: none; }
.turns:empty { display: none; }
.turn {
  max-width: 88%;
  margin: 0;
  line-height: 1.4;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.turn[data-role="agent"] { align-self: flex-start; }
.turn[data-role="user"] {
  align-self: flex-end;
  text-align: end;
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: calc(var(--sk-font-size, 13px) - 1px);
}
.turn[data-draft] { opacity: 0.62; }
.turn[data-interrupted]::after {
  content: " \\2026";
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
.bar {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 36px;
  padding: 4px 6px 4px 12px;
}
.status {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: var(--sk-font-size-small, 11px);
  font-weight: 600;
}
.bar[data-state="listening"] .status { color: var(--sk-live, #dc2626); }
.notice-slot { flex: 1 1 auto; min-width: 0; }
.notice-slot:empty { display: none; }
.icon {
  display: inline-grid;
  place-items: center;
  width: 24px;
  height: 24px;
  flex: none;
  border: 0;
  border-radius: var(--sk-radius-pill, 999px);
  background: transparent;
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  cursor: pointer;
  padding: 0;
  transition: background-color var(--sk-motion-base, 180ms) ease, color var(--sk-motion-base, 180ms) ease;
}
.icon:hover { background: color-mix(in srgb, currentColor 10%, transparent); color: var(--sk-text, inherit); }
.icon.end:hover { color: var(--sk-danger, #dc2626); }
.icon:focus-visible { outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210)); outline-offset: 1px; }
.icon[hidden], .ind[hidden], .status[hidden] { display: none; }
.bar[data-state="error"] .icon { align-self: flex-start; margin-top: 3px; }
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
@keyframes sk-dialog-in {
  from { opacity: 0; transform: translateY(4px) scale(0.985); }
  to { opacity: 1; transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .card { animation: none; }
}
${INDICATOR_CSS}
`;

/**
 * `<speechkit-voice-dialog>` — minimalist, frameless, slightly translucent
 * card that streams a voice-agent dialogue (user and agent turns, newest at
 * the bottom, bounded height with a top fade). A slim bar carries the
 * recording indicator, the status, an end-session icon and a hide icon; a
 * denial renders as the compact `speechkit-voice-notice` in that bar.
 *
 * Attributes: `open`, `auto-open` (opens when a voice-agent session starts
 * or is denied; after the user hides it, stays closed until the next
 * session), `anchor` (`above` | `below` | `start` | `end`; positions the card
 * next to a launcher inside a positioned container), `no-dismiss`, `no-end`. Slots: `leading` (before the indicator), `actions` (before the
 * icons). Events: `speechkit-end-session` (cancelable; default stops the
 * controller), `speechkit-dismiss`, `speechkit-retry` (from the notice).
 */
export class SpeechKitVoiceDialogElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-dialog";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "open", "anchor", "no-dismiss", "no-end", "auto-open"];
  }

  #turns: VoiceAgentTurn[] = [];
  #turnsOverride: VoiceAgentTurn[] | undefined;
  #statusOverride: SpeechKitVoiceSessionStatus | undefined;
  #denialOverride: SpeechKitVoiceDenial | undefined;
  #seenEvents = 0;
  #wasActive = false;
  #userHidden = false;
  #renderedTurns: readonly VoiceAgentTurn[] | null = null;
  #announced = 0;
  #unsubscribeLevel: (() => void) | undefined;

  readonly #turnsEl: HTMLDivElement;
  readonly #bar: HTMLDivElement;
  readonly #status: HTMLSpanElement;
  readonly #noticeSlot: HTMLDivElement;
  readonly #notice: SpeechKitVoiceNoticeElement;
  readonly #end: HTMLButtonElement;
  readonly #close: HTMLButtonElement;
  readonly #live: HTMLSpanElement;
  readonly #indicator = new RecordingIndicator();

  #onKeydown = (event: KeyboardEvent): void => {
    if (event.key === "Escape" && this.hasAttribute("open") && !this.hasAttribute("no-dismiss")) {
      event.stopPropagation();
      this.dismiss();
    }
  };

  constructor() {
    super(CSS);
    const card = document.createElement("section");
    card.className = "card";
    card.setAttribute("part", "dialog");
    this.#turnsEl = document.createElement("div");
    this.#turnsEl.className = "turns";
    this.#turnsEl.setAttribute("part", "turns");
    this.#turnsEl.setAttribute("role", "log");
    this.#turnsEl.setAttribute("aria-live", "off");

    this.#bar = document.createElement("div");
    this.#bar.className = "bar";
    this.#bar.setAttribute("part", "bar");
    const leading = document.createElement("slot");
    leading.name = "leading";
    this.#status = document.createElement("span");
    this.#status.className = "status";
    this.#status.setAttribute("part", "status");
    this.#status.setAttribute("role", "status");
    this.#noticeSlot = document.createElement("div");
    this.#noticeSlot.className = "notice-slot";
    this.#notice = document.createElement(
      SpeechKitVoiceNoticeElement.tagName
    ) as SpeechKitVoiceNoticeElement;
    this.#notice.setAttribute("part", "notice");
    this.#notice.setAttribute("flat", "");
    // While denied, the notice's ✕ is the card's hide control.
    this.#notice.addEventListener("speechkit-dismiss", (event) => {
      event.stopPropagation();
      this.dismiss();
    });
    this.#notice.addEventListener("speechkit-retry", (event) => {
      // The dialog owns the restart so it can clear the previous dialogue.
      event.preventDefault();
      this.#restart();
    });
    const actions = document.createElement("slot");
    actions.name = "actions";
    this.#end = this.#icon("end", "end-session", STOP_ICON, () => this.end());
    this.#close = this.#icon("close", "dismiss", CLOSE_ICON, () => this.dismiss());
    this.#bar.append(leading, this.#indicator.element, this.#status, this.#noticeSlot, actions, this.#end, this.#close);

    this.#live = document.createElement("span");
    this.#live.className = "sr-only";
    this.#live.setAttribute("aria-live", "polite");

    card.append(this.#turnsEl, this.#bar, this.#live);
    this.root.append(card);
  }

  #icon(className: string, part: string, svg: string, onClick: () => void): HTMLButtonElement {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `icon ${className}`;
    button.setAttribute("part", part);
    button.innerHTML = svg;
    button.addEventListener("click", onClick);
    return button;
  }

  // ----- presentational overrides -------------------------------------------

  get turns(): readonly VoiceAgentTurn[] {
    return this.#turnsOverride ?? this.#turns;
  }

  set turns(value: VoiceAgentTurn[] | undefined) {
    this.#turnsOverride = value;
    this.requestUpdate();
  }

  set status(value: SpeechKitVoiceSessionStatus | undefined) {
    this.#statusOverride = value;
    this.requestUpdate();
  }

  set denial(value: SpeechKitVoiceDenial | undefined) {
    this.#denialOverride = value;
    this.requestUpdate();
  }

  /** Host-driven level (0..1) when no controller level feed exists. */
  setLevel(value: number, source: "input" | "output" = "input"): void {
    this.#indicator.setLevelFeed(true);
    this.#indicator.setLevel(value, source);
  }

  // ----- lifecycle ----------------------------------------------------------

  show(): void {
    this.setAttribute("open", "");
  }

  hide(): void {
    this.removeAttribute("open");
  }

  /** Hides the card (the session keeps running; the launcher still shows it). */
  dismiss(): void {
    this.#userHidden = true;
    this.hide();
    this.emitKitEvent("speechkit-dismiss");
  }

  /** Ends the session (cancelable `speechkit-end-session`; default stops the controller). */
  end(): void {
    const proceed = this.dispatchEvent(
      new CustomEvent("speechkit-end-session", { bubbles: true, composed: true, cancelable: true })
    );
    if (proceed) void this.controller?.stop();
  }

  #restart(): void {
    this.#turns = [];
    this.#seenEvents = this.state?.events.length ?? 0;
    this.#wasActive = false;
    void this.controller?.start("voice_agent");
    this.requestUpdate();
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.addEventListener("keydown", this.#onKeydown);
    this.#indicator.resume();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.removeEventListener("keydown", this.#onKeydown);
    this.#indicator.stop();
  }

  protected override onControllerChanged(controller: VoiceUiController | null): void {
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
    this.#notice.controller = controller;
    this.#indicator.setLevelFeed(Boolean(controller?.subscribeLevel));
    if (controller?.subscribeLevel) {
      this.#unsubscribeLevel = controller.subscribeLevel((level, source) =>
        this.#indicator.setLevel(level, source)
      );
    }
  }

  protected override onSessionState(state: SpeechKitVoiceSessionState): void {
    if (state.events.length < this.#seenEvents) {
      this.#turns = [];
      this.#seenEvents = 0;
    }
    for (let i = this.#seenEvents; i < state.events.length; i += 1) {
      const event = state.events[i];
      if (event === undefined) continue;
      if (event.type === "voice.capture_started" && !isVoiceSessionActive(this.#lastStatus)) {
        // A new session: fresh dialogue, and auto-open may show the card again.
        this.#turns = [];
        this.#userHidden = false;
        this.#announced = 0;
      }
      this.#turns = reduceVoiceAgentTurns(this.#turns, event);
      this.#lastStatus = event.type === "voice.capture_started" ? "capturing" : this.#lastStatus;
    }
    this.#seenEvents = state.events.length;
    this.#lastStatus = state.status;
    if (isVoiceSessionActive(state.status)) this.#wasActive = true;
    if (
      this.hasAttribute("auto-open") &&
      !this.#userHidden &&
      state.mode === "voice_agent" &&
      (isVoiceSessionActive(state.status) || state.status === "denied")
    ) {
      this.show();
    }
  }

  #lastStatus: SpeechKitVoiceSessionStatus = "idle";

  // ----- rendering ----------------------------------------------------------

  #currentStatus(): SpeechKitVoiceSessionStatus {
    return this.#statusOverride ?? this.state?.status ?? "idle";
  }

  #statusLabel(status: SpeechKitVoiceSessionStatus, connecting: boolean, messages: VoiceUiMessageCatalog): string {
    if (connecting) return messages["sk.voice.agent.connecting"];
    switch (status) {
      case "capturing":
        return messages["sk.voice.agent.listening"];
      case "processing":
        return messages["sk.voice.state.processing"];
      case "speaking":
        return messages["sk.voice.agent.speaking"];
      default:
        return messages["sk.voice.agent.ended"];
    }
  }

  protected override render(): void {
    const messages = this.msgs();
    const status = this.#currentStatus();
    const denial = this.#denialOverride ?? (status === "denied" ? this.state?.denial : undefined);
    const active = isVoiceSessionActive(status);
    const connecting = !active && !denial && !this.#wasActive && this.#statusOverride === undefined;

    const indicatorState = sessionStatusToIndicatorState(status, connecting);
    this.#indicator.setState(denial ? "error" : indicatorState);
    this.#bar.dataset["state"] = this.#indicator.state;

    this.#noticeSlot.replaceChildren();
    if (denial) {
      this.#notice.denial = this.#denialOverride;
      const locale = this.getAttribute("locale");
      if (locale) this.#notice.setAttribute("locale", locale);
      this.#noticeSlot.append(this.#notice);
      this.#status.hidden = true;
      this.#indicator.element.hidden = true; // the notice carries its own mark
    } else {
      this.#indicator.element.hidden = false;
      this.#status.hidden = false;
      this.#status.textContent = this.#statusLabel(status, connecting, messages);
    }

    this.#end.hidden = this.hasAttribute("no-end") || !(active || connecting);
    this.#end.setAttribute("aria-label", messages["sk.voice.dialog.end"]);
    this.#end.title = messages["sk.voice.dialog.end"];
    this.#close.hidden = this.hasAttribute("no-dismiss") || denial !== undefined;
    this.#notice.toggleAttribute("no-dismiss", this.hasAttribute("no-dismiss"));
    this.#close.setAttribute("aria-label", messages["sk.voice.dialog.hide"]);
    this.#close.title = messages["sk.voice.dialog.hide"];
    this.root.querySelector(".card")?.setAttribute("aria-label", messages["sk.voice.dialog.label"]);

    this.#renderTurns(messages);
  }

  #renderTurns(messages: VoiceUiMessageCatalog): void {
    const turns = this.turns;
    if (turns === this.#renderedTurns) return;
    const scroller = this.#turnsEl;
    const pinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 24;
    scroller.replaceChildren();
    for (const turn of turns) {
      if (!turn.text) continue;
      const p = document.createElement("p");
      p.className = "turn";
      p.setAttribute("part", turn.role === "user" ? "turn turn-user" : "turn turn-agent");
      p.dataset["role"] = turn.role;
      if (!turn.final) p.dataset["draft"] = "";
      if (turn.interrupted) p.dataset["interrupted"] = "";
      const who = document.createElement("span");
      who.className = "sr-only";
      who.textContent = `${turn.role === "user" ? messages["sk.voice.agent.you"] : messages["sk.voice.agent.assistant"]}: `;
      p.append(who, document.createTextNode(turn.text));
      scroller.append(p);
    }
    // Announce only closed turns, once each.
    if (turns.length < this.#announced) this.#announced = 0;
    for (let i = this.#announced; i < turns.length; i += 1) {
      const turn = turns[i];
      if (!turn?.final) break;
      const who = turn.role === "user" ? messages["sk.voice.agent.you"] : messages["sk.voice.agent.assistant"];
      this.#live.textContent = `${who}: ${turn.text}`;
      this.#announced = i + 1;
    }
    this.#renderedTurns = turns;
    if (pinned) scroller.scrollTop = scroller.scrollHeight;
  }
}
