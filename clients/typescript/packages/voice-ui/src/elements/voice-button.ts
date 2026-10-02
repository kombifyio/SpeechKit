import { SpeechKitElement } from "../core/element.js";
import { isVoiceSessionActive, type VoiceUiController } from "../core/controller.js";
import {
  INDICATOR_CSS,
  RecordingIndicator,
  sessionStatusToIndicatorState,
  type RecordingIndicatorState
} from "../core/indicator.js";
import { SpeechKitVoiceNoticeElement } from "./voice-notice.js";
import type {
  SpeechKitVoiceDenial,
  SpeechKitVoiceEvent,
  SpeechKitVoiceSessionState
} from "../core/voice-surface.js";

const LONG_PRESS_MS = 500;
/** Safety bound for the kit-local `requesting` state when no event arrives. */
const REQUESTING_TIMEOUT_MS = 20_000;

const CSS = `
:host {
  display: inline-flex;
  font-size: var(--sk-font-size, 13px);
}
.split {
  position: relative;
  display: inline-flex;
  align-items: stretch;
  overflow: visible;
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-radius: var(--sk-radius, 14px);
  background: var(--sk-surface, color-mix(in srgb, #ffffff 72%, transparent));
  backdrop-filter: blur(var(--sk-blur, 16px));
  -webkit-backdrop-filter: blur(var(--sk-blur, 16px));
  box-shadow: var(--sk-shadow, 0 12px 40px -12px rgba(16, 20, 24, 0.25));
  color: var(--sk-text, inherit);
}
button {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  padding: 8px 12px;
}
button:focus-visible {
  outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210));
  outline-offset: -2px;
}
.primary {
  border-start-start-radius: var(--sk-radius, 14px);
  border-end-start-radius: var(--sk-radius, 14px);
}
:host(:not([agent])) .primary {
  border-radius: var(--sk-radius, 14px);
}
.primary {
  transition: color var(--sk-motion-base, 180ms) ease, background-color var(--sk-motion-base, 180ms) ease;
}
.primary[data-indicator="listening"] {
  color: var(--sk-live, #dc2626);
  background: color-mix(in srgb, var(--sk-live, #dc2626) 9%, transparent);
}
.primary:is([data-indicator="processing"], [data-indicator="speaking"]) {
  color: var(--sk-accent, oklch(0.65 0.13 210));
}
.state-label {
  font-weight: 600;
  white-space: nowrap;
}
.state-label[hidden], .label[hidden] { display: none; }
:host([compact]) .state-label {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
}
@media (prefers-reduced-motion: reduce) {
  /* Without motion the visible label carries "you are being recorded". */
  :host([compact]) .state-label {
    position: static;
    width: auto;
    height: auto;
    overflow: visible;
    clip: auto;
  }
}
.agent {
  border-inline-start: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-start-end-radius: var(--sk-radius, 14px);
  border-end-end-radius: var(--sk-radius, 14px);
  padding: 8px 10px;
}
.agent[data-locked] {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
.chevron {
  display: inline-block;
  width: 0;
  height: 0;
  border-inline-start: 4px solid transparent;
  border-inline-end: 4px solid transparent;
  border-top: 5px solid currentColor;
}
.lock {
  font-size: var(--sk-font-size-small, 11px);
  line-height: 1;
}
.denial {
  position: absolute;
  inset-inline-end: 0;
  bottom: calc(100% + 6px);
  z-index: 1;
  width: max-content;
  max-width: min(92vw, 360px);
}
.denial[hidden] { display: none; }
.sr-only {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
${INDICATOR_CSS}
`;

