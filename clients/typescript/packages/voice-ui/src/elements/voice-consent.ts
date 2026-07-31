import { SpeechKitElement } from "../core/element.js";

export type VoiceConsentScope = "one_shot" | "continuous";
export type VoiceConsentDecision = "granted" | "declined" | "unset";

/**
 * Consent persistence boundary. The default adapter stores per-surface records
 * under `speechkit.voice.consent.v1` in localStorage with the fail-closed
 * scope semantics of the Floating Panel reference (`kombify.voice.consent.v1`):
 * granting "continuous" implies "one_shot"; a plain one-shot grant never
 * implies "continuous"; declining revokes every scope.
 */
export interface VoiceConsentAdapter {
  read(scope: VoiceConsentScope): VoiceConsentDecision;
  write(decision: Exclude<VoiceConsentDecision, "unset">, scope: VoiceConsentScope): void;
}

export const VOICE_CONSENT_STORAGE_KEY = "speechkit.voice.consent.v1";

interface ConsentRecord {
  decision?: unknown;
  scopes?: unknown;
}

export function createLocalStorageConsentAdapter(
  surface = "default",
  storageKey = VOICE_CONSENT_STORAGE_KEY
): VoiceConsentAdapter {
  function storage(): Storage | undefined {
    try {
      return globalThis.localStorage;
    } catch {
      // Sandboxed iframes without allow-same-origin throw on access.
      return undefined;
    }
  }
  return {
    read(scope) {
      const store = storage();
      if (!store) return "unset";
      try {
        const raw = store.getItem(storageKey);
        if (!raw) return "unset";
        const parsed = JSON.parse(raw) as Record<string, ConsentRecord | undefined>;
        const record = parsed?.[surface];
        const decision = record?.decision;
        if (decision !== "granted" && decision !== "declined") return "unset";
        if (decision === "declined") return "declined";
        if (scope === "one_shot") return "granted";
        const scopes = Array.isArray(record?.scopes) ? record.scopes : [];
        return scopes.includes("continuous") ? "granted" : "unset";
      } catch {
        return "unset";
      }
    },
    write(decision, scope) {
      const store = storage();
      if (!store) return;
      let record: Record<string, unknown> = {};
      try {
        const parsed: unknown = JSON.parse(store.getItem(storageKey) ?? "{}");
        if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
          record = parsed as Record<string, unknown>;
        }
      } catch {
        // Corrupted records are replaced.
      }
      const previous = record[surface] as ConsentRecord | undefined;
      const previousScopes = Array.isArray(previous?.scopes)
        ? previous.scopes.filter((entry): entry is string => typeof entry === "string")
        : [];
      const scopes =
        decision === "declined"
          ? []
          : [
              ...new Set([
                ...previousScopes,
                "one_shot",
                ...(scope === "continuous" ? ["continuous"] : [])
              ])
            ];
      record[surface] = { decision, decided_at: new Date().toISOString(), scopes };
      try {
        store.setItem(storageKey, JSON.stringify(record));
      } catch {
        // Quota/security errors: the in-memory decision still applies this session.
      }
    }
  };
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
