import { SmoothedLevel } from "./level.js";
import type { SpeechKitVoiceSessionStatus } from "./voice-surface.js";

/**
 * Recording indicator states (tokens.json → indicator). `requesting` is the
 * kit-local state between `start()` and the first `voice.capture_started` /
 * `voice.denied` (microphone prompt, session setup).
 */
export type RecordingIndicatorState =
  | "idle"
  | "requesting"
  | "listening"
  | "processing"
  | "speaking"
  | "error";

export function sessionStatusToIndicatorState(
  status: SpeechKitVoiceSessionStatus,
  requesting = false
): RecordingIndicatorState {
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
      return requesting ? "requesting" : "idle";
  }
}

const BAR_COUNT = 5;

/** Per-bar height (0.18..1) for a smoothed level; desktop overlay band formula. */
export function indicatorBarHeights(level: number, bars = BAR_COUNT): number[] {
  const center = (bars - 1) / 2;
  return Array.from({ length: bars }, (_, i) => {
    const dist = Math.abs(i - center) / Math.max(center, 1);
    const variation = 0.6 + 0.4 * (1 - dist);
    const harmonic = (Math.sin((i + 1) * 1.618 + bars * 0.75) + 1) / 2;
    return Math.max(0.18, Math.min(1, level * variation * (0.8 + harmonic * 0.4)));
  });
}

/** Shadow-DOM CSS for the indicator; included by every element that renders one. */
export const INDICATOR_CSS = `
.ind {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: var(--sk-indicator-height, 14px);
  flex: none;
  color: var(--sk-accent, oklch(0.65 0.13 210));
}
.ind-dot {
  width: 8px;
  height: 8px;
  flex: none;
  border-radius: var(--sk-radius-pill, 999px);
  background: currentColor;
  transition: background-color var(--sk-motion-base, 180ms) ease;
}
.ind-bars {
  display: none;
  align-items: center;
  gap: var(--sk-indicator-bar-gap, 2px);
  height: 100%;
}
.ind-bar {
  width: var(--sk-indicator-bar-width, 3px);
  height: 100%;
  border-radius: var(--sk-radius-pill, 999px);
  background: currentColor;
  transform: scaleY(var(--h, 0.18));
  transition: transform var(--sk-motion-level, 90ms) linear;
}
.ind[data-state="requesting"] .ind-dot {
  background: var(--sk-pending, #d97706);
  animation: sk-ind-pulse 1.2s ease-in-out infinite;
}
.ind[data-state="listening"] { color: var(--sk-live, #dc2626); }
.ind[data-state="listening"] .ind-dot { background: var(--sk-live, #dc2626); }
.ind:is([data-state="listening"], [data-state="processing"], [data-state="speaking"]) .ind-bars {
  display: inline-flex;
}
.ind:is([data-state="processing"], [data-state="speaking"]) .ind-dot { display: none; }
/* Error is a hollow ring so it never reads as the solid "recording" dot. */
.ind[data-state="error"] .ind-dot {
  box-sizing: border-box;
  background: transparent;
  border: 2px solid var(--sk-danger, #dc2626);
}
.ind[data-state="processing"] .ind-bar {
  animation: sk-ind-sweep 900ms ease-in-out infinite;
}
.ind:not([data-levels]):is([data-state="listening"], [data-state="speaking"]) .ind-bar {
  animation: sk-ind-wave 1100ms ease-in-out infinite;
}
.ind-bar:nth-child(2) { animation-delay: 90ms; }
.ind-bar:nth-child(3) { animation-delay: 180ms; }
.ind-bar:nth-child(4) { animation-delay: 270ms; }
.ind-bar:nth-child(5) { animation-delay: 360ms; }
@keyframes sk-ind-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.35; }
}
@keyframes sk-ind-sweep {
  0%, 100% { transform: scaleY(0.25); opacity: 0.5; }
  40% { transform: scaleY(0.75); opacity: 1; }
}
@keyframes sk-ind-wave {
  0%, 100% { transform: scaleY(0.3); }
  50% { transform: scaleY(0.85); }
}
@media (prefers-reduced-motion: reduce) {
  .ind-dot, .ind-bar { animation: none !important; transition: none !important; }
  .ind-bar { transform: scaleY(0.45) !important; }
  .ind-bar:nth-child(2), .ind-bar:nth-child(4) { transform: scaleY(0.7) !important; }
  .ind-bar:nth-child(3) { transform: scaleY(1) !important; }
}
`;

