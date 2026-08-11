/**
 * Fast-rise / slow-decay envelope follower for visualizer levels. Raw RMS
 * levels from `subscribeLevel` arrive at meter rate (~20 Hz) and look jittery
 * when written straight to a transform; this smooths them for per-frame
 * rendering (attack 0.55 per tick, exponential release ≈5/s).
 */
export class SmoothedLevel {
  #value = 0;
  #target = 0;
  #lastTime = 0;

  set target(level: number) {
    this.#target = Math.max(0, Math.min(1, level));
  }

  /** Advance to `now` (ms, e.g. a rAF timestamp) and return the smoothed level. */
  tick(now: number): number {
    const dt = this.#lastTime === 0 ? 16 : Math.min(100, now - this.#lastTime);
    this.#lastTime = now;
    if (this.#target > this.#value) {
      this.#value += (this.#target - this.#value) * 0.55;
    } else {
      this.#value *= Math.exp((-dt / 1000) * 5);
      if (this.#value < 0.002) this.#value = 0;
    }
    return this.#value;
  }

  get value(): number {
    return this.#value;
  }
}
