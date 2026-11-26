import type { OpalFrame } from "./opal.js";

const TAU = Math.PI * 2;
const clamp = (value: number): number => Number.isFinite(value) ? Math.max(0, Math.min(1, value)) : 0;

/** Canvas renderer for the radial voice visual and the kombify braided rosette.
 * The host owns animation scheduling, accessibility, and the canvas lifetime.
 */
export class VoiceMotifRenderer {
  readonly #canvas: HTMLCanvasElement;
  readonly #context: CanvasRenderingContext2D | null;
  readonly #motif: "resonance" | "rosette";
  #lastFrame = -Infinity;
  #lastSignature = "";
  #thinking = 0;

  constructor(canvas: HTMLCanvasElement, motif: "resonance" | "rosette") {
    this.#canvas = canvas;
    this.#context = canvas.getContext("2d");
    this.#motif = motif;
  }

  render(frame: OpalFrame): void {
    const context = this.#context;
    if (!context) return;
    const rect = this.#canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const size = Math.max(1, Math.min(768, Math.round(Math.min(rect.width, rect.height) * Math.min(2, window.devicePixelRatio || 1))));
    const resized = this.#canvas.width !== size || this.#canvas.height !== size;
    if (resized) {
      this.#canvas.width = size;
      this.#canvas.height = size;
    }
    const elapsed = Math.max(0, Math.min(100, frame.now - this.#lastFrame));
    const signature = `${frame.state}/${frame.profile}/${frame.reducedMotion}/${size}`;
    if (!resized && signature === this.#lastSignature && (frame.reducedMotion || elapsed < 33)) return;
    this.#lastFrame = frame.now;
    this.#lastSignature = signature;
    const target = frame.state === "processing" || frame.state === "connecting" ? 1 : 0;
    this.#thinking += (target - this.#thinking) * (frame.reducedMotion ? 1 : 1 - Math.exp(-elapsed / 180));
    const time = frame.reducedMotion ? 0 : frame.now / 1000;
    const input = frame.reducedMotion ? 0 : clamp(frame.inputLevel);
    const output = frame.reducedMotion ? 0 : clamp(frame.outputLevel);
    context.setTransform(size / 256, 0, 0, size / 256, size / 2, size / 2);
    context.clearRect(-128, -128, 256, 256);
    if (this.#motif === "resonance") this.#drawResonance(context, time, input, output);
    else this.#drawRosette(context, time, input, output, size);
  }

  #drawResonance(context: CanvasRenderingContext2D, time: number, input: number, output: number): void {
    context.lineCap = "round";
    const circle = (radius: number, color: string, width: number): void => {
      context.beginPath();
      context.arc(0, 0, radius, 0, TAU);
      context.strokeStyle = color;
      context.lineWidth = width;
      context.stroke();
    };
    const line = (x1: number, y1: number, x2: number, y2: number, color: string, width: number): void => {
      context.beginPath();
      context.moveTo(x1, y1);
      context.lineTo(x2, y2);
      context.strokeStyle = color;
      context.lineWidth = width;
      context.stroke();
    };
    circle(79, "rgba(202,217,198,.18)", 0.65);
    circle(104, "rgba(219,228,203,.15)", 0.6);
    for (let n = 0; n < 100; n += 1) {
      const angle = TAU * n / 100;
      const x = Math.cos(angle);
      const y = Math.sin(angle);
      const band = (0.5 + 0.5 * Math.sin(angle * 3 - time * 1.65)) * (0.6 + 0.4 * Math.cos(angle * 5 + time * 2.1));
      const innerBand = (0.5 + 0.5 * Math.cos(angle * 2 + time * 1.8)) * (0.7 + 0.3 * Math.sin(angle * 6 - time));
      const metal = 0.5 + 0.5 * Math.cos(angle + 1);
      const length = 5 + 6 * metal + output * (8 + 23 * band);
      const brightness = Math.round(144 + 78 * metal + 27 * output * band);
      line(x * 82, y * 82, x * (82 + length), y * (82 + length), `rgb(${brightness},${Math.min(255, brightness + 2)},${brightness - 13})`, 2.9);
      const innerLength = 2.7 + 2.8 * metal + input * (7 + 16 * innerBand);
      line(x * 72, y * 72, x * (72 - innerLength), y * (72 - innerLength), `rgba(192,227,209,${0.35 + 0.38 * metal + 0.22 * input})`, 1.55);
    }
    circle(48, "rgba(229,239,210,.18)", 0.65);
    const heights = [3, 7, 10, 7, 3];
    heights.forEach((height, n) => {
      const h = 1.7 + (input + output) * (0.18 + Math.abs(Math.sin(time * 2.1 + n * 0.8))) * height + this.#thinking * (2 + 2 * Math.cos(time * 3 - n));
      line((n - 2) * 5, -h, (n - 2) * 5, h, "#e1e8d7", 2.25);
    });
  }

  #drawRosette(context: CanvasRenderingContext2D, time: number, input: number, output: number, size: number): void {
    const outer = 83 + output * 10 - input * 7;
    // Fewer cross-sections at keyboard scale preserve the motif while reducing work.
    const sectionCount = size < 150 ? 72 : 136;
    const points = size < 150 ? 20 : 32;
    for (let k = 0; k < sectionCount; k += 1) {
      const angle = k * TAU / sectionCount;
      const ca = Math.cos(angle);
      const sa = Math.sin(angle);
      const twist = angle * 3 + 0.42 + output * 0.5 * Math.sin(1.5 * time) + input * 0.3 * Math.cos(1.8 * time) + this.#thinking * 0.26 * Math.cos(time);
      const radius = 15 + 3 * Math.cos(3 * angle - twist * 0.3) + output * 4;
      const depth = 0.5 + 0.5 * Math.cos(angle + 1.8);
      const light = clamp(0.15 + 0.67 * depth + 0.19 * Math.cos(twist + 0.3));
      context.beginPath();
      for (let j = 0; j <= points; j += 1) {
        const b = TAU * j / points;
        const dr = radius * Math.cos(b) * Math.cos(twist) - radius * 0.5 * Math.sin(b) * Math.sin(twist);
        const dz = radius * Math.cos(b) * Math.sin(twist) + radius * 0.5 * Math.sin(b) * Math.cos(twist);
        const inner = Math.max(0, -dr / radius) * input * (5 + 5 * Math.cos(2 * angle - time * 1.6));
        const voice = Math.max(0, dr / radius) * output * (5 + 5 * Math.sin(3 * angle + time * 1.4));
        const r = outer + dr + 3.2 * Math.cos(3 * angle + 0.9) - inner + voice;
        const x = ca * r - 0.22 * dz;
        const y = sa * r * 0.965 - 0.84 * dz;
        if (j === 0) context.moveTo(x, y);
        else context.lineTo(x, y);
      }
      context.strokeStyle = `rgba(${Math.round(31 + 161 * light)},${Math.round(134 + 116 * light)},${Math.round(149 + 100 * light)},${0.37 + 0.57 * light})`;
      context.lineWidth = 1.05;
      context.stroke();
    }
    // A host-provided brand mark can be layered over the open centre.
  }
}
