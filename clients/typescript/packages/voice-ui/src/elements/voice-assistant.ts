import { SpeechKitElement } from "../core/element.js";
import { SmoothedLevel } from "../core/level.js";
import type { VoiceUiController } from "../core/controller.js";
import { reduceVoiceAgentTurns, type VoiceAgentTurn } from "../core/turns.js";
import type {
  SpeechKitVoiceSessionState,
  SpeechKitVoiceSessionStatus
} from "../core/voice-surface.js";
import type { VoiceUiMessageCatalog } from "../i18n/index.js";
import { AssistantWaveformVisual, type WaveformLayout } from "./assistant-visuals/waveform.js";

export type VoiceAssistantSize = "orb" | "compact" | "expanded";
export type VoiceAssistantFrame = "overlay" | "keyboard" | "watch" | "phone" | "panel";

/**
 * Visual variants (decision 2026-08-13): `aura` is the default Voice
 * Assistant look; `waveform` (the lab's "Glass Waveform") is the supported
 * customization preset. The variant swaps only the motif in the visual slot;
 * shells, status, transcript, and interaction are identical.
 */
export type VoiceAssistantVariant = "aura" | "waveform";

/** `connecting` is the kit-local optimistic state (spec §Session status). */
export type VoiceAssistantStatus = SpeechKitVoiceSessionStatus | "connecting";

/**
 * Orb visual states. A superset of the session statuses: `recovering` and
 * `settling` exist only in host FSMs richer than the surface contract (the
 * Device-Target prompter drives them directly via `aura-state`).
 */
export type VoiceAuraState =
  | "inactive"
  | "connecting"
  | "listening"
  | "processing"
  | "speaking"
  | "recovering"
  | "settling"
  | "error";

const AURA_STATES: readonly VoiceAuraState[] = [
  "inactive",
  "connecting",
  "listening",
  "processing",
  "speaking",
  "recovering",
  "settling",
  "error"
];

export function sessionStatusToAuraState(status: VoiceAssistantStatus): VoiceAuraState {
  switch (status) {
    case "connecting":
      return "connecting";
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
      return "inactive";
  }
}

function isAuraState(value: string): value is VoiceAuraState {
  return (AURA_STATES as readonly string[]).includes(value);
}

