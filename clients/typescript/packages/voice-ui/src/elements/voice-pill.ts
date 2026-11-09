import { SpeechKitElement } from "../core/element.js";
import type { VoiceUiController } from "../core/controller.js";
import {
  INDICATOR_CSS,
  RecordingIndicator,
  sessionStatusToIndicatorState,
} from "../core/indicator.js";
import {
  formatVoicePillElapsed,
  isVoicePillPhase,
  voicePillView,
  type VoicePillMode,
  type VoicePillPhase,
} from "../core/pill.js";

const CSS = `
:host {
  display: inline-flex;
  max-width: 100%;
  vertical-align: middle;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size-small, 11px);
}
:host([hidden]), :host([data-empty]) { display: none; }
.pill {
  display: inline-flex;
  align-items: center;
  gap: var(--sk-pill-gap, 6px);
  min-width: 0;
  height: var(--sk-pill-height, 24px);
  box-sizing: border-box;
  padding: 0 10px 0 8px;
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-chip-surface, color-mix(in srgb, #ffffff 84%, transparent));
  transition: color var(--sk-motion-base, 180ms) var(--sk-motion-ease, ease-out);
}
.pill[data-phase="listening"] { color: var(--sk-live, #dc2626); }
.pill[data-phase="error"] { color: var(--sk-danger, #dc2626); }
.text {
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-weight: 600;
  line-height: 1;
}
.time { font-variant-numeric: tabular-nums; }
.text[hidden] { display: none; }
.sr {
  position: absolute;
  width: 1px;
  height: 1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  clip-path: inset(50%);
  white-space: nowrap;
}
${INDICATOR_CSS}
/* static = host-forced reduced motion; same rules as prefers-reduced-motion. */
:host([static]) .ind-dot, :host([static]) .ind-bar { animation: none !important; transition: none !important; }
:host([static]) .ind-bar { transform: scaleY(0.45) !important; }
:host([static]) .ind-bar:nth-child(2), :host([static]) .ind-bar:nth-child(4) { transform: scaleY(0.7) !important; }
:host([static]) .ind-bar:nth-child(3) { transform: scaleY(1) !important; }
:host([static]) .pill { transition: none; }
@media (prefers-reduced-motion: reduce) {
  .pill { transition: none; }
}
`;

const TICK_MS = 1000;

/**
 * `<speechkit-voice-pill>` — the in-Composer voice pill: recording dot,
 * level-reactive bars and, while dictating, the elapsed recording time; for
 * the Voice Agent the phase label instead (spec §Voice pill).
 *
 * Controller-driven by default (status → phase via the recording indicator
 * mapping, levels via `subscribeLevel`); hosts with a richer FSM set `phase`
 * (`requesting | listening | processing | speaking | error`) and feed `level`
 * or {@link setLevel}. The element hides itself while idle. `mode`
 * (`dictation` default, `voice_agent`) selects elapsed time or phase label;
 * `started-at` (epoch ms) anchors the elapsed time, otherwise it counts from
 * the first listening render of this activation (reset when the pill hides). `static` forces the reduced-motion
 * rendering. Labels use the `sk.voice.*` catalogs (`messages` overrides per
 * ID); the phase is announced once per change through a polite status region,
 * the ticking time is a `timer` that is never announced on its own.
 */
