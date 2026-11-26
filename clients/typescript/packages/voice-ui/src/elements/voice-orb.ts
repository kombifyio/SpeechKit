import { SmoothedLevel } from "../core/level.js";
import { OpalRenderer, type OpalProfile, type OpalState } from "../core/opal.js";
import { VoiceMotifRenderer } from "../core/voice-motifs.js";
import { upgradeOwnProperties } from "../core/upgrade.js";

export type VoiceOrbVariant = "opal" | "resonance" | "rosette";
export type VoiceOrbState = OpalState;
export type VoiceOrbCore = OpalProfile;

/** A transport-independent voice visual. Hosts own its accessible label and controls. */
export class SpeechKitVoiceOrbElement extends HTMLElement {
  static readonly tagName = "speechkit-voice-orb";
  static readonly observedAttributes = ["variant", "state", "opal-core"];
  readonly #canvas = document.createElement("canvas");
  readonly #input = new SmoothedLevel();
  readonly #output = new SmoothedLevel();
  readonly #motion = matchMedia("(prefers-reduced-motion: reduce)");
  readonly #resize = new ResizeObserver(() => this.#wake());
  #visible = true;
  readonly #intersection = new IntersectionObserver(([entry]) => {
    this.#visible = entry?.isIntersecting ?? true;
    this.#wake();
  });
  #renderer: OpalRenderer | VoiceMotifRenderer | undefined;
  #frame = 0;
  #previousState: VoiceOrbState | undefined;
  #settleUntil = 0;

  constructor() {
    super();
    const root = this.attachShadow({ mode: "open" });
    const style = document.createElement("style");
    style.textContent = ":host{position:relative;display:inline-block;width:var(--sk-voice-orb-size,64px);aspect-ratio:1}canvas{display:block;width:100%;height:100%}::slotted([slot=mark]){position:absolute;inset:0;margin:auto;width:var(--sk-assistant-mark-ratio,34%);height:var(--sk-assistant-mark-ratio,34%);object-fit:contain;pointer-events:none}";
    this.#canvas.setAttribute("aria-hidden", "true");
    const mark = document.createElement("slot");
    mark.name = "mark";
    root.append(style, this.#canvas, mark);
  }

  get variant(): VoiceOrbVariant {
    const value = this.getAttribute("variant");
    return value === "resonance" || value === "rosette" ? value : "opal";
  }
  set variant(value: VoiceOrbVariant) { this.setAttribute("variant", value); }

  get opalCore(): VoiceOrbCore { return this.getAttribute("opal-core") === "small" ? "small" : "original"; }
  set opalCore(value: VoiceOrbCore) { this.setAttribute("opal-core", value); }

  get state(): VoiceOrbState {
    const state = this.getAttribute("state");
    switch (state) {
      case "connecting": case "listening": case "processing": case "speaking":
      case "recovering": case "settling": case "error": return state;
      default: return "inactive";
    }
  }
  set state(value: VoiceOrbState) { this.setAttribute("state", value); }

  setLevel(value: number, source: "input" | "output" = "input"): void {
    const level = source === "output" ? this.#output : this.#input;
    level.target = Number.isFinite(value) ? value : 0;
  }

  connectedCallback(): void {
    upgradeOwnProperties(this);
    this.#motion.addEventListener("change", this.#wake);
    this.ownerDocument.addEventListener("visibilitychange", this.#wake);
    this.#resize.observe(this);
    this.#intersection.observe(this);
    this.#wake();
  }
  disconnectedCallback(): void {
    this.#resize.disconnect();
    this.#intersection.disconnect();
    this.#motion.removeEventListener("change", this.#wake);
    this.ownerDocument.removeEventListener("visibilitychange", this.#wake);
    cancelAnimationFrame(this.#frame);
    this.#frame = 0;
  }

  attributeChangedCallback(name: string, before: string | null, after: string | null): void {
    if (before === after) return;
    if (name === "variant") this.#renderer = undefined;
    this.#wake();
  }

  #wake = (): void => {
    if (this.isConnected && this.#visible && !this.ownerDocument.hidden && !this.#frame) {
      this.#frame = requestAnimationFrame(this.#draw);
    }
  };

  #draw = (now: number): void => {
    this.#frame = 0;
    if (!this.#visible || this.ownerDocument.hidden || !this.#canvas.clientWidth) return;
    if (!this.#renderer) {
      if (!this.#canvas.getContext("2d")) return;
      this.#renderer = this.variant === "opal"
        ? new OpalRenderer(this.#canvas)
        : new VoiceMotifRenderer(this.#canvas, this.variant);
    }
    const state = this.state;
    const resting = state === "inactive" || state === "error";
    if (state !== this.#previousState) {
      // Finish the same colour/ribbon transition before putting a resting orb to sleep.
      this.#settleUntil = resting && this.#previousState !== undefined ? now + 1000 : 0;
      this.#previousState = state;
    }
    const reducedMotion = this.#motion.matches || (resting && this.#settleUntil === 0);
    this.#renderer.render({ now, state, profile: this.opalCore, inputLevel: this.#input.tick(now),
      outputLevel: this.#output.tick(now), reducedMotion });
    if (!this.#motion.matches && (!resting || now < this.#settleUntil)) this.#wake();
  };
}

/** Register only the standalone visual, for hosts that provide their own UI. */
export function registerSpeechKitVoiceOrb(): void {
  if (!customElements.get(SpeechKitVoiceOrbElement.tagName)) {
    customElements.define(SpeechKitVoiceOrbElement.tagName, SpeechKitVoiceOrbElement);
  }
}