function prefersReducedMotion(): boolean {
  return typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * The recording indicator: a dot plus five level-reactive bars. Motion lives
 * inside the control only. Feed levels with {@link setLevel}; the rAF loop
 * runs only while listening/speaking with a live level feed and never under
 * reduced motion (CSS then rests the bars at static heights).
 */
export class RecordingIndicator {
  readonly element: HTMLSpanElement;
  readonly #bars: HTMLSpanElement[] = [];
  readonly #input = new SmoothedLevel();
  readonly #output = new SmoothedLevel();
  #state: RecordingIndicatorState = "idle";
  #raf = 0;
  #hasLevels = false;

  constructor() {
    this.element = document.createElement("span");
    this.element.className = "ind";
    this.element.setAttribute("part", "indicator");
    this.element.setAttribute("aria-hidden", "true");
    this.element.dataset["state"] = "idle";
    const dot = document.createElement("span");
    dot.className = "ind-dot";
    const bars = document.createElement("span");
    bars.className = "ind-bars";
    for (let i = 0; i < BAR_COUNT; i += 1) {
      const bar = document.createElement("span");
      bar.className = "ind-bar";
      this.#bars.push(bar);
      bars.append(bar);
    }
    this.element.append(dot, bars);
  }

  get state(): RecordingIndicatorState {
    return this.#state;
  }

  setState(state: RecordingIndicatorState): void {
    if (state === this.#state) return;
    this.#state = state;
    this.element.dataset["state"] = state;
    this.#syncLoop();
  }

  /** Marks whether a level feed exists (without one, CSS runs a gentle wave). */
  setLevelFeed(present: boolean): void {
    this.#hasLevels = present;
    this.element.toggleAttribute("data-levels", present);
    this.#syncLoop();
  }

  setLevel(level: number, source: "input" | "output" = "input"): void {
    if (source === "output") this.#output.target = level;
    else this.#input.target = level;
  }

  /** Restarts the loop if the state needs it (call from connectedCallback). */
  resume(): void {
    this.#syncLoop();
  }

  /** Stops the animation loop (call from disconnectedCallback). */
  stop(): void {
    if (this.#raf) cancelAnimationFrame(this.#raf);
    this.#raf = 0;
  }

  #syncLoop(): void {
    const live = this.#state === "listening" || this.#state === "speaking";
    if (live && this.#hasLevels && !prefersReducedMotion() && typeof requestAnimationFrame === "function") {
      if (!this.#raf) this.#raf = requestAnimationFrame(this.#tick);
    } else {
      this.stop();
      for (const bar of this.#bars) bar.style.removeProperty("--h");
    }
  }

  #tick = (now: number): void => {
    const input = this.#input.tick(now);
    const output = this.#output.tick(now);
    // The active channel leads; the other still counts so a controller that
    // meters only one channel never leaves the bars dead.
    const level = Math.max(this.#state === "speaking" ? output : input, Math.max(input, output) * 0.6);
    const heights = indicatorBarHeights(level, this.#bars.length);
    this.#bars.forEach((bar, i) => {
      // A slow idle swell keeps "live" visible during silence.
      const floor = 0.2 + 0.14 * (0.5 + 0.5 * Math.sin(now / 320 + i * 0.9));
      bar.style.setProperty("--h", Math.max(heights[i] ?? 0, floor).toFixed(3));
    });
    this.#raf = requestAnimationFrame(this.#tick);
  };
}
