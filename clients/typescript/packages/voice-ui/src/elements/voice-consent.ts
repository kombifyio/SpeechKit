import { SpeechKitElement } from "../core/element.js";
import {
  createVoiceConsentStore,
  VOICE_CONSENT_STORAGE_KEY,
  type VoiceConsentAdapter,
  type VoiceConsentDecision,
  type VoiceConsentScope
} from "../core/consent.js";

export {
  VOICE_CONSENT_STORAGE_KEY,
  type VoiceConsentAdapter,
  type VoiceConsentDecision,
  type VoiceConsentScope
};

/**
 * Device-only consent adapter (per-surface records under
 * `speechkit.voice.consent.v1`). Kept for compatibility; hosts that also
 * persist consent in the user's account use `createVoiceConsentStore` with
 * `onConsentChange` / `setConsentRecord` instead.
 */
export function createLocalStorageConsentAdapter(
  surface = "default",
  storageKey = VOICE_CONSENT_STORAGE_KEY
): VoiceConsentAdapter {
  return createVoiceConsentStore({ surface, storageKey });
}

const CSS = `
:host {
  display: grid;
  gap: var(--sk-gap, 8px);
  color: var(--sk-text, inherit);
  font-size: var(--sk-font-size, 13px);
  text-align: start;
}
strong {
  font-size: calc(var(--sk-font-size, 13px) + 1px);
}
p {
  margin: 0;
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
  font-size: var(--sk-font-size-small, 11px);
  line-height: 1.5;
}
.actions {
  display: flex;
  gap: var(--sk-gap, 8px);
}
button {
  border: 1px solid var(--sk-border, color-mix(in srgb, currentColor 12%, transparent));
  border-radius: var(--sk-radius, 14px);
  background: transparent;
  color: inherit;
  cursor: pointer;
  font: inherit;
  font-size: var(--sk-font-size, 13px);
  padding: 6px 12px;
}
button:focus-visible {
  outline: 2px solid var(--sk-accent, oklch(0.65 0.13 210));
  outline-offset: -2px;
}
.accept {
  border-color: transparent;
  background: var(--sk-accent, oklch(0.65 0.13 210));
  color: var(--sk-accent-contrast, #ffffff);
}
.declined {
  color: var(--sk-text-muted, color-mix(in srgb, currentColor 55%, transparent));
}
`;

/**
 * `<speechkit-voice-consent>` — fail-closed consent gate. Renders the consent
 * copy for the requested `scope` (`one_shot` default, `continuous` for
 * voice_agent) and persists the decision through the injected
 * {@link VoiceConsentAdapter}. Emits `speechkit-consent-change` with
 * `{ decision, scope }`.
 */
export class SpeechKitVoiceConsentElement extends SpeechKitElement {
  static readonly tagName = "speechkit-voice-consent";

  static override get observedAttributes(): string[] {
    return [...super.observedAttributes, "scope", "surface"];
  }

  #adapter: VoiceConsentAdapter | undefined;

  get consentAdapter(): VoiceConsentAdapter {
    if (!this.#adapter) {
      this.#adapter = createLocalStorageConsentAdapter(this.getAttribute("surface") ?? "default");
    }
    return this.#adapter;
  }

  set consentAdapter(value: VoiceConsentAdapter) {
    this.#adapter = value;
    this.requestUpdate();
  }

  get scope(): VoiceConsentScope {
    return this.getAttribute("scope") === "continuous" ? "continuous" : "one_shot";
  }

  get decision(): VoiceConsentDecision {
    return this.consentAdapter.read(this.scope);
  }

  #decide(decision: Exclude<VoiceConsentDecision, "unset">): void {
    this.consentAdapter.write(decision, this.scope);
    this.emitKitEvent("speechkit-consent-change", { decision, scope: this.scope });
    this.requestUpdate();
  }

  constructor() {
    super(CSS);
  }

  protected override render(): void {
    const messages = this.msgs();
    this.root.replaceChildren(...(this.root.querySelectorAll("style") as unknown as Element[]));
    const decision = this.decision;
    if (decision === "declined") {
      const declined = document.createElement("p");
      declined.className = "declined";
      declined.setAttribute("part", "declined");
      declined.textContent = messages["sk.voice.consent.declined"];
      const retry = document.createElement("button");
      retry.type = "button";
      retry.textContent = messages["sk.voice.consent.accept"];
      retry.addEventListener("click", () => this.#decide("granted"));
      this.root.append(declined, retry);
      return;
    }

    const title = document.createElement("strong");
    title.setAttribute("part", "title");
    title.textContent = messages["sk.voice.consent.title"];
    const capture = document.createElement("p");
    capture.textContent = messages["sk.voice.consent.capture"];
    const destination = document.createElement("p");
    destination.textContent = messages["sk.voice.consent.destination"];
    this.root.append(title, capture, destination);
    if (this.scope === "continuous") {
      const continuous = document.createElement("p");
      continuous.textContent = messages["sk.voice.consent.continuous"];
      this.root.append(continuous);
    }

    const actions = document.createElement("div");
    actions.className = "actions";
    actions.setAttribute("part", "actions");
    const accept = document.createElement("button");
    accept.type = "button";
    accept.className = "accept";
    accept.setAttribute("part", "accept");
    accept.textContent = messages["sk.voice.consent.accept"];
    accept.addEventListener("click", () => this.#decide("granted"));
    const decline = document.createElement("button");
    decline.type = "button";
    decline.setAttribute("part", "decline");
    decline.textContent = messages["sk.voice.consent.decline"];
    decline.addEventListener("click", () => this.#decide("declined"));
    actions.append(accept, decline);
    this.root.append(actions);
  }
}
