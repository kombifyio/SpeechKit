/** Canvas-only Opal visual. The host owns the canvas, frame loop and lifecycle. */
export type OpalState = "inactive" | "connecting" | "listening" | "processing" | "speaking" | "recovering" | "settling" | "error";
export type OpalProfile = "original" | "small";
export interface OpalFrame {
  /** requestAnimationFrame timestamp in milliseconds. */
  now: number;
  state: OpalState;
  /** Smoothed input and output amplitudes in the range 0–1. */
  inputLevel: number;
  outputLevel: number;
  profile: OpalProfile;
  reducedMotion: boolean;
}

const SIZE = 192;
const HALF = SIZE / 2;
const clamp = (value: number): number => Math.max(0, Math.min(1, Number.isFinite(value) ? value : 0));
type RGB = readonly [number, number, number];
interface GlassSample { at: number; sky: number; base: number; edge: number; alpha: number }
interface MistSample { at: number; d: number; x: number; y: number; c2: number; s2: number; c3: number; s3: number }

/**
 * Rasterizes the approved glass, ribbon and light mist at a bounded internal
 * resolution. No animation frame, audio capture, or observers are created.
 */
export class OpalRenderer {
  readonly #canvas: HTMLCanvasElement;
  readonly #context: CanvasRenderingContext2D;
  readonly #glass: HTMLCanvasElement;
  readonly #glassContext: CanvasRenderingContext2D;
  readonly #glassImage: ImageData;
  readonly #mist: HTMLCanvasElement;
  readonly #mistContext: CanvasRenderingContext2D;
  readonly #mistImage: ImageData;
  readonly #colorContext: CanvasRenderingContext2D;
  readonly #geometry: GlassSample[] = [];
  readonly #haze: MistSample[] = [];
  readonly #weights = [1, 0, 0];
  readonly #velocity = [0, 0, 0];
  readonly #inputHistory = new Float32Array(96);
  readonly #outputHistory = new Float32Array(96);
  #historyIndex = 0;
  #profile: OpalProfile | undefined;
  #lastNow = 0;
  #lastDraw = -Infinity;
  #time = 0;
  #errorTint = 0;
  #activity = 1;
  #lastSignature = "";
  #radius = 55.5;
  #reach = 39.5;