/**
 * `<speechkit-voice-button>` — the standard split button (Voice Chat UI
 * Standard): primary segment starts/stops Dictation; the secondary segment
 * (chevron click or long-press on the primary) starts the Voice Agent. The
 * secondary segment exists only when the host sets the `agent` attribute;
 * without the `voice_agent` capability it renders locked and activation
 * surfaces the denial guidance instead of disappearing (fail-closed,
 * FEATURE-ENTITLEMENT-UX-STANDARD).
 *
 * The primary segment carries the recording indicator (idle, requesting,
 * listening with live level bars, processing, speaking, error) and, while a
 * session is active, a short state label instead of the slotted text
 * (`compact` keeps the label for assistive tech only, except under reduced
 * motion). Any denial renders as a compact `speechkit-voice-notice` above the
 * control (`no-notice` opts out).
 */
export class SpeechKitVoiceButtonElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-button";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "agent", "disabled", "for", "compact", "no-notice"];
  }

  #primary: HTMLButtonElement;
  #agentButton: HTMLButtonElement;
  #denialPopover: HTMLDivElement;
  #notice: SpeechKitVoiceNoticeElement;
  #indicator = new RecordingIndicator();
  #label: HTMLSpanElement;
  #stateLabel: HTMLSpanElement;
  #requesting = false;
  #requestingTimer: ReturnType<typeof setTimeout> | undefined;
  #unsubscribeLevel: (() => void) | undefined;
  #seenEvents = 0;
  #longPressTimer: ReturnType<typeof setTimeout> | undefined;
  #longPressFired = false;
  #localDenial: SpeechKitVoiceDenial | undefined;

  constructor() {
    super(CSS);
    const wrap = document.createElement("div");
    wrap.className = "split";
    wrap.setAttribute("part", "button");

    this.#primary = document.createElement("button");
    this.#primary.type = "button";
    this.#primary.className = "primary";
    this.#primary.setAttribute("part", "primary");
    this.#primary.addEventListener("click", () => this.#onPrimary());
    this.#primary.addEventListener("pointerdown", (event) => this.#onPointerDown(event));
    this.#primary.addEventListener("pointerup", () => this.#clearLongPress());
    this.#primary.addEventListener("pointerleave", () => this.#clearLongPress());
    this.#primary.addEventListener("contextmenu", (event) => {
      if (this.#longPressTimer !== undefined || this.#longPressFired) event.preventDefault();
    });

    this.#agentButton = document.createElement("button");
    this.#agentButton.type = "button";
    this.#agentButton.className = "agent";
    this.#agentButton.setAttribute("part", "agent");
    this.#agentButton.addEventListener("click", () => this.#onAgent());

    this.#label = document.createElement("span");
    this.#label.className = "label";
    this.#label.append(document.createElement("slot"));
    this.#stateLabel = document.createElement("span");
    this.#stateLabel.className = "state-label";
    this.#stateLabel.setAttribute("part", "state-label");
    this.#stateLabel.hidden = true;
    this.#primary.append(this.#indicator.element, this.#label, this.#stateLabel);

    this.#denialPopover = document.createElement("div");
    this.#denialPopover.className = "denial";
    this.#denialPopover.setAttribute("part", "denial");
    this.#denialPopover.hidden = true;
    this.#notice = document.createElement(
      SpeechKitVoiceNoticeElement.tagName
    ) as SpeechKitVoiceNoticeElement;
    this.#notice.addEventListener("speechkit-dismiss", () => this.#hideDenial());
    this.#notice.addEventListener("speechkit-retry", () => this.#hideDenial());
    this.#denialPopover.append(this.#notice);

    wrap.append(this.#primary, this.#agentButton, this.#denialPopover);
    this.root.append(wrap);
  }

  #status(): SpeechKitVoiceSessionState["status"] {
    return this.state?.status ?? this.controller?.getState().status ?? "idle";
  }

  #agentCapability(): boolean {
    return this.controller?.contract.capabilities.voice_agent ?? false;
  }

  #onPrimary(): void {
    if (this.#longPressFired) {
      this.#longPressFired = false;
      return; // the long-press already dispatched the agent action
    }
    this.#hideDenial();
    const controller = this.controller;
    if (!controller) return;
    const status = this.#status();
    if (status === "capturing") {
      void controller.stop();
    } else if (status === "processing") {
      controller.cancel();
    } else if (!isVoiceSessionActive(status)) {
      this.#beginRequesting(controller.start("dictation"));
    }
  }

  /** Kit-local `requesting` state until the controller reports progress. */
  #beginRequesting(result: void | Promise<void>): void {
    this.#setRequesting(true);
    if (result && typeof (result as Promise<void>).catch === "function") {
      (result as Promise<void>).catch(() => this.#setRequesting(false));
    }
  }

  #setRequesting(value: boolean): void {
    if (this.#requestingTimer !== undefined) clearTimeout(this.#requestingTimer);
    this.#requestingTimer = undefined;
    if (value) {
      this.#requestingTimer = setTimeout(() => this.#setRequesting(false), REQUESTING_TIMEOUT_MS);
    }
    if (this.#requesting === value) return;
    this.#requesting = value;
    this.requestUpdate();
  }

  /** Current recording indicator state (read-only mirror of the control). */
  get indicatorState(): RecordingIndicatorState {
    return this.#indicator.state;
  }

  #onPointerDown(event: PointerEvent): void {
    if (event.button !== 0 || !this.hasAttribute("agent")) return;
    this.#clearLongPress();
    this.#longPressTimer = setTimeout(() => {
      this.#longPressTimer = undefined;
      this.#longPressFired = true;
      this.#onAgent();
    }, LONG_PRESS_MS);
  }

  #clearLongPress(): void {
    if (this.#longPressTimer !== undefined) {
      clearTimeout(this.#longPressTimer);
      this.#longPressTimer = undefined;
    }
  }

  #onAgent(): void {
    this.emitKitEvent("speechkit-agent-request");
    const controller = this.controller;
    if (!this.#agentCapability()) {
      // Fail-closed guidance path: the canonical denial comes from the
      // controller's own start() gate; without a controller a local denial
      // keeps the popover honest.
      if (controller) {
        void controller.start("voice_agent");
      } else {
        this.#localDenial = missingControllerDenial();
        this.requestUpdate();
      }
      this.#showDenialSoon();
      return;
    }
    // With a `for` target the overlay orchestrates the session (consent gate
    // first, then start); only without one does the button start directly —
    // the controller's own consent precondition still applies there.
    if (this.#openTargetOverlay()) return;
    if (controller) this.#beginRequesting(controller.start("voice_agent"));
  }

  #openTargetOverlay(): boolean {
    const targetId = this.getAttribute("for");
    if (!targetId) return false;
    const overlay = (this.getRootNode() as Document | ShadowRoot).getElementById?.(targetId);
    if (overlay && "show" in overlay && typeof (overlay as { show: unknown }).show === "function") {
      (overlay as unknown as { show(): void }).show();
      return true;
    }
    return false;
  }

  #showDenialSoon(): void {
    // The controller emits voice.denied synchronously from start(); render on
    // the microtask so the popover picks the denial up either way.
    queueMicrotask(() => this.requestUpdate());
  }

  #hideDenial(): void {
    this.#localDenial = undefined;
    this.#dismissedAt = this.state?.events.length ?? 0;
    if (!this.#denialPopover.hidden) {
      this.#denialPopover.hidden = true;
    }
  }

  #dismissedAt = -1;

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

  override connectedCallback(): void {
    super.connectedCallback();
    this.#indicator.resume();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#indicator.stop();
    this.#setRequesting(false);
  }

  protected override onSessionState(state: SpeechKitVoiceSessionState): void {
    if (state.events.length > this.#seenEvents) this.#setRequesting(false);
    for (let i = this.#seenEvents; i < state.events.length; i += 1) {
      const event = state.events[i];
      if (event === undefined) continue;
      this.emitKitEvent<SpeechKitVoiceEvent>("speechkit-voice-event", event);
      if (event.type === "voice.transcript_draft" || event.type === "voice.transcript_final") {
        this.emitKitEvent("speechkit-transcript", {
          text: event.text ?? "",
          final: event.type === "voice.transcript_final"
        });
      } else if (event.type === "voice.denied" && event.error) {
        this.emitKitEvent<SpeechKitVoiceDenial>("speechkit-denied", event.error);
      }
    }
    this.#seenEvents = state.events.length;
  }

  protected override render(): void {
    const messages = this.msgs();
    const status = this.#status();
    const capturing = status === "capturing";
    const disabled = this.hasAttribute("disabled");

    this.#primary.disabled = disabled;
    this.#primary.dataset.status = status;
    const indicator = sessionStatusToIndicatorState(status, this.#requesting);
    this.#indicator.setState(indicator);
    this.#primary.dataset.indicator = indicator;
    this.#primary.toggleAttribute("aria-busy", indicator === "requesting" || indicator === "processing");
    const stateText =
      indicator === "requesting"
        ? messages["sk.voice.state.requesting"]
        : indicator === "listening"
          ? messages["sk.voice.state.capturing"]
          : indicator === "processing"
            ? messages["sk.voice.state.processing"]
            : indicator === "speaking"
              ? messages["sk.voice.state.speaking"]
              : "";
    this.#stateLabel.textContent = stateText;
    this.#stateLabel.hidden = stateText === "";
    this.#label.hidden = stateText !== "";
    const primaryLabel = capturing
      ? messages["sk.voice.button.dictation.stop"]
      : messages["sk.voice.button.dictation.start"];
    this.#primary.setAttribute("aria-label", stateText ? `${primaryLabel} (${stateText})` : primaryLabel);
    this.#primary.title = primaryLabel;

    const wantsAgent = this.hasAttribute("agent");
    const capability = this.#agentCapability();
    this.#agentButton.hidden = !wantsAgent;
    if (wantsAgent) {
      const locked = !capability;
      this.#agentButton.replaceChildren();
      if (locked) {
        this.#agentButton.dataset.locked = "";
        const lock = document.createElement("span");
        lock.className = "lock";
        lock.setAttribute("aria-hidden", "true");
        lock.textContent = "\u{1F512}";
        this.#agentButton.append(lock);
        this.#agentButton.setAttribute("aria-label", messages["sk.voice.button.agent_locked"]);
        this.#agentButton.title = messages["sk.voice.button.agent_locked"];
      } else {
        delete this.#agentButton.dataset.locked;
        const chevron = document.createElement("span");
        chevron.className = "chevron";
        chevron.setAttribute("aria-hidden", "true");
        const iconSlot = document.createElement("slot");
        iconSlot.name = "agent-icon";
        this.#agentButton.append(iconSlot, chevron);
        this.#agentButton.setAttribute("aria-label", messages["sk.voice.button.agent"]);
        this.#agentButton.title = messages["sk.voice.button.agent"];
      }
    }

    const denial = this.#localDenial ?? (status === "denied" ? this.state?.denial : undefined);
    const dismissed = !this.#localDenial && this.#dismissedAt === (this.state?.events.length ?? 0);
    if (denial && !dismissed && !this.hasAttribute("no-notice")) {
      this.#notice.denial = denial;
      const locale = this.getAttribute("locale");
      if (locale) this.#notice.setAttribute("locale", locale);
      this.#denialPopover.hidden = false;
    } else {
      this.#denialPopover.hidden = true;
    }
  }
}

function missingControllerDenial(): SpeechKitVoiceDenial {
  return {
    error_code: "voice_agent_unavailable",
    reason_code: "voice_agent_controller_missing",
    capability: "speechkit.voiceagent.live",
    required_features: [],
    missing_features: [],
    retryable: false,
    remediation: "Assign a VoiceUiController (or wrap in <speechkit-voice-provider>) before enabling the agent segment.",
    user_guidance: {
      title: "Voice conversation is not available",
      body: "This surface has no live voice controller, so no session or audio capture was started.",
      next_steps: ["Use dictation instead.", "Contact the host application if voice conversation should be available here."]
    }
  };
}