const CSS = `
:host {
  display: block;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size, 13px);
  height: 100%;
}
:host([size="orb"]) {
  display: inline-grid;
  place-items: center;
  height: auto;
}
.va {
  height: 100%;
  display: flex;
  --level: 0;
  --aura-a: var(--sk-aura-inactive-a, 156, 163, 184);
  --aura-b: var(--sk-aura-inactive-b, 100, 116, 139);
  --tone: rgb(var(--aura-a));
  --tone-soft: rgba(var(--aura-a), 0.22);
}
.va[data-aura="connecting"] { --aura-a: var(--sk-aura-connecting-a, 56, 189, 248); --aura-b: var(--sk-aura-connecting-b, 129, 140, 248); }
.va[data-aura="listening"] { --aura-a: var(--sk-aura-listening-a, 52, 211, 153); --aura-b: var(--sk-aura-listening-b, 56, 189, 248); }
.va[data-aura="processing"] { --aura-a: var(--sk-aura-processing-a, 251, 191, 36); --aura-b: var(--sk-aura-processing-b, 244, 114, 182); }
.va[data-aura="speaking"] { --aura-a: var(--sk-aura-speaking-a, 129, 140, 248); --aura-b: var(--sk-aura-speaking-b, 167, 139, 250); }
.va[data-aura="recovering"] { --aura-a: var(--sk-aura-recovering-a, 34, 211, 238); --aura-b: var(--sk-aura-recovering-b, 129, 140, 248); }
.va[data-aura="settling"] { --aura-a: var(--sk-aura-settling-a, 52, 211, 153); --aura-b: var(--sk-aura-settling-b, 129, 140, 248); }
.va[data-aura="error"] { --aura-a: var(--sk-aura-error-a, 248, 113, 113); --aura-b: var(--sk-aura-error-b, 251, 113, 133); }

/* ── Orb ─────────────────────────────────────────────────────────
   Layer stack (outward → inward): breathing glow, aurora sweep,
   counter-rotating inner sweep, level-reactive halo ring, glassy
   core, centre spark, brand mark. Identical geometry and timings in
   the Compose and LVGL ports (tokens.json → assistant). */
.orb {
  position: relative;
  width: var(--sk-assistant-orb, 30px);
  height: var(--sk-assistant-orb, 30px);
  aspect-ratio: 1;
  flex: none;
  border: 0;
  background: transparent;
  padding: 0;
  cursor: default;
  font: inherit;
  color: inherit;
}
.va[data-status="speaking"] .orb { cursor: pointer; }
.orb:focus-visible { outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210)); outline-offset: 2px; border-radius: 50%; }
.orb > * { position: absolute; border-radius: 50%; }
.orb .glow {
  inset: 0;
  background: radial-gradient(circle at 50% 50%, rgba(var(--aura-a), 0.38), rgba(var(--aura-a), 0.08) 55%, rgba(var(--aura-a), 0) 72%);
  animation: sk-aura-breathe 4.5s ease-in-out infinite;
}
.orb .sweep {
  inset: 6%;
  overflow: hidden;
  opacity: 0.9;
  background: conic-gradient(from 0deg, rgba(var(--aura-a), 0) 0deg, rgba(var(--aura-a), 0.55) 90deg, rgba(var(--aura-b), 0) 200deg, rgba(var(--aura-b), 0.45) 300deg, rgba(var(--aura-a), 0) 360deg);
  filter: blur(6px);
  animation: sk-aura-spin 9s linear infinite;
}
.orb .sweep-inner {
  inset: 16%;
  overflow: hidden;
  opacity: 0.8;
  background: conic-gradient(from 180deg, rgba(var(--aura-b), 0) 0deg, rgba(var(--aura-b), 0.4) 120deg, rgba(var(--aura-a), 0) 260deg);
  filter: blur(4px);
  animation: sk-aura-spin-reverse 13s linear infinite;
}
.orb .halo {
  inset: 10%;
  border: 1px solid rgba(var(--aura-a), 0.5);
  box-shadow: 0 0 18px rgba(var(--aura-a), 0.35) inset, 0 0 12px rgba(var(--aura-b), 0.28);
  transform: scale(calc(0.82 + var(--level) * 0.3));
  opacity: calc(0.35 + var(--level) * 0.5);
  transition: transform 120ms ease-out, opacity 200ms ease-out;
}
.orb .core {
  inset: 26%;
  backdrop-filter: blur(2px);
  -webkit-backdrop-filter: blur(2px);
  background: radial-gradient(circle at 38% 32%, rgba(255, 255, 255, 0.3), rgba(255, 255, 255, 0.06) 46%, rgba(255, 255, 255, 0.02) 70%);
  border: 1px solid rgba(255, 255, 255, 0.14);
  animation: sk-aura-core 3.2s ease-in-out infinite;
}
/* Centre spark — backlight for the brand mark. */
.orb .spark {
  left: 50%;
  top: 50%;
  width: 12%;
  height: 12%;
  transform: translate(-50%, -50%);
  background: radial-gradient(circle, rgba(var(--aura-a), 0.95), rgba(var(--aura-a), 0.2) 70%, rgba(var(--aura-a), 0) 100%);
  opacity: 0.85;
}
/* Resting states: no motion, thinned effects. */
.va:is([data-aura="inactive"], [data-aura="error"]) .orb :is(.glow, .sweep, .sweep-inner, .core) { animation: none; }
.va:is([data-aura="inactive"], [data-aura="error"]) .orb .halo { opacity: 0.18; box-shadow: 0 0 18px rgba(var(--aura-a), 0.12) inset, 0 0 12px rgba(var(--aura-b), 0.08); }
.va:is([data-aura="inactive"], [data-aura="error"]) .orb .spark { opacity: 0.4; }

@keyframes sk-aura-spin { to { transform: rotate(360deg); } }
@keyframes sk-aura-spin-reverse { to { transform: rotate(-360deg); } }
@keyframes sk-aura-breathe {
  0%, 100% { transform: scale(0.92); opacity: 0.55; }
  50% { transform: scale(1.06); opacity: 0.9; }
}
@keyframes sk-aura-core {
  0%, 100% { opacity: 0.78; }
  50% { opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  .orb > *, .mark img { animation: none !important; transition: none !important; }
  .orb .halo, .mark img { transform: none; }
  .orb .spark { transform: translate(-50%, -50%); }
}

/* ── Brand mark (host-provided via mark-src) ─────────────────────
   Part of the motion system, not a sticker: greyscale ghost while
   resting, full colour plus level-driven glow and scale when live. */
.mark {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  pointer-events: none;
}
.mark img {
  width: var(--sk-assistant-mark-ratio, 34%);
  height: var(--sk-assistant-mark-ratio, 34%);
  object-fit: contain;
  filter: saturate(1.15) drop-shadow(0 0 calc(4px + var(--level) * 12px) rgba(var(--aura-a), calc(0.35 + var(--level) * 0.4)));
  opacity: 1;
  transform: scale(calc(1 + var(--level) * 0.08));
  transition: filter 420ms ease, opacity 420ms ease, transform 420ms ease;
}
.va:is([data-aura="inactive"], [data-aura="error"]) .mark img {
  filter: grayscale(1) brightness(0.95);
  opacity: 0.5;
  transform: none;
}

/* ── Waveform variant (variant="waveform") ───────────────────────
   The dual-hue level-history motif in the orb's slot; drawing spec
   in tokens.json → assistant-variants.waveform. */
.wave-visual {
  position: relative;
  flex: none;
  border: 0;
  background: transparent;
  padding: 0;
  cursor: default;
  font: inherit;
  color: inherit;
}
.va[data-status="speaking"] .wave-visual { cursor: pointer; }
.wave-visual:focus-visible { outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210)); outline-offset: 2px; border-radius: 8px; }
.wave-visual canvas { display: block; width: 100%; height: 100%; }
.wave-visual[data-layout="radial"] {
  width: var(--sk-assistant-orb, 30px);
  height: var(--sk-assistant-orb, 30px);
  border-radius: 50%;
}
.wave-visual[data-layout="linear"] {
  width: var(--sk-assistant-wave-compact-w, 72px);
  height: var(--sk-assistant-wave-compact-h, 22px);
  border-radius: 6px;
}
.va[data-frame="keyboard"] .wave-visual[data-layout="linear"] { flex: 1; width: auto; }

/* ── Compact: pill (overlay/phone/panel) / bar (keyboard) ────── */
.compact-shell {
  display: flex; align-items: center; gap: 10px;
  padding: 8px 14px 8px 9px;
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-surface, color-mix(in srgb, #ffffff 72%, transparent));
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  backdrop-filter: blur(var(--sk-blur, 16px));
  -webkit-backdrop-filter: blur(var(--sk-blur, 16px));
  box-shadow: var(--sk-shadow, 0 12px 40px -12px rgba(16, 20, 24, 0.25));
  min-width: 0; max-width: 100%;
  margin: auto;
}
.va[data-frame="keyboard"] .compact-shell { width: 100%; border-radius: var(--sk-radius, 14px); margin: 0; }
.compact-line { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.compact-line .who { color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent)); font-size: var(--sk-font-size-small, 11px); margin-right: 6px; text-transform: uppercase; letter-spacing: 0.08em; }
.status-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--tone); flex: none; }
.va:is([data-status="capturing"], [data-status="speaking"]) .status-dot { animation: sk-aura-pulse 1.2s ease-in-out infinite; }
@keyframes sk-aura-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.35; } }
@media (prefers-reduced-motion: reduce) { .status-dot { animation: none; } }

/* ── Compact: watch face ─────────────────────────────────────── */
.watch-shell { margin: auto; display: flex; flex-direction: column; align-items: center; gap: 14px; text-align: center; padding: 12px; max-width: 100%; }
.watch-shell :is(.orb, .wave-visual) { --sk-assistant-orb: var(--sk-assistant-orb-watch, 84px); }
.watch-line { font-size: 13px; line-height: 1.35; max-height: 3.9em; overflow: hidden; color: var(--sk-text, inherit); }
.watch-status { font-size: var(--sk-font-size-small, 11px); color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent)); text-transform: uppercase; letter-spacing: 0.14em; }

/* ── Expanded: hero + turn list ──────────────────────────────── */
.expanded-shell { display: flex; flex-direction: column; height: 100%; width: 100%; background: var(--sk-surface, color-mix(in srgb, #ffffff 72%, transparent)); backdrop-filter: blur(var(--sk-blur, 16px)); -webkit-backdrop-filter: blur(var(--sk-blur, 16px)); }
.hero { display: flex; flex-direction: column; align-items: center; gap: 12px; padding: 26px 16px 18px; }
.hero :is(.orb, .wave-visual) { --sk-assistant-orb: var(--sk-assistant-orb-hero, 92px); }
.status-pill {
  display: inline-flex; align-items: center; gap: 7px;
  padding: 4px 12px; border-radius: var(--sk-radius-pill, 999px);
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  background: var(--sk-surface-strong, color-mix(in srgb, #ffffff 92%, transparent));
  font-size: var(--sk-font-size-small, 11px);
}
.turns-wrap { position: relative; display: flex; flex: 1; min-height: 0; }
.turns { flex: 1; overflow-y: auto; padding: 4px 18px 14px; display: flex; flex-direction: column; gap: 2px; scrollbar-width: thin; }
.turn { padding: 9px 0; border-top: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent)); }
.turn:first-child { border-top: none; }
.turn-head { display: flex; align-items: center; gap: 7px; margin-bottom: 3px; }
.turn-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--sk-assistant-user-dot, #8fa9ff); }
.turn[data-role="agent"] .turn-dot { background: var(--sk-assistant-agent-dot, #47e3c1); }
.turn-role { font-size: 10px; letter-spacing: 0.12em; text-transform: uppercase; color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent)); }
.turn-flag { font-size: 10px; color: var(--sk-live, #dc2626); border: 1px solid color-mix(in srgb, var(--sk-live, #dc2626) 40%, transparent); border-radius: var(--sk-radius-pill, 999px); padding: 0 6px; }
.turn-text { margin: 0; line-height: 1.45; overflow-wrap: anywhere; }
.turn[data-draft] .turn-text { color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent)); }
.turn[data-draft] .turn-text::after { content: "\\258d"; color: var(--tone); animation: sk-aura-pulse 1s ease-in-out infinite; }
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
:host(:dir(rtl)) .jump { transform: translateX(50%); }
.hidden { display: none !important; }
`;