  constructor(canvas: HTMLCanvasElement) {
    this.#canvas = canvas;
    const context = canvas.getContext("2d");
    const glass = document.createElement("canvas");
    const mist = document.createElement("canvas");
    const color = document.createElement("canvas");
    glass.width = glass.height = SIZE;
    mist.width = mist.height = HALF;
    color.width = color.height = 1;
    const glassContext = glass.getContext("2d");
    const mistContext = mist.getContext("2d");
    const colorContext = color.getContext("2d", { willReadFrequently: true });
    if (!context || !glassContext || !mistContext || !colorContext) throw new Error("Opal requires a 2D canvas context");
    this.#context = context;
    this.#glass = glass;
    this.#glassContext = glassContext;
    this.#glassImage = glassContext.createImageData(SIZE, SIZE);
    this.#mist = mist;
    this.#mistContext = mistContext;
    this.#mistImage = mistContext.createImageData(HALF, HALF);
    this.#colorContext = colorContext;
    for (let row = 0; row < SIZE; row += 1) for (let col = 0; col < SIZE; col += 1) {
      const x = (2 * col + 1 - SIZE) / (SIZE - 1.5);
      const y = (2 * row + 1 - SIZE) / (SIZE - 1.5);
      const r = Math.hypot(x, y);
      if (r >= 1) continue;
      const z = Math.sqrt(1 - r * r), u = x * .937 - y * .35;
      const sky = clamp(.54 - .29 * x - .49 * y + .16 * z);
      const spec = Math.max(0, -.53 * x - .63 * y + .57 * z) ** 29;
      const edge = (1 - z) ** 3, rim = 1 / (1 + 1800 * (z - .18) ** 2);
      const shimmer = clamp(1 - z * 2.2) * (.5 + .5 * Math.cos(56 * z + u * 5));
      this.#geometry.push({ at: 4 * (row * SIZE + col), sky, edge,
        base: 17 + 71 * sky + 66 * sky * sky + 76 * edge + 47 * rim + 87 * spec + 16 * shimmer,
        alpha: Math.round(clamp((1 - r) * SIZE / 2) * 255) });
    }
  }

  render(frame: OpalFrame): void {
    const rect = this.#canvas.getBoundingClientRect();
    if (!rect.width || !rect.height) return;
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    const width = Math.max(1, Math.round(rect.width * ratio)), height = Math.max(1, Math.round(rect.height * ratio));
    const resized = this.#canvas.width !== width || this.#canvas.height !== height;
    if (resized) { this.#canvas.width = width; this.#canvas.height = height; }
    const signature = `${frame.state}/${frame.profile}/${frame.reducedMotion}/${width}/${height}`;
    if (!resized && signature === this.#lastSignature && (frame.reducedMotion || frame.now - this.#lastDraw < 40)) return;
    const dt = this.#lastNow ? Math.max(0, Math.min(.1, (frame.now - this.#lastNow) / 1000)) : .04;
    this.#lastNow = frame.now;
    this.#lastDraw = frame.now;
    this.#lastSignature = signature;
    if (frame.profile !== this.#profile) this.#setProfile(frame.profile);
    const input = clamp(frame.inputLevel), output = clamp(frame.outputLevel);
    const overlap = frame.state === "speaking" ? clamp(input * 1.5) * .72 : 0;
    const target = (frame.state === "processing" || frame.state === "connecting" || frame.state === "recovering") ? [0, 1, 0] : frame.state === "speaking" ? [overlap, 0, 1 - overlap] : [1, 0, 0];
    const rate = 6.6, decay = Math.exp(-rate * dt);
    for (let i = 0; i < 3; i += 1) {
      const goal = target[i] ?? 0, current = this.#weights[i] ?? 0, speed = this.#velocity[i] ?? 0;
      const delta = current - goal, slope = speed + rate * delta;
      this.#weights[i] = frame.reducedMotion ? goal : goal + (delta + slope * dt) * decay;
      this.#velocity[i] = frame.reducedMotion ? 0 : (speed - rate * slope * dt) * decay;
    }
    this.#errorTint = frame.reducedMotion ? Number(frame.state === "error") : this.#errorTint + (Number(frame.state === "error") - this.#errorTint) * (1 - Math.exp(-6.6 * dt));
    const activity = frame.state === "inactive" ? .45 : 1;
    this.#activity = frame.reducedMotion ? activity : this.#activity + (activity - this.#activity) * (1 - Math.exp(-6.6 * dt));
    if (!frame.reducedMotion) this.#time += dt;
    this.#historyIndex = (this.#historyIndex + 1) % this.#inputHistory.length;
    this.#inputHistory[this.#historyIndex] = frame.reducedMotion ? 0 : input;
    this.#outputHistory[this.#historyIndex] = frame.reducedMotion ? 0 : output;
    this.#draw(frame.reducedMotion ? 0 : this.#time, frame.reducedMotion ? 0 : input, frame.reducedMotion ? 0 : output);
  }

  #setProfile(profile: OpalProfile): void {
    this.#profile = profile;
    this.#radius = profile === "small" ? 32 : 55.5;
    this.#reach = HALF - this.#radius - 1;
    this.#haze.length = 0;
    this.#mistImage.data.fill(0);
    for (let y = 0; y < HALF; y += 1) for (let x = 0; x < HALF; x += 1) {
      const dx = (x + .5) * 2 - HALF, dy = (y + .5) * 2 - HALF;
      const distance = Math.hypot(dx, dy) - this.#radius;
      if (distance < -2 || distance > this.#reach) continue;
      const a = Math.atan2(dy, dx);
      this.#haze.push({ at: 4 * (y * HALF + x), d: Math.max(0, distance), 
        x: Math.cos(a), y: Math.sin(a), c2: Math.cos(2 * a), s2: Math.sin(2 * a), c3: Math.cos(3 * a), s3: Math.sin(3 * a) });
    }
  }

  #draw(time: number, input: number, output: number): void {
    const L = this.#weights[0] ?? 0, T = this.#weights[1] ?? 0, S = this.#weights[2] ?? 0;
    const colorContext = this.#colorContext;
    colorContext.fillStyle = `oklch(${.75 * L + .70 * T + .78 * S} ${.13 * L + .18 * (T + S)} ${(210 * L + 285 * T + 405 * S) * (1 - this.#errorTint) + 25 * this.#errorTint})`;
    colorContext.fillRect(0, 0, 1, 1);
    const pixel = colorContext.getImageData(0, 0, 1, 1).data;
    const color: RGB = [pixel[0] ?? 0, pixel[1] ?? 0, pixel[2] ?? 0];
    const image = this.#glassImage.data;
    for (const { at, base, sky, edge, alpha } of this.#geometry) {
      const glass = .78 * base, dye = .76 * (.17 + .36 * sky);
      image[at] = glass + dye * color[0]; image[at + 1] = glass + dye * color[1] + 3 + 6 * edge;
      image[at + 2] = glass + dye * color[2] + 8 * sky; image[at + 3] = alpha;
    }
    this.#glassContext.putImageData(this.#glassImage, 0, 0);
    this.#ribbon(time, L, T, S, input, output, color);
    const ctx = this.#context;
    ctx.clearRect(0, 0, this.#canvas.width, this.#canvas.height);
    ctx.save();
    ctx.scale(this.#canvas.width / SIZE, this.#canvas.height / SIZE);
    ctx.globalAlpha = this.#activity;
    this.#drawMist(time, S, false, color);
    this.#drawMist(time, L, true, color);
    ctx.drawImage(this.#glass, HALF - this.#radius, HALF - this.#radius, 2 * this.#radius, 2 * this.#radius);
    ctx.restore();
  }

  #drawMist(time: number, strength: number, listening: boolean, color: RGB): void {
    if (strength < .002) return;
    const data = this.#mistImage.data, drift = time * .58;
    const ca = Math.cos(drift), sa = Math.sin(drift), cb = Math.cos(.83 * drift), sb = Math.sin(.83 * drift);
    const history = listening ? this.#inputHistory : this.#outputHistory;
    for (const { at, d, x, y, c2, s2, c3, s3 } of this.#haze) {
      const delay = d * (listening ? .05 : .22);
      const newer = (this.#historyIndex - Math.floor(delay) + history.length) % history.length;
      const from = history[newer] ?? 0;
      const to = history[(newer + history.length - 1) % history.length] ?? 0;
      const level = from + (to - from) * (delay % 1);
      const billow = .55 + .21 * (c2 * ca + s2 * sa) + .13 * (c3 * cb - s3 * sb) - .08 * y + .03 * x;
      const spread = (6.5 + (11.5 + 23 * level) * billow + 9.5 * level) * (this.#profile === "small" ? 1.65 : 1);
      const density = Math.exp(-1.85 * (d / spread) ** 2), edge = clamp((this.#reach - d) / 8);
      const pearl = Math.exp(-d / 4.5) * (.22 + .25 * level);
      data[at] = color[0] + (255 - color[0]) * pearl; data[at + 1] = color[1] + (244 - color[1]) * pearl;
      data[at + 2] = color[2] + (213 - color[2]) * pearl;
      const intensity = listening ? .86 * Math.sqrt(level) : .16 + .61 * level;
      data[at + 3] = 255 * strength * edge * edge * density * intensity * (.74 + .38 * billow);
    }
    this.#mistContext.putImageData(this.#mistImage, 0, 0);
    this.#context.drawImage(this.#mist, 0, 0, SIZE, SIZE);
  }

  #ribbon(time: number, L: number, T: number, S: number, input: number, voice: number, color: RGB): void {
    const ctx = this.#glassContext, line = new Path2D(), fold = new Path2D();
    const depth = SIZE * (.018 + .034 * L + .064 * S);
    const point = (f: number): [number, number] => {
      const angle = (2 * f - 1) * Math.PI * (1 + T) / 2;
      const x = (1.09 - .39 * T) * Math.sin(angle);
      const bowl = .34 - .82 * x * x - .15 * input * (1 - x * x) + .022 * Math.sin(1.7 * time);
      const wave = -.22 * x + .24 * Math.sin(3.75 * x + .42 * Math.sin(2.6 * time)) + .11 - .21 * voice;
      const y = L * bowl + T * .32 * Math.cos(angle) + S * wave, tilt = -.36 * T - .38 * S;
      return [SIZE * (.5 + .499 * (x * Math.cos(tilt) - y * Math.sin(tilt))), SIZE * (.5 + .499 * (x * Math.sin(tilt) + y * Math.cos(tilt)))];
    };
    for (let k = 0; k <= 160; k += 1) {
      const f = k / 160, [x, y] = point(f), offset = depth * (.71 + .29 * Math.cos(2 * Math.PI * (f - .5)));
      if (k === 0) { line.moveTo(x, y); fold.moveTo(x, y + offset); } else { line.lineTo(x, y); fold.lineTo(x, y + offset); }
    }
    ctx.save(); ctx.beginPath(); ctx.arc(HALF, HALF, (SIZE - 1.5) / 2, 0, 2 * Math.PI); ctx.clip();
    ctx.lineCap = "round"; ctx.lineJoin = "round";
    const stroke = (path: Path2D, width: number, paint: string, blur: number): void => {
      ctx.filter = blur ? `blur(${blur}px)` : "none"; ctx.lineWidth = width; ctx.strokeStyle = paint; ctx.stroke(path);
    };
    stroke(fold, 12, "rgba(12,16,35,.30)", 4.5);
    stroke(line, 14, `rgba(${color[0]},${color[1]},${color[2]},.90)`, 6.5);
    stroke(fold, 2.5, `rgba(247,245,245,${.18 + .14 * S})`, 1.5);
    stroke(line, 6, "rgba(255,248,246,.51)", 2.5);
    stroke(line, 1.35, "rgba(255,253,249,.93)", .7);
    const f = (time * .22 + .21) % 1, [x, y] = point(f);
    const seam = clamp(Math.min(f, 1 - f) * 13), highlight = T * T * (T + (1 - T) * seam);
    if (highlight > .002) {
      ctx.filter = "none";
      const gradient = ctx.createRadialGradient(x, y, 0, x, y, 7);
      gradient.addColorStop(0, `rgba(255,255,255,${highlight})`);
      gradient.addColorStop(.24, `rgba(255,255,255,${highlight * .85})`);
      gradient.addColorStop(1, "rgba(255,255,255,0)");
      ctx.fillStyle = gradient; ctx.fillRect(x - 7, y - 7, 14, 14);
    }
    ctx.restore();
  }
}
