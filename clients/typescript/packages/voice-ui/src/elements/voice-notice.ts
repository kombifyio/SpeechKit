import { SpeechKitElement } from "../core/element.js";
import {
  isRetryableNoticeKind,
  stripUrls,
  voiceNoticeHint,
  voiceNoticeKind,
  voiceNoticeKindForDenial,
  voiceNoticeMessage,
  type VoiceNoticeKind
} from "../core/notice.js";
import type { SpeechKitVoiceDenial } from "../core/voice-surface.js";

export type VoiceNoticeTone = "error" | "warning" | "info";

const CHEVRON_ICON =
  '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="M4.5 6.5L8 10l3.5-3.5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const CLOSE_ICON =
  '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="M4 4l8 8M12 4l-8 8" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>';

const CSS = `
:host {
  display: inline-block;
  max-width: 100%;
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size-small, 11px);
}
:host([data-empty]) { display: none; }
.wrap { display: grid; gap: 4px; max-width: 100%; }
.notice {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: 100%;
  min-height: var(--sk-chip-height, 30px);
  box-sizing: border-box;
  padding: 0 4px 0 10px;
  border-radius: var(--sk-chip-radius, 999px);
  background: var(--sk-chip-surface, color-mix(in srgb, #ffffff 84%, transparent));
  backdrop-filter: blur(var(--sk-blur, 16px));
  -webkit-backdrop-filter: blur(var(--sk-blur, 16px));
  box-shadow: var(--sk-dialog-shadow, 0 18px 48px -20px rgba(16, 20, 24, 0.35));
  animation: sk-notice-in var(--sk-motion-base, 180ms) var(--sk-motion-ease, ease-out);
}
.mark {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: var(--sk-radius-pill, 999px);
  background: var(--sk-danger, #dc2626);
}
:host([tone="warning"]) .mark { background: var(--sk-pending, #d97706); }
:host([tone="info"]) .mark { background: var(--sk-accent, oklch(0.65 0.13 210)); }
.msg {
  flex: 1 1 auto;
  min-width: 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-weight: 600;
}
button {
  flex: none;
  border: 0;
  border-radius: var(--sk-radius-pill, 999px);
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-weight: 600;
  padding: 4px 8px;
  white-space: nowrap;
  transition: background-color var(--sk-motion-base, 180ms) ease;
}
button:hover { background: color-mix(in srgb, currentColor 8%, transparent); }
button:focus-visible {
  outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210));
  outline-offset: -2px;
}
button[hidden] { display: none; }
/* Flat: inside another card (dialog, overlay) — no chip surface of its own. */
:host([flat]) { display: block; }
:host([flat]) .notice {
  display: flex;
  padding-inline-start: 2px;
  background: transparent;
  box-shadow: none;
  backdrop-filter: none;
  -webkit-backdrop-filter: none;
}
.toggle, .toggle svg { transition: transform var(--sk-motion-base, 180ms) ease; }
.toggle[aria-expanded="true"] svg { transform: rotate(180deg); }
.retry { color: var(--sk-accent, oklch(0.65 0.13 210)); }
.close {
  display: inline-grid;
  place-items: center;
  width: 22px;
  height: 22px;
  padding: 0;
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
.details {
  display: grid;
  gap: 2px;
  margin: 0;
  padding: 0 12px;
  font-size: var(--sk-font-size-small, 11px);
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  line-height: 1.45;
  overflow-wrap: anywhere;
}
.details[hidden] { display: none; }
.details p { margin: 0; }
.meta { font-variant-numeric: tabular-nums; opacity: 0.85; }
@keyframes sk-notice-in {
  from { opacity: 0; transform: translateY(2px); }
  to { opacity: 1; transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .notice { animation: none; }
}
`;

let detailsCounter = 0;

/**
 * `<speechkit-voice-notice>` — compact one-line error/status chip. The message
 * is localized by reason code (`reason` attribute, a `denial` envelope, or the
 * controller's current denial); the default view never shows raw codes or
 * URLs. "Details" discloses the localized hint, sanitized guidance steps, the
 * reason code and the support reference — never a URL.
 *
 * Attributes: `reason`, `tone` (`error` | `warning` | `info`), `retryable` /
 * `no-retry`, `no-dismiss`, `request-id`, `flat` (no chip surface, for use
 * inside another card).
 *
 * Events: `speechkit-retry` (cancelable; default restarts the controller in
 * the denied mode) and `speechkit-dismiss`.
 */
export class SpeechKitVoiceNoticeElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-notice";

  static override get observedAttributes(): string[] {
    return [
      ...super.observedAttributes,
      "reason",
      "tone",
      "retryable",
      "no-retry",
      "no-dismiss",
      "request-id",
      "flat"
    ];
  }

  #denial: SpeechKitVoiceDenial | undefined;
  #message: string | undefined;
  #details: string | undefined;
  #dismissedKey: string | undefined;
  #expanded = false;
  #renderedKey = "";

  readonly #notice: HTMLDivElement;
  readonly #msg: HTMLSpanElement;
  readonly #toggle: HTMLButtonElement;
  readonly #retry: HTMLButtonElement;
  readonly #close: HTMLButtonElement;
  readonly #detailsEl: HTMLDivElement;

  constructor() {
    super(CSS);
    const wrap = document.createElement("div");
    wrap.className = "wrap";
    this.#notice = document.createElement("div");
    this.#notice.className = "notice";
    this.#notice.setAttribute("part", "notice");
    const mark = document.createElement("span");
    mark.className = "mark";
    mark.setAttribute("aria-hidden", "true");
    this.#msg = document.createElement("span");
    this.#msg.className = "msg";
    this.#msg.setAttribute("part", "message");
    this.#toggle = this.#button("toggle", "details-toggle", () => {
      this.#expanded = !this.#expanded;
      this.#renderedKey = "";
      this.requestUpdate();
    });
    this.#retry = this.#button("retry", "retry", () => this.retry());
    this.#close = this.#button("close", "dismiss", () => this.dismiss());
    this.#close.innerHTML = CLOSE_ICON;
    this.#toggle.innerHTML = CHEVRON_ICON;
    this.#detailsEl = document.createElement("div");
    this.#detailsEl.className = "details";
    this.#detailsEl.setAttribute("part", "details");
    this.#detailsEl.id = `sk-notice-details-${(detailsCounter += 1)}`;
    this.#detailsEl.hidden = true;
    this.#toggle.setAttribute("aria-controls", this.#detailsEl.id);
    this.#notice.append(mark, this.#msg, this.#toggle, this.#retry, this.#close);
    wrap.append(this.#notice, this.#detailsEl);
    this.root.append(wrap);
  }

  #button(className: string, part: string, onClick: () => void): HTMLButtonElement {
    const button = document.createElement("button");
    button.type = "button";
    button.className = className;
    button.setAttribute("part", part);
    button.addEventListener("click", onClick);
    return button;
  }

  // ----- inputs ---------------------------------------------------------------

  /** Denial envelope to render (takes precedence over the controller's). */
  get denial(): SpeechKitVoiceDenial | undefined {
    return this.#denial;
  }

  set denial(value: SpeechKitVoiceDenial | undefined) {
    this.#denial = value;
    this.#dismissedKey = undefined;
    this.requestUpdate();
  }

  /** Host-localized one-line override (URLs are stripped). */
  get message(): string | undefined {
    return this.#message;
  }

  set message(value: string | undefined) {
    this.#message = value;
    this.requestUpdate();
  }

  /** Extra host detail text for the disclosure (URLs are stripped). */
  get details(): string | undefined {
    return this.#details;
  }

  set details(value: string | undefined) {
    this.#details = value;
    this.requestUpdate();
  }

  get reason(): string | null {
    return this.getAttribute("reason");
  }

  set reason(value: string | null) {
    if (value === null) this.removeAttribute("reason");
    else this.setAttribute("reason", value);
  }

  // ----- actions --------------------------------------------------------------

  retry(): void {
    const denial = this.#activeDenial();
    const proceed = this.dispatchEvent(
      new CustomEvent("speechkit-retry", {
        detail: { reason: this.#reasonCode(denial) },
        bubbles: true,
        composed: true,
        cancelable: true
      })
    );
    this.#dismissedKey = this.#key(denial);
    this.requestUpdate();
    if (proceed && this.controller && this.state?.status === "denied") {
      void this.controller.start(this.state.mode);
    }
  }

  dismiss(): void {
    this.#dismissedKey = this.#key(this.#activeDenial());
    this.#expanded = false;
    this.emitKitEvent("speechkit-dismiss");
    this.requestUpdate();
  }

  // ----- rendering ------------------------------------------------------------

  #activeDenial(): SpeechKitVoiceDenial | undefined {
    if (this.#denial) return this.#denial;
    if (this.hasAttribute("reason")) return undefined;
    return this.state?.status === "denied" ? this.state.denial : undefined;
  }

  #reasonCode(denial: SpeechKitVoiceDenial | undefined): string | undefined {
    return (
      this.getAttribute("reason") ??
      denial?.reason_code ??
      (this.state?.status === "denied" && !this.#denial ? this.state.reason_code : undefined) ??
      undefined
    );
  }

  #key(denial: SpeechKitVoiceDenial | undefined): string {
    const state = this.state;
    const reason = this.getAttribute("reason") ?? denial?.reason_code ?? state?.reason_code ?? "";
    // Controller denials are keyed by the event index so a repeat denial re-shows.
    const index = !this.#denial && !this.hasAttribute("reason") ? (state?.events.length ?? 0) : 0;
    return `${reason}|${denial?.request_id ?? ""}|${index}`;
  }

  #kind(denial: SpeechKitVoiceDenial | undefined): VoiceNoticeKind {
    const reason = this.getAttribute("reason");
    if (reason) return voiceNoticeKind(reason);
    if (denial) return voiceNoticeKindForDenial(denial);
    return voiceNoticeKind(this.state?.reason_code);
  }

  protected override render(): void {
    const messages = this.msgs();
    const denial = this.#activeDenial();
    const reason = this.#reasonCode(denial);
    const visible =
      (this.hasAttribute("reason") || denial !== undefined || this.#message !== undefined) &&
      this.#dismissedKey !== this.#key(denial);
    this.toggleAttribute("data-empty", !visible);
    if (!visible) return;

    const kind = this.#kind(denial);
    const message = this.#message ? stripUrls(this.#message) : voiceNoticeMessage(kind, messages);
    const retryable = this.hasAttribute("no-retry")
      ? false
      : this.hasAttribute("retryable") || (denial?.retryable ?? isRetryableNoticeKind(kind));
    const requestId = this.getAttribute("request-id") ?? denial?.request_id;
    const steps = (denial?.user_guidance.next_steps ?? [])
      .map(stripUrls)
      .filter((step) => step.length > 0 && !/^[\w.:/-]+$/.test(step));
    const extra = this.#details ? stripUrls(this.#details) : "";

    const key = JSON.stringify([
      message, kind, retryable, requestId, steps, extra, reason, this.#expanded,
      this.hasAttribute("no-dismiss"), this.resolvedLocale()
    ]);
    if (key === this.#renderedKey) return;
    this.#renderedKey = key;

    const tone = this.getAttribute("tone");
    this.#notice.setAttribute("role", tone === "info" ? "status" : "alert");
    this.#msg.textContent = message;
    this.#msg.title = message;
    const toggleLabel = this.#expanded
      ? messages["sk.voice.notice.hide_details"]
      : messages["sk.voice.notice.details"];
    this.#toggle.setAttribute("aria-label", toggleLabel);
    this.#toggle.title = toggleLabel;
    this.#toggle.setAttribute("aria-expanded", String(this.#expanded));
    this.#retry.hidden = !retryable;
    this.#retry.textContent = messages["sk.voice.denied.retry"];
    this.#close.hidden = this.hasAttribute("no-dismiss");
    this.#close.setAttribute("aria-label", messages["sk.voice.notice.dismiss"]);
    this.#close.title = messages["sk.voice.notice.dismiss"];

    this.#detailsEl.hidden = !this.#expanded;
    this.#detailsEl.replaceChildren();
    if (this.#expanded) {
      const lines = [voiceNoticeHint(kind, messages), ...steps];
      if (extra) lines.push(extra);
      for (const line of lines) {
        const p = document.createElement("p");
        p.textContent = line;
        this.#detailsEl.append(p);
      }
      if (reason) this.#detailsEl.append(this.#meta(messages["sk.voice.notice.code"], reason));
      if (requestId) {
        this.#detailsEl.append(this.#meta(messages["sk.voice.notice.reference"], requestId));
      }
    }
  }

  #meta(label: string, value: string): HTMLParagraphElement {
    const p = document.createElement("p");
    p.className = "meta";
    p.textContent = `${label}: ${stripUrls(value)}`;
    return p;
  }
}
