import { SpeechKitElement } from "../core/element.js";
import { isVoiceSessionActive, type VoiceUiController } from "../core/controller.js";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../core/turns.js";
import {
  SpeechKitVoiceConsentElement,
  createLocalStorageConsentAdapter,
  type VoiceConsentAdapter
} from "./voice-consent.js";
import {
  SpeechKitVoiceVisualizerElement,
  sessionStatusToVisualizerState
} from "./voice-visualizer.js";
import type {
  SpeechKitVoiceDenial,
  SpeechKitVoiceEvent,
  SpeechKitVoiceSessionState,
  SpeechKitVoiceSessionStatus
} from "../core/voice-surface.js";
import type { VoiceUiMessageCatalog } from "../i18n/index.js";

const CSS = `
:host {
  display: none;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size, 13px);
}
:host([open]) {
  display: block;
}
.overlay {
  display: flex;
  flex-direction: column;
  min-height: 0;
  max-height: min(60vh, 420px);
  width: min(92vw, 360px);
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-radius: var(--sk-radius, 14px);
  background: var(--sk-surface, color-mix(in srgb, #ffffff 72%, transparent));
  backdrop-filter: blur(var(--sk-blur, 16px));
  -webkit-backdrop-filter: blur(var(--sk-blur, 16px));
  box-shadow: var(--sk-shadow, 0 12px 40px -12px rgba(16, 20, 24, 0.25));
  overflow: hidden;
}
:host([placement="center"]) {
  position: fixed;
  inset: 0;
  display: none;
  place-items: center;
  background: color-mix(in srgb, #000 20%, transparent);
  z-index: 2147483000;
}
:host([placement="center"][open]) {
  display: grid;
}
.transcript-wrap {
  position: relative;
  display: flex;
  min-height: 120px;
  flex: 1;
}
.transcript {
  display: flex;
  min-height: 0;
  flex: 1;
  flex-direction: column;
  gap: 8px;
  overflow: auto;
  padding: 16px 14px;
  mask-image: linear-gradient(to bottom, transparent 0, #000 24px, #000 calc(100% - 24px), transparent 100%);
  -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 24px, #000 calc(100% - 24px), transparent 100%);
}
.turn {
  display: grid;
  gap: 2px;
  margin: 0;
  line-height: 1.5;
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.role {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: 10px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}
.turn .text {
  font-weight: 600;
}
.turn[data-role="user"] .text {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: calc(var(--sk-font-size, 13px) - 1px);
  font-weight: 500;
}
.turn[data-draft] .text {
  opacity: 0.55;
}
.turn[data-interrupted] .text::after {
  content: " \\2026";
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
.jump {
  position: absolute;
  bottom: 8px;
  inset-inline-start: 50%;
  transform: translateX(-50%);
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-surface-strong, color-mix(in srgb, #ffffff 92%, transparent));
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--sk-font-size-small, 11px);
  font-weight: 600;
  padding: 4px 10px;
}
:host(:dir(rtl)) .jump {
  transform: translateX(50%);
}
.stage {
  display: grid;
  flex: 0 0 auto;
  justify-items: center;
  gap: 6px;
  border-top: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  padding: 12px 14px 12px;
}
.status {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--sk-font-size-small, 11px);
  font-weight: 700;
}
.live-dot {
  width: 7px;
  height: 7px;
  flex: 0 0 auto;
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-live, #dc2626);
  animation: sk-ov-breathe 1.2s ease-in-out infinite;
}
@keyframes sk-ov-breathe {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
@media (prefers-reduced-motion: reduce) {
  .live-dot { animation: none; }
}
.hint {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: var(--sk-font-size-small, 11px);
}
.ended {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: var(--sk-font-size-small, 11px);
}
.denied {
  display: grid;
  justify-items: center;
  gap: 4px;
  font-size: var(--sk-font-size-small, 11px);
  text-align: center;
}
.denied span {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
.actions {
  display: flex;
  gap: var(--sk-gap, 8px);
}
.actions button {
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-radius: var(--sk-radius, 14px);
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--sk-font-size-small, 11px);
  font-weight: 600;
  padding: 5px 10px;
}
.actions .reconnect {
  border-color: transparent;
  background: var(--sk-accent, oklch(0.65 0.13 210));
  color: var(--sk-accent-contrast, #ffffff);
}
.orb {
  border: 0;
  background: transparent;
  cursor: pointer;
  padding: 0;
}
.consent-wrap {
  padding: 16px 14px;
}
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
button:focus-visible {
  outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210));
  outline-offset: -2px;
}
`;

