/**
 * "Glass Waveform" assistant visual (`variant="waveform"`, lab variant B
 * promoted 2026-08-13). The motif replaces the Aura orb in the same visual
 * slot — shells, status text, transcript, and interaction stay identical to
 * the default variant. It draws a live dual-hue level history: agent output
 * in `--sk-assistant-wave-output`, mic input in `--sk-assistant-wave-input`.
 * Round slots (bare orb, watch face, expanded hero) render the radial rim;
 * the compact pill and keyboard bar render the linear strip.
 *
 * Geometry and cadence are normative in `tokens.json` →
 * `assistant-variants.waveform` (the SSOT native parity ports implement).
 */

export const WAVEFORM_HISTORY_SLOTS = 72;
export const WAVEFORM_CADENCE_MS = 45;

export type WaveformLayout = "linear" | "radial";

interface WaveColors {
  input: string;
  output: string;
}

interface WaveSample {
  input: number;
  output: number;
}

export interface WaveformFrameOptions {
  /** false for the resting aura states (`inactive`, `error`): dimmed draw, no history advance. */
  active: boolean;
  /** true under prefers-reduced-motion: static idle baseline, no scroll, no level amplitude. */
  reducedMotion: boolean;
}

export class AssistantWaveformVisual {
  readonly element: HTMLButtonElement;

  readonly #canvas: HTMLCanvasElement;
  readonly #layout: WaveformLayout;
  readonly #history: WaveSample[] = [];
  #accum = 0;
  #lastNow = 0;

  constructor(layout: WaveformLayout) {
    this.#layout = layout;
    this.element = document.createElement("button");
    this.element.type = "button";
    this.element.className = "wave-visual";
    this.element.setAttribute("part", "orb");
    this.element.dataset["layout"] = layout;
    this.#canvas = document.createElement("canvas");
    this.element.append(this.#canvas);
    for (let i = 0; i < WAVEFORM_HISTORY_SLOTS; i += 1) {
      this.#history.push({ input: 0, output: 0 });
    }
  }

  /**
   * Driven from the element's level animation loop. The fixed 45 ms history
   * cadence keeps scroll speed frame-rate independent (tokens SSOT).
   */
  onFrame(input: number, output: number, now: number, options: WaveformFrameOptions): void {
    const dt = this.#lastNow === 0 ? 16 : Math.min(100, now - this.#lastNow);
    this.#lastNow = now;
    if (options.active && !options.reducedMotion) {
      this.#accum += dt;
      while (this.#accum >= WAVEFORM_CADENCE_MS) {
        this.#accum -= WAVEFORM_CADENCE_MS;
        this.#history.push({ input, output });
        if (this.#history.length > WAVEFORM_HISTORY_SLOTS) this.#history.shift();
      }
    }
    this.#draw(options);
  }

  #colors(): WaveColors {
    const styles = getComputedStyle(this.element);
    return {
      output: styles.getPropertyValue("--sk-assistant-wave-output").trim() || "#38bdf8",
      input: styles.getPropertyValue("--sk-assistant-wave-input").trim() || "#34d399"
    };
  }

  #draw(options: WaveformFrameOptions): void {
    const canvas = this.#canvas;
    const rect = canvas.getBoundingClientRect();
    if (rect.width === 0 || rect.height === 0) return;
    const dpr = window.devicePixelRatio || 1;
    const width = Math.round(rect.width * dpr);
    const height = Math.round(rect.height * dpr);
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
    }
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.clearRect(0, 0, width, height);
    const colors = this.#colors();
    const history = options.reducedMotion ? this.#idleHistory() : this.#history;
    const dim = options.active ? 1 : 0.35;
    if (this.#layout === "radial") {
      this.#drawRadial(ctx, width, height, colors, history, dim, dpr);
    } else {
      this.#drawLinear(ctx, width, height, colors, history, dim);
    }
    ctx.globalAlpha = 1;
  }

  #idleHistory(): WaveSample[] {
    return this.#history.map(() => ({ input: 0, output: 0 }));
  }

  #drawLinear(
    ctx: CanvasRenderingContext2D,
    width: number,
    height: number,
    colors: WaveColors,
    history: WaveSample[],
    dim: number
  ): void {
    const n = history.length;
    const step = width / n;
    const barWidth = Math.max(1.5, step * 0.55);
    const mid = height / 2;
    const idleDot = Math.max(1.5, height * 0.03);
    for (let i = 0; i < n; i += 1) {
      const entry = history[i];
      if (!entry) continue;
      const x = i * step + (step - barWidth) / 2;
      const out = entry.output * (height * 0.46);
      const inp = entry.input * (height * 0.46);
      ctx.fillStyle = colors.output;
      ctx.globalAlpha = 0.85 * dim;
      ctx.fillRect(x, mid - Math.max(idleDot, out), barWidth, Math.max(idleDot, out) * 2);
      if (inp > idleDot) {
        ctx.fillStyle = colors.input;
        ctx.globalAlpha = 0.9 * dim;
        ctx.fillRect(x, mid - inp, barWidth * 0.6, inp * 2);
      }
    }
  }

  #drawRadial(
    ctx: CanvasRenderingContext2D,
    width: number,
    height: number,
    colors: WaveColors,
    history: WaveSample[],
    dim: number,
    dpr: number
  ): void {
    const cx = width / 2;
    const cy = height / 2;
    const r0 = Math.min(width, height) * 0.34;
    const maxLen = Math.min(width, height) * 0.14;
    const n = history.length;
    ctx.lineCap = "round";
    for (let i = 0; i < n; i += 1) {
      const entry = history[i];
      if (!entry) continue;
      const angle = (i / n) * Math.PI * 2 - Math.PI / 2;
      const level = Math.max(entry.output, entry.input);
      const isInput = entry.input > entry.output;
      const len = Math.max(2, level * maxLen);
      ctx.strokeStyle = isInput ? colors.input : colors.output;
      ctx.globalAlpha = (0.35 + level * 0.6) * dim;
      ctx.lineWidth = Math.max(1.5, dpr * 1.5);
      ctx.beginPath();
      ctx.moveTo(cx + Math.cos(angle) * r0, cy + Math.sin(angle) * r0);
      ctx.lineTo(cx + Math.cos(angle) * (r0 + len), cy + Math.sin(angle) * (r0 + len));
      ctx.stroke();
    }
  }
}