export class SpeechKitVoicePillElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-pill";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "mode", "phase", "level", "started-at", "static"];
  }

  #indicator = new RecordingIndicator();
  #pill: HTMLDivElement;
  #sr: HTMLSpanElement;
  #phaseText: HTMLSpanElement;
  #time: HTMLSpanElement;
  #unsubscribeLevel: (() => void) | undefined;
  #controllerLevels = false;
  #hostLevels = false;
  #activeSince: number | undefined;
  #timer: ReturnType<typeof setInterval> | undefined;

  constructor() {
    super(CSS);
    this.#pill = document.createElement("div");
    this.#pill.className = "pill";
    this.#pill.setAttribute("part", "pill");
    this.#pill.setAttribute("role", "status");
    this.#pill.setAttribute("aria-live", "polite");

    this.#sr = document.createElement("span");
    this.#sr.className = "sr";

    this.#phaseText = document.createElement("span");
    this.#phaseText.className = "text";
    this.#phaseText.setAttribute("part", "text");
    this.#phaseText.setAttribute("aria-hidden", "true");

    this.#time = document.createElement("span");
    this.#time.className = "text time";
    this.#time.setAttribute("part", "time");
    this.#time.setAttribute("role", "timer");
    this.#time.setAttribute("aria-live", "off");

    this.#pill.append(this.#indicator.element, this.#sr, this.#phaseText, this.#time);
    this.root.append(this.#pill);
  }

  /** The rendered phase, or `null` while the pill is hidden (idle). */
  get phase(): VoicePillPhase | null {
    const forced = this.getAttribute("phase");
    if (isVoicePillPhase(forced)) return forced;
    const status = this.state?.status ?? this.controller?.getState().status ?? "idle";
    const derived = sessionStatusToIndicatorState(status);
    return derived === "idle" ? null : derived;
  }

  set phase(value: VoicePillPhase | null) {
    if (value === null) this.removeAttribute("phase");
    else this.setAttribute("phase", value);
  }

  get mode(): VoicePillMode {
    return this.getAttribute("mode") === "voice_agent" ? "voice_agent" : "dictation";
  }

  set mode(value: VoicePillMode) {
    this.setAttribute("mode", value);
  }

  /** Feeds a level (0..1) from a host-owned meter. */
  setLevel(level: number, source: "input" | "output" = "input"): void {
    if (!this.#hostLevels) {
      this.#hostLevels = true;
      this.#syncLevelFeed();
    }
    this.#indicator.setLevel(Math.max(0, Math.min(1, level)), source);
  }

  protected override onControllerChanged(controller: VoiceUiController | null): void {
    this.#unsubscribeLevel?.();
    this.#unsubscribeLevel = undefined;
    this.#controllerLevels = Boolean(controller?.subscribeLevel);
    if (controller?.subscribeLevel) {
      this.#unsubscribeLevel = controller.subscribeLevel((level, source) =>
        this.#indicator.setLevel(level, source),
      );
    }
    this.#syncLevelFeed();
  }

  #syncLevelFeed(): void {
    const feed = this.#controllerLevels || this.#hostLevels || this.hasAttribute("level");
    // Under `static` the indicator keeps its CSS resting heights (no rAF loop).
    this.#indicator.setLevelFeed(feed && !this.hasAttribute("static"));
  }

  override attributeChangedCallback(name?: string, _old?: string | null, value?: string | null): void {
    if (name === "level" && value !== null && value !== undefined) {
      const level = Number(value);
      if (Number.isFinite(level)) this.#indicator.setLevel(Math.max(0, Math.min(1, level)));
    }
    if (name === "level" || name === "static") this.#syncLevelFeed();
    super.attributeChangedCallback();
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#syncLevelFeed();
    this.#indicator.resume();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#indicator.stop();
    this.#stopTimer();
  }

  #startedAt(now: number): number {
    const anchor = Number(this.getAttribute("started-at"));
    if (this.hasAttribute("started-at") && Number.isFinite(anchor)) return anchor;
    this.#activeSince ??= now;
    return this.#activeSince;
  }

  #stopTimer(): void {
    if (this.#timer !== undefined) clearInterval(this.#timer);
    this.#timer = undefined;
  }

  protected override render(): void {
    const phase = this.phase;
    this.toggleAttribute("data-empty", phase === null);
    if (phase === null) {
      this.#activeSince = undefined;
      this.#stopTimer();
      this.#indicator.setState("idle");
      return;
    }
    const view = voicePillView(this.mode, phase);
    const messages = this.msgs();
    const label = messages[view.label];

    this.#indicator.setState(view.indicator);
    this.#pill.dataset["phase"] = phase;
    this.#pill.dataset["mode"] = this.mode;
    this.#pill.toggleAttribute("aria-busy", view.busy);
    // The status region announces only the phase label, once per change.
    if (this.#sr.textContent !== label) this.#sr.textContent = label;

    this.#phaseText.textContent = label;
    this.#phaseText.hidden = view.elapsed;
    this.#time.hidden = !view.elapsed;

    if (view.elapsed) {
      const now = Date.now();
      const elapsed = formatVoicePillElapsed(now - this.#startedAt(now));
      this.#time.textContent = elapsed;
      this.#time.setAttribute("aria-label", `${messages["sk.voice.pill.elapsed"]} ${elapsed}`);
      if (this.#timer === undefined) {
        this.#timer = setInterval(() => this.requestUpdate(), TICK_MS);
      }
    } else {
      this.#stopTimer();
    }
  }
}