/**
 * `<speechkit-voice-overlay>` — the compact ephemeral glass voice-agent
 * overlay: teleprompter turn list (drafts dimmed, finals solid), autoscroll
 * with jump-to-live, tap-to-interrupt orb (barge-in), consent gate
 * (`continuous` scope), ended/reconnect stage, Escape exit with focus
 * restore. Functional port of the Floating Panel `VoiceAgentView` reference.
 *
 * Controller-driven by default; hosts that keep their own reducer set the
 * presentational overrides (`turns`, `status`, `level`, `denial`, `ended`).
 */
export class SpeechKitVoiceOverlayElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-overlay";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "open", "placement", "modal"];
  }

  #turns: VoiceAgentTurn[] = [];
  #turnsOverride: VoiceAgentTurn[] | undefined;
  #statusOverride: SpeechKitVoiceSessionStatus | undefined;
  #denialOverride: SpeechKitVoiceDenial | undefined;
  #endedOverride: boolean | undefined;
  #ended = false;
  #wasActive = false;
  #seenEvents = 0;
  #consentAdapter: VoiceConsentAdapter | undefined;
  #consentPending = false;
  #invoker: Element | null = null;
  #unsubscribeLevel: (() => void) | undefined;

  #scroller: HTMLDivElement;
  #jump: HTMLButtonElement;
  #stage: HTMLDivElement;
  #visualizer: SpeechKitVoiceVisualizerElement | undefined;
  #pinned = true;
  #programmaticScroll = false;

  #onKeydown = (event: KeyboardEvent): void => {
    if (event.key === "Escape" && this.hasAttribute("open")) {
      event.stopPropagation();
      this.exit();
    }
  };

  constructor() {
    super(CSS);
    const overlay = document.createElement("div");
    overlay.className = "overlay";
    overlay.setAttribute("part", "overlay");
    overlay.setAttribute("role", "dialog");
    overlay.setAttribute("aria-label", "Voice");

    const wrap = document.createElement("div");
    wrap.className = "transcript-wrap";
    this.#scroller = document.createElement("div");
    this.#scroller.className = "transcript";
    this.#scroller.setAttribute("part", "transcript");
    this.#scroller.setAttribute("aria-live", "polite");
    this.#scroller.addEventListener("scroll", () => this.#handleScroll());
    this.#jump = document.createElement("button");
    this.#jump.type = "button";
    this.#jump.className = "jump";
    this.#jump.setAttribute("part", "jump-to-live");
    this.#jump.hidden = true;
    this.#jump.addEventListener("click", () => this.#jumpToLive());
    wrap.append(this.#scroller, this.#jump);

    this.#stage = document.createElement("div");
    this.#stage.className = "stage";
    this.#stage.setAttribute("part", "actions");

    overlay.append(wrap, this.#stage);
    this.root.append(overlay);
  }

  // ----- presentational overrides -------------------------------------------

  get turns(): VoiceAgentTurn[] {
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

  set ended(value: boolean | undefined) {
    this.#endedOverride = value;
    this.requestUpdate();
  }

  set level(value: number) {
    if (this.#visualizer) this.#visualizer.level = value;
  }

  get consentAdapter(): VoiceConsentAdapter {
    if (!this.#consentAdapter) {
      this.#consentAdapter = createLocalStorageConsentAdapter(
        this.controller?.contract.surface ?? "default"
      );
    }
    return this.#consentAdapter;
  }

  set consentAdapter(value: VoiceConsentAdapter) {
    this.#consentAdapter = value;
  }

  // ----- lifecycle ----------------------------------------------------------

  /**
   * Opens the overlay. When the consent scope `continuous` is not granted the
   * consent gate renders first and the session only starts after acceptance
   * (fail-closed); a declined decision closes the overlay again.
   */
  show(): void {
    this.#invoker = document.activeElement;
    this.#ended = false;
    this.#wasActive = false;
    if (this.consentAdapter.read("continuous") !== "granted") {
      this.#consentPending = true;
    } else if (!isVoiceSessionActive(this.#status())) {
      // Consent already granted: the overlay owns the session start (the
      // split button only opens the overlay, it never double-starts). A fresh
      // session clears the previous dialogue like #reconnect does.
      this.#startFreshSession();
    }
    this.setAttribute("open", "");
    this.requestUpdate();
  }

  #startFreshSession(): void {
    this.#turns = [];
    this.#seenEvents = this.state?.events.length ?? 0;
    void this.controller?.start("voice_agent");
  }

  hide(): void {
    this.removeAttribute("open");
    this.#consentPending = false;
    const invoker = this.#invoker;
    this.#invoker = null;
    if (invoker instanceof HTMLElement) invoker.focus();
  }

  /** Explicit exit control: stops the session and closes the overlay. */
  exit(): void {
    void this.controller?.stop();
    this.emitKitEvent("speechkit-exit");
    this.hide();
  }

  #reconnect(): void {
    this.#ended = false;
    this.#turns = [];
    this.#seenEvents = this.state?.events.length ?? 0;
    this.emitKitEvent("speechkit-reconnect");
    void this.controller?.start("voice_agent");
    this.requestUpdate();
  }

  #interrupt(): void {
    if (this.#status() !== "speaking") return;
    this.controller?.interrupt?.();
    this.emitKitEvent("speechkit-interrupt");
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.addEventListener("keydown", this.#onKeydown);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.removeEventListener("keydown", this.#onKeydown);
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
  }

  protected override onControllerChanged(controller: VoiceUiController | null): void {
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
    if (controller?.subscribeLevel) {
      this.#unsubscribeLevel = controller.subscribeLevel((level) => {
        this.level = level;
      });
    }
  }

  protected override onSessionState(state: SpeechKitVoiceSessionState): void {
    for (let i = this.#seenEvents; i < state.events.length; i += 1) {
      const event = state.events[i];
      if (event === undefined) continue;
      this.#turns = reduceVoiceAgentTurns(this.#turns, event);
      this.emitKitEvent<SpeechKitVoiceEvent>("speechkit-voice-event", event);
    }
    this.#seenEvents = state.events.length;
    if (isVoiceSessionActive(state.status)) {
      this.#wasActive = true;
      this.#ended = false;
    } else if (this.#wasActive && this.hasAttribute("open") && state.status !== "denied") {
      // Session settled back to idle/cancelled while the overlay stayed open.
      this.#ended = true;
    }
  }

  // ----- scroll bookkeeping (port of the reference rules) -------------------

  #handleScroll(): void {
    if (this.#programmaticScroll) {
      this.#programmaticScroll = false;
      return;
    }
    const nextPinned =
      this.#scroller.scrollHeight - this.#scroller.scrollTop - this.#scroller.clientHeight < 24;
    if (nextPinned !== this.#pinned) {
      this.#pinned = nextPinned;
      this.#jump.hidden = nextPinned;
    }
  }

  #scrollToLive(): void {
    const target = this.#scroller.scrollHeight - this.#scroller.clientHeight;
    if (Math.abs(this.#scroller.scrollTop - target) < 1) return;
    this.#programmaticScroll = true;
    this.#scroller.scrollTop = this.#scroller.scrollHeight;
  }

  #jumpToLive(): void {
    this.#pinned = true;
    this.#jump.hidden = true;
    this.#scrollToLive();
  }

  // ----- rendering ----------------------------------------------------------

  #status(): SpeechKitVoiceSessionStatus {
    return this.#statusOverride ?? this.state?.status ?? "idle";
  }

  #denial(): SpeechKitVoiceDenial | undefined {
    return this.#denialOverride ?? (this.#status() === "denied" ? this.state?.denial : undefined);
  }

  #isEnded(): boolean {
    return this.#endedOverride ?? this.#ended;
  }

  protected override render(): void {
    const messages = this.msgs();
    if (this.#consentPending) {
      this.#renderConsent();
      return;
    }
    this.#renderTurns(messages);
    this.#renderStage(messages);
    if (this.#pinned) this.#scrollToLive();
  }

  #renderConsent(): void {
    this.#scroller.replaceChildren();
    this.#stage.replaceChildren();
    const wrap = document.createElement("div");
    wrap.className = "consent-wrap";
    wrap.setAttribute("part", "consent");
    const consent = document.createElement(
      SpeechKitVoiceConsentElement.tagName
    ) as SpeechKitVoiceConsentElement;
    consent.setAttribute("scope", "continuous");
    const locale = this.getAttribute("locale");
    if (locale) consent.setAttribute("locale", locale);
    consent.consentAdapter = this.consentAdapter;
    consent.addEventListener("speechkit-consent-change", (event) => {
      const detail = (event as CustomEvent<{ decision: string }>).detail;
      this.#consentPending = false;
      if (detail.decision === "granted") {
        this.#startFreshSession();
        this.requestUpdate();
      } else {
        this.hide();
      }
    });
    wrap.append(consent);
    this.#stage.replaceChildren(wrap);
  }

  #renderTurns(messages: VoiceUiMessageCatalog): void {
    const turns = this.turns;
    this.#scroller.replaceChildren();
    for (const turn of turns) {
      const p = document.createElement("p");
      p.className = "turn";
      p.setAttribute("part", turn.role === "user" ? "turn turn-user" : "turn turn-agent");
      p.dataset.role = turn.role;
      if (!turn.final) p.dataset.draft = "";
      if (turn.interrupted) p.dataset.interrupted = "";
      const role = document.createElement("span");
      role.className = "role";
      role.textContent =
        turn.role === "user" ? messages["sk.voice.agent.you"] : messages["sk.voice.agent.assistant"];
      const text = document.createElement("span");
      text.className = "text";
      text.textContent = turn.text;
      p.append(role, text);
      this.#scroller.append(p);
    }
    this.#jump.textContent = messages["sk.voice.agent.jumpToLive"];
    this.#jump.hidden = this.#pinned;
  }

  #renderStage(messages: VoiceUiMessageCatalog): void {
    this.#stage.replaceChildren();
    const status = this.#status();
    const denial = this.#denial();
    const ended = this.#isEnded();
    const speaking = status === "speaking";

    if (denial) {
      const denied = document.createElement("div");
      denied.className = "denied";
      denied.setAttribute("part", "denied");
      denied.setAttribute("role", "alert");
      const title = document.createElement("strong");
      title.textContent = denial.user_guidance.title;
      const body = document.createElement("span");
      body.textContent = denial.user_guidance.body;
      denied.append(title, body);
      this.#stage.append(denied);
    } else if (ended) {
      const endedEl = document.createElement("span");
      endedEl.className = "ended";
      endedEl.setAttribute("role", "status");
      endedEl.textContent = messages["sk.voice.agent.ended"];
      this.#stage.append(endedEl);
    } else {
      const statusLabel =
        status === "capturing"
          ? messages["sk.voice.agent.listening"]
          : status === "processing"
            ? messages["sk.voice.state.processing"]
            : speaking
              ? messages["sk.voice.agent.speaking"]
              : messages["sk.voice.agent.connecting"];

      const orb = document.createElement("button");
      orb.type = "button";
      orb.className = "orb";
      orb.setAttribute("part", "orb");
      orb.setAttribute("aria-label", speaking ? messages["sk.voice.agent.interrupt"] : statusLabel);
      orb.addEventListener("click", () => this.#interrupt());
      this.#visualizer = document.createElement(
        SpeechKitVoiceVisualizerElement.tagName
      ) as SpeechKitVoiceVisualizerElement;
      this.#visualizer.setAttribute(
        "state",
        this.state || this.#statusOverride
          ? sessionStatusToVisualizerState(status)
          : "connecting"
      );
      orb.append(this.#visualizer);
      this.#stage.append(orb);

      const statusEl = document.createElement("span");
      statusEl.className = "status";
      statusEl.setAttribute("part", "status");
      statusEl.setAttribute("role", "status");
      if (status === "capturing") {
        const dot = document.createElement("span");
        dot.className = "live-dot";
        dot.setAttribute("aria-hidden", "true");
        const sr = document.createElement("span");
        sr.className = "sr-only";
        sr.textContent = messages["sk.voice.agent.live"];
        statusEl.append(dot, sr);
      }
      statusEl.append(document.createTextNode(statusLabel));
      this.#stage.append(statusEl);

      if (speaking) {
        const hint = document.createElement("span");
        hint.className = "hint";
        hint.textContent = messages["sk.voice.agent.interrupt"];
        this.#stage.append(hint);
      }
    }

    const actions = document.createElement("div");
    actions.className = "actions";
    const canReconnect = ended || (status === "denied" && denial?.retryable === true);
    if (canReconnect) {
      const reconnect = document.createElement("button");
      reconnect.type = "button";
      reconnect.className = "reconnect";
      reconnect.setAttribute("part", "reconnect");
      reconnect.textContent = messages["sk.voice.agent.reconnect"];
      reconnect.addEventListener("click", () => this.#reconnect());
      actions.append(reconnect);
    }
    const exit = document.createElement("button");
    exit.type = "button";
    exit.setAttribute("part", "exit");
    exit.textContent = messages["sk.voice.agent.exit"];
    exit.addEventListener("click", () => this.exit());
    actions.append(exit);
    this.#stage.append(actions);
  }
}
