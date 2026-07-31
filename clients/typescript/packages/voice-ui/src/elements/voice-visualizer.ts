import { SpeechKitElement } from "../core/element.js";
import type { VoiceUiController } from "../core/controller.js";
import type { SpeechKitVoiceSessionState } from "../core/voice-surface.js";

export type VoiceVisualizerState =
  | "idle"
  | "connecting"
  | "listening"
  | "processing"
  | "speaking"
  | "error";

const VISUALIZER_STATES: readonly VoiceVisualizerState[] = [
  "idle",
  "connecting",
  "listening",
  "processing",
  "speaking",
  "error"
];

export function sessionStatusToVisualizerState(
  status: SpeechKitVoiceSessionState["status"]
): VoiceVisualizerState {
  switch (status) {
    case "capturing":
      return "listening";
    case "processing":
      return "processing";
    case "speaking":
      return "speaking";
    case "denied":
      return "error";
    case "idle":
    case "cancelled":
      return "idle";
  }
}

const CSS = `
:host {
  display: inline-grid;
  place-items: center;
}
.halo {
  display: grid;
  place-items: center;
  width: var(--sk-orb-size, 56px);
  height: var(--sk-orb-size, 56px);
  border-radius: var(--sk-radius-pill, 999px);
  background: color-mix(in srgb, var(--sk-accent, oklch(0.65 0.13 210)) 14%, transparent);
}
:host([state="speaking"]) .halo {
  background: color-mix(in srgb, var(--sk-accent, oklch(0.65 0.13 210)) 24%, transparent);
}
.core {
  width: var(--sk-orb-core-size, 22px);
  height: var(--sk-orb-core-size, 22px);
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-accent, oklch(0.65 0.13 210));
  transform: scale(calc(1 + var(--sk-voice-level, 0) * 0.85));
  transition: transform var(--sk-motion-fast, 80ms) linear;
}
:host([variant="pill"]) .halo {
  width: calc(var(--sk-orb-size, 56px) * 1.6);
  height: calc(var(--sk-orb-size, 56px) * 0.55);
}
:host([variant="dot"]) .halo {
  width: calc(var(--sk-orb-size, 56px) * 0.5);
  height: calc(var(--sk-orb-size, 56px) * 0.5);
}
:host([variant="dot"]) .core {
  width: calc(var(--sk-orb-core-size, 22px) * 0.6);
  height: calc(var(--sk-orb-core-size, 22px) * 0.6);
}
:host([state="connecting"]) .core {
  animation: sk-breathe 1.2s ease-in-out infinite;
  opacity: 0.4;
}
:host([state="listening"]) .core {
  animation: sk-breathe var(--sk-motion-slow, 1600ms) ease-in-out infinite;
}
:host([state="processing"]) .core {
  animation: sk-breathe 0.9s ease-in-out infinite;
  opacity: 0.75;
}
:host([state="speaking"]) .core {
  animation: sk-pulse 0.8s ease-in-out infinite;
}
:host([state="error"]) .core {
  animation: none;
  opacity: 0.5;
}
@keyframes sk-breathe {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.55; }
}
@keyframes sk-pulse {
  0%, 100% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--sk-accent, oklch(0.65 0.13 210)) 45%, transparent); }
  50% { box-shadow: 0 0 0 10px transparent; }
}
@media (prefers-reduced-motion: reduce) {
  .core {
    animation: none !important;
    transform: none;
    transition: none;
  }
}
:host([static]) .core {
  animation: none !important;
  transform: none;
  transition: none;
}
`;

/**
 * `<speechkit-voice-visualizer>` — orb/pill/dot audio-state indicator.
 * Attribute-driven standalone (`state`, `level`) or controller-driven when a
 * {@link VoiceUiController} is assigned/provided (status mapping per
 * `sessionStatusToVisualizerState`; level via `subscribeLevel`).
 */
export class SpeechKitVoiceVisualizerElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-visualizer";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "variant", "state", "level", "static"];
  }

  #unsubscribeLevel: (() => void) | undefined;
  #halo: HTMLElement;
  #core: HTMLElement;

  constructor() {
    super(CSS);
    this.#halo = document.createElement("div");
    this.#halo.className = "halo";
    this.#halo.setAttribute("part", "halo");
    this.#core = document.createElement("div");
    this.#core.className = "core";
    this.#core.setAttribute("part", "core");
    this.#core.setAttribute("aria-hidden", "true");
    this.#halo.append(this.#core);
    this.root.append(this.#halo);
  }

  /** Fast path for 60fps level writes without attribute churn. */
  set level(value: number) {
    const clamped = Math.min(1, Math.max(0, value));
    this.#core.style.setProperty("--sk-voice-level", clamped.toFixed(3));
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

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
  }

  protected override render(): void {
    // A live controller drives the state attribute; otherwise the host owns it
    // (CSS reacts to :host([state=...]) either way). Guarded write: setAttribute
    // with an unchanged value would still re-trigger attributeChangedCallback.
    if (this.state) {
      const next = sessionStatusToVisualizerState(this.state.status);
      if (this.getAttribute("state") !== next) this.setAttribute("state", next);
    } else {
      const explicit = this.getAttribute("state");
      if (explicit !== null && !(VISUALIZER_STATES as readonly string[]).includes(explicit)) {
        this.removeAttribute("state");
      }
    }
    const levelAttr = this.getAttribute("level");
    if (levelAttr !== null) {
      const parsed = Number.parseFloat(levelAttr);
      if (!Number.isNaN(parsed)) this.level = parsed;
    }
  }
}