/**
 * `<speechkit-voice-assistant>` — the default Voice Assistant surface
 * ("Aura Orb", decision 2026-08-10).
 *
 * One element serves every kit frame: `size="orb"` renders the bare orb for
 * hosts that own their own chrome (Device-Target prompter, Android overlay);
 * `compact` renders the pill (overlay/phone/panel), inline bar (keyboard), or
 * watch face; `expanded` renders the hero orb with status pill and
 * teleprompter turn list. `transcript` toggles text rendering (compact shows
 * at most the last sentence). `variant` selects the motif in the visual slot:
 * the Aura orb (default) or the Glass Waveform (`waveform`); everything else
 * is identical across variants. `mark-src` places a host-provided brand image
 * in the orb centre, state-coupled (resting greys out, activity restores
 * colour, speech level breathes glow/scale) — the kit itself ships no brand
 * asset, and the waveform variant has no mark slot.
 *
 * Controller-driven by default (state via `subscribe`, levels via
 * `subscribeLevel`); hosts with their own reducer or a richer FSM set the
 * presentational overrides (`turns`, `status`, `aura-state`/`auraState`,
 * `level`/`setLevel`). Orb tap while `speaking` is barge-in per the spec;
 * inert otherwise.
 */
export class SpeechKitVoiceAssistantElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-assistant";

  static override get observedAttributes(): string[] {
    return [
      ...super.observedAttributes,
      "size",
      "transcript",
      "frame",
      "variant",
      "mark-src",
      "aura-state"
    ];
  }

  #turns: VoiceAgentTurn[] = [];
  #turnsOverride: VoiceAgentTurn[] | undefined;
  #statusOverride: VoiceAssistantStatus | undefined;
  #auraOverride: VoiceAuraState | undefined;
  #seenEvents = 0;

  readonly #inputLevel = new SmoothedLevel();
  readonly #outputLevel = new SmoothedLevel();
  #unsubscribeLevel: (() => void) | undefined;
  #rafId = 0;

  #wrap: HTMLElement | null = null;
  #compactLine: HTMLElement | null = null;
  #watchLine: HTMLElement | null = null;
  #watchStatus: HTMLElement | null = null;
  #statusPill: HTMLElement | null = null;
  #turnScroller: HTMLElement | null = null;
  #turnsWrap: HTMLElement | null = null;
  #jump: HTMLButtonElement | null = null;
  #orbButtons: HTMLButtonElement[] = [];
  #waveforms: AssistantWaveformVisual[] = [];
  #skeletonKey = "";
  #renderedTurnsRef: unknown = null;
  #renderedTurnCount = -1;
  #pinned = true;
  #programmaticScroll = false;

  constructor() {
    super(CSS);
  }

  // ----- attribute contract -------------------------------------------------

  // Each of these reflects to its attribute, because frameworks that bind to
  // custom elements (React 19, Vue, Svelte) assign properties whenever one
  // exists and throw on getter-only accessors.

  get size(): VoiceAssistantSize {
    const value = this.getAttribute("size");
    if (value === "expanded" || value === "orb") return value;
    return "compact";
  }

  set size(value: VoiceAssistantSize) {
    this.setAttribute("size", value);
  }

  get transcriptEnabled(): boolean {
    return this.hasAttribute("transcript");
  }

  set transcriptEnabled(value: boolean) {
    this.toggleAttribute("transcript", value);
  }

  get frame(): VoiceAssistantFrame {
    const value = this.getAttribute("frame");
    if (value === "overlay" || value === "keyboard" || value === "watch" || value === "phone") {
      return value;
    }
    return "panel";
  }

  set frame(value: VoiceAssistantFrame) {
    this.setAttribute("frame", value);
  }

  get variant(): VoiceAssistantVariant {
    return this.getAttribute("variant") === "waveform" ? "waveform" : "aura";
  }

  set variant(value: VoiceAssistantVariant) {
    this.setAttribute("variant", value);
  }

  /** Host-provided brand mark URL; absent renders the pure orb. */
  get markSrc(): string | null {
    return this.getAttribute("mark-src");
  }

  set markSrc(value: string | null) {
    if (value === null) this.removeAttribute("mark-src");
    else this.setAttribute("mark-src", value);
  }

  // ----- presentational overrides -------------------------------------------

  get turns(): readonly VoiceAgentTurn[] {
    return this.#turnsOverride ?? this.#turns;
  }

  set turns(value: VoiceAgentTurn[] | undefined) {
    this.#turnsOverride = value;
    this.requestUpdate();
  }

  set status(value: VoiceAssistantStatus | undefined) {
    this.#statusOverride = value;
    this.requestUpdate();
  }

  /**
   * Host override for the orb visual, including the states the surface
   * contract has no equivalent for (`recovering`, `settling`).
   */
  get auraState(): VoiceAuraState {
    if (this.#auraOverride) return this.#auraOverride;
    const attr = this.getAttribute("aura-state");
    if (attr !== null && isAuraState(attr)) return attr;
    return sessionStatusToAuraState(this.#status());
  }

  set auraState(value: VoiceAuraState | undefined) {
    this.#auraOverride = value;
    this.requestUpdate();
  }

  /** Fast path for host-driven input level (0..1); no attribute churn. */
  set level(value: number) {
    this.#inputLevel.target = value;
  }

  /** Host-driven level with an explicit source channel. */
  setLevel(value: number, source: "input" | "output" = "input"): void {
    if (source === "output") this.#outputLevel.target = value;
    else this.#inputLevel.target = value;
  }

  // ----- controller wiring --------------------------------------------------

  protected override onControllerChanged(controller: VoiceUiController | null): void {
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
    if (controller?.subscribeLevel) {
      this.#unsubscribeLevel = controller.subscribeLevel((level, source) => {
        if (source === "output") this.#outputLevel.target = level;
        else this.#inputLevel.target = level;
      });
    }
  }

  protected override onSessionState(state: SpeechKitVoiceSessionState): void {
    if (state.events.length < this.#seenEvents) {
      // The controller reset its session state (fresh start) — start over.
      this.#turns = [];
      this.#seenEvents = 0;
    }
    for (let i = this.#seenEvents; i < state.events.length; i += 1) {
      const event = state.events[i];
      if (event !== undefined) this.#turns = reduceVoiceAgentTurns(this.#turns, event);
    }
    this.#seenEvents = state.events.length;
  }

  // ----- level animation loop -----------------------------------------------

  static #reducedMotionQuery: MediaQueryList | null | undefined;

  static #reducedMotion(): boolean {
    if (SpeechKitVoiceAssistantElement.#reducedMotionQuery === undefined) {
      SpeechKitVoiceAssistantElement.#reducedMotionQuery =
        typeof matchMedia === "function" ? matchMedia("(prefers-reduced-motion: reduce)") : null;
    }
    return SpeechKitVoiceAssistantElement.#reducedMotionQuery?.matches ?? false;
  }

  #loop = (now: number): void => {
    const input = this.#inputLevel.tick(now);
    const output = this.#outputLevel.tick(now);
    this.#wrap?.style.setProperty("--level", Math.max(input, output).toFixed(3));
    if (this.#waveforms.length > 0) {
      const aura = this.auraState;
      const options = {
        active: aura !== "inactive" && aura !== "error",
        reducedMotion: SpeechKitVoiceAssistantElement.#reducedMotion()
      };
      for (const wave of this.#waveforms) wave.onFrame(input, output, now, options);
    }
    this.#rafId = requestAnimationFrame(this.#loop);
  };

  override connectedCallback(): void {
    super.connectedCallback();
    this.#rafId = requestAnimationFrame(this.#loop);
  }

  override disconnectedCallback(): void {
    cancelAnimationFrame(this.#rafId);
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
    super.disconnectedCallback();
  }

  // ----- interaction --------------------------------------------------------

  #interrupt(): void {
    if (this.#status() !== "speaking") return;
    this.controller?.interrupt?.();
    this.emitKitEvent("speechkit-interrupt");
  }

  // ----- scroll bookkeeping (spec §Teleprompter turn rules) -----------------

  #handleScroll(): void {
    if (this.#programmaticScroll) {
      this.#programmaticScroll = false;
      return;
    }
    const scroller = this.#turnScroller;
    if (!scroller) return;
    const nextPinned = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 24;
    if (nextPinned !== this.#pinned) {
      this.#pinned = nextPinned;
      if (this.#jump) this.#jump.hidden = nextPinned;
    }
  }

  #scrollToLive(): void {
    const scroller = this.#turnScroller;
    if (!scroller) return;
    const target = scroller.scrollHeight - scroller.clientHeight;
    if (Math.abs(scroller.scrollTop - target) < 1) return;
    this.#programmaticScroll = true;
    scroller.scrollTop = scroller.scrollHeight;
  }

  #jumpToLive(): void {
    this.#pinned = true;
    if (this.#jump) this.#jump.hidden = true;
    this.#scrollToLive();
  }

  // ----- rendering ----------------------------------------------------------

  #status(): VoiceAssistantStatus {
    if (this.#statusOverride) return this.#statusOverride;
    if (this.state) return this.state.status;
    return this.controller ? "connecting" : "idle";
  }

  #statusLabel(status: VoiceAssistantStatus, messages: VoiceUiMessageCatalog): string {
    if (status === "connecting") return messages["sk.voice.agent.connecting"];
    return messages[`sk.voice.state.${status}`];
  }

  #orb(): HTMLElement {
    const orb = document.createElement("button");
    orb.type = "button";
    orb.className = "orb";
    orb.setAttribute("part", "orb");
    orb.addEventListener("click", () => this.#interrupt());
    for (const layer of ["glow", "sweep", "sweep-inner", "halo", "core", "spark"]) {
      const node = document.createElement("div");
      node.className = layer;
      orb.append(node);
    }
    const markSrc = this.markSrc;
    if (markSrc) {
      const mark = document.createElement("div");
      mark.className = "mark";
      mark.setAttribute("part", "mark");
      const img = document.createElement("img");
      img.src = markSrc;
      img.alt = "";
      img.setAttribute("aria-hidden", "true");
      mark.append(img);
      orb.append(mark);
    }
    this.#orbButtons.push(orb);
    return orb;
  }

  /**
   * The variant's motif for the visual slot: the Aura orb (default) or the
   * waveform canvas. `layout` only matters for the waveform — round slots
   * (bare orb, watch face, hero) draw the radial rim, the compact pill and
   * keyboard bar draw the linear strip.
   */
  #visual(layout: WaveformLayout): HTMLElement {
    if (this.variant === "waveform") {
      const wave = new AssistantWaveformVisual(layout);
      wave.element.addEventListener("click", () => this.#interrupt());
      this.#orbButtons.push(wave.element);
      this.#waveforms.push(wave);
      return wave.element;
    }
    return this.#orb();
  }

  #skeleton(): HTMLElement {
    const wrap = document.createElement("div");
    wrap.className = "va";
    wrap.setAttribute("part", "assistant");
    this.#orbButtons = [];
    this.#waveforms = [];

    if (this.size === "orb") {
      wrap.append(this.#visual("radial"));
      return wrap;
    }

    if (this.size === "expanded") {
      const shell = document.createElement("div");
      shell.className = "expanded-shell";
      const hero = document.createElement("div");
      hero.className = "hero";
      hero.setAttribute("part", "hero");
      hero.append(this.#visual("radial"));
      this.#statusPill = document.createElement("span");
      this.#statusPill.className = "status-pill";
      this.#statusPill.setAttribute("part", "status");
      this.#statusPill.setAttribute("role", "status");
      const dot = document.createElement("span");
      dot.className = "status-dot";
      dot.setAttribute("aria-hidden", "true");
      const text = document.createElement("span");
      text.className = "status-text";
      this.#statusPill.append(dot, text);
      hero.append(this.#statusPill);

      this.#turnsWrap = document.createElement("div");
      this.#turnsWrap.className = "turns-wrap";
      this.#turnScroller = document.createElement("div");
      this.#turnScroller.className = "turns";
      this.#turnScroller.setAttribute("part", "transcript");
      this.#turnScroller.setAttribute("aria-live", "polite");
      this.#turnScroller.addEventListener("scroll", () => this.#handleScroll());
      this.#jump = document.createElement("button");
      this.#jump.type = "button";
      this.#jump.className = "jump";
      this.#jump.setAttribute("part", "jump-to-live");
      this.#jump.hidden = true;
      this.#jump.addEventListener("click", () => this.#jumpToLive());
      this.#turnsWrap.append(this.#turnScroller, this.#jump);

      shell.append(hero, this.#turnsWrap);
      wrap.append(shell);
    } else if (this.frame === "watch") {
      const shell = document.createElement("div");
      shell.className = "watch-shell";
      this.#watchStatus = document.createElement("div");
      this.#watchStatus.className = "watch-status";
      this.#watchStatus.setAttribute("part", "status");
      this.#watchStatus.setAttribute("role", "status");
      this.#watchLine = document.createElement("div");
      this.#watchLine.className = "watch-line";
      this.#watchLine.setAttribute("aria-live", "polite");
      shell.append(this.#visual("radial"), this.#watchStatus, this.#watchLine);
      wrap.append(shell);
    } else {
      const shell = document.createElement("div");
      shell.className = "compact-shell";
      shell.setAttribute("part", "shell");
      this.#compactLine = document.createElement("div");
      this.#compactLine.className = "compact-line";
      this.#compactLine.setAttribute("aria-live", "polite");
      const dot = document.createElement("span");
      dot.className = "status-dot";
      dot.setAttribute("aria-hidden", "true");
      shell.append(this.#visual("linear"), this.#compactLine, dot);
      wrap.append(shell);
    }
    return wrap;
  }

  /**
   * Compact-mode text: the last turn's text trimmed to its final sentence so
   * pill/watch surfaces never overflow.
   */
  #lastSentence(): { role: "user" | "agent"; text: string } | null {
    const turns = this.turns;
    const turn = turns.length > 0 ? turns[turns.length - 1] : undefined;
    if (!turn || !turn.text) return null;
    const sentences = turn.text.split(/(?<=[.!?…])\s+/).filter((s) => s.length > 0);
    const text = sentences.length > 0 ? (sentences[sentences.length - 1] ?? turn.text) : turn.text;
    return { role: turn.role, text };
  }

  #renderTurnList(messages: VoiceUiMessageCatalog): void {
    const scroller = this.#turnScroller;
    if (!scroller) return;
    scroller.replaceChildren();
    for (const turn of this.turns) {
      const item = document.createElement("article");
      item.className = "turn";
      item.setAttribute("part", turn.role === "user" ? "turn turn-user" : "turn turn-agent");
      item.dataset["role"] = turn.role;
      if (!turn.final) item.dataset["draft"] = "";
      const head = document.createElement("header");
      head.className = "turn-head";
      const dot = document.createElement("span");
      dot.className = "turn-dot";
      dot.setAttribute("aria-hidden", "true");
      const role = document.createElement("span");
      role.className = "turn-role";
      role.textContent =
        turn.role === "user" ? messages["sk.voice.agent.you"] : messages["sk.voice.agent.assistant"];
      head.append(dot, role);
      if (turn.interrupted) {
        const flag = document.createElement("span");
        flag.className = "turn-flag";
        flag.textContent = messages["sk.voice.agent.interrupted"];
        head.append(flag);
      }
      const text = document.createElement("p");
      text.className = "turn-text";
      text.textContent = turn.text;
      item.append(head, text);
      scroller.append(item);
    }
  }

  protected override render(): void {
    const messages = this.msgs();
    const key = `${this.size}/${this.frame}/${this.variant}/${this.markSrc ?? ""}`;
    if (this.#wrap && this.#skeletonKey !== key) {
      this.#wrap.remove();
      this.#wrap = null;
      this.#compactLine = this.#watchLine = this.#watchStatus = null;
      this.#statusPill = this.#turnScroller = this.#turnsWrap = null;
      this.#jump = null;
      this.#renderedTurnsRef = null;
      this.#renderedTurnCount = -1;
      this.#pinned = true;
    }
    if (!this.#wrap) {
      this.#skeletonKey = key;
      this.#wrap = this.#skeleton();
      this.root.append(this.#wrap);
    }

    const status = this.#status();
    this.#wrap.dataset["status"] = status;
    this.#wrap.dataset["aura"] = this.auraState;
    this.#wrap.dataset["frame"] = this.frame;
    this.#wrap.dataset["variant"] = this.variant;

    const label = this.#statusLabel(status, messages);
    const speaking = status === "speaking";
    for (const orb of this.#orbButtons) {
      orb.setAttribute("aria-label", speaking ? messages["sk.voice.agent.interrupt"] : label);
    }
    if (this.size === "orb") return;

    const sentence = this.#lastSentence();
    if (this.#compactLine) {
      this.#compactLine.classList.toggle("hidden", !this.transcriptEnabled);
      this.#compactLine.replaceChildren();
      if (sentence && this.transcriptEnabled) {
        const who = document.createElement("span");
        who.className = "who";
        who.textContent =
          sentence.role === "user"
            ? messages["sk.voice.agent.you"]
            : messages["sk.voice.agent.assistant"];
        this.#compactLine.append(who, document.createTextNode(sentence.text));
      } else {
        const who = document.createElement("span");
        who.className = "who";
        who.textContent = label;
        this.#compactLine.append(who);
      }
    }
    if (this.#watchStatus) this.#watchStatus.textContent = label;
    if (this.#watchLine) {
      this.#watchLine.classList.toggle("hidden", !this.transcriptEnabled);
      this.#watchLine.textContent = this.transcriptEnabled ? (sentence?.text ?? "") : "";
    }
    if (this.#statusPill) {
      const text = this.#statusPill.querySelector(".status-text");
      if (text) text.textContent = label;
    }
    if (this.#turnsWrap && this.#turnScroller) {
      this.#turnsWrap.classList.toggle("hidden", !this.transcriptEnabled);
      const turns = this.turns;
      if (turns !== this.#renderedTurnsRef || turns.length !== this.#renderedTurnCount) {
        this.#renderTurnList(messages);
        this.#renderedTurnsRef = turns;
        this.#renderedTurnCount = turns.length;
      }
      if (this.#jump) this.#jump.textContent = messages["sk.voice.agent.jumpToLive"];
      if (this.#pinned) this.#scrollToLive();
    }
  }
}
