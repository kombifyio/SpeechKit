import type { VoiceUiController } from "./controller.js";
import { requestVoiceController } from "./context.js";
import { upgradeOwnProperties } from "./upgrade.js";
import {
  voiceUiMessages,
  type VoiceUiMessageCatalog,
  type VoiceUiMessageId
} from "../i18n/index.js";
import type { SpeechKitVoiceSessionState } from "./voice-surface.js";

const styleSheetCache = new Map<string, CSSStyleSheet>();

function supportsAdoptedStyleSheets(root: ShadowRoot): boolean {
  return (
    "adoptedStyleSheets" in root &&
    typeof CSSStyleSheet !== "undefined" &&
    typeof CSSStyleSheet.prototype.replaceSync === "function"
  );
}

/**
 * Base class for the kit's custom elements: open shadow root, style adoption
 * (constructable stylesheets with a `<style>` fallback), microtask-batched
 * rendering, locale/messages resolution, and the controller slot (direct
 * property assignment with a context-protocol fallback to
 * `<speechkit-voice-provider>`).
 */
export abstract class SpeechKitElement extends HTMLElement {
  static get observedAttributes(): string[] {
    return ["locale"];
  }

  protected readonly root: ShadowRoot;

  #controller: VoiceUiController | null = null;
  #explicitController = false;
  #unsubscribeState: (() => void) | undefined;
  #unsubscribeContext: (() => void) | undefined;
  #renderQueued = false;
  #connected = false;
  #messagesOverride: Partial<Record<VoiceUiMessageId, string>> | undefined;

  protected state: SpeechKitVoiceSessionState | undefined;

  constructor(css: string) {
    super();
    this.root = this.attachShadow({ mode: "open" });
    this.#adoptStyles(css);
  }

  #adoptStyles(css: string): void {
    if (supportsAdoptedStyleSheets(this.root)) {
      let sheet = styleSheetCache.get(css);
      if (!sheet) {
        sheet = new CSSStyleSheet();
        sheet.replaceSync(css);
        styleSheetCache.set(css, sheet);
      }
      this.root.adoptedStyleSheets = [...this.root.adoptedStyleSheets, sheet];
      return;
    }
    const style = document.createElement("style");
    style.textContent = css;
    this.root.append(style);
  }

  /** Host-injected controller. Assigning takes precedence over context. */
  get controller(): VoiceUiController | null {
    return this.#controller;
  }

  set controller(value: VoiceUiController | null) {
    this.#explicitController = true;
    this.#setController(value);
  }

  /** Per-key message overrides merged over the resolved locale catalog. */
  get messages(): Partial<Record<VoiceUiMessageId, string>> | undefined {
    return this.#messagesOverride;
  }

  set messages(value: Partial<Record<VoiceUiMessageId, string>> | undefined) {
    this.#messagesOverride = value;
    this.requestUpdate();
  }

  #setController(value: VoiceUiController | null): void {
    if (this.#controller === value) return;
    this.#unsubscribeState?.();
    this.#unsubscribeState = undefined;
    this.#controller = value;
    if (value && this.#connected) this.#subscribeController(value);
    this.onControllerChanged(value);
    this.requestUpdate();
  }

  #subscribeController(controller: VoiceUiController): void {
    this.#unsubscribeState = controller.subscribe((state) => {
      this.state = state;
      this.onSessionState(state);
      this.requestUpdate();
    });
  }

  /** Hook for subclasses; called after the controller reference changed. */
  protected onControllerChanged(_controller: VoiceUiController | null): void {}

  /** Hook for subclasses; called on every controller state emission. */
  protected onSessionState(_state: SpeechKitVoiceSessionState): void {}

  protected resolvedLocale(): string | undefined {
    return (
      this.getAttribute("locale") ??
      (this.closest("[lang]") as HTMLElement | null)?.lang ??
      document.documentElement.lang ??
      undefined
    );
  }

  protected msgs(): VoiceUiMessageCatalog {
    return voiceUiMessages(this.resolvedLocale(), this.#messagesOverride);
  }

  protected emitKitEvent<T>(name: string, detail?: T): void {
    this.dispatchEvent(new CustomEvent(name, { detail, bubbles: true, composed: true }));
  }

  requestUpdate(): void {
    if (this.#renderQueued || !this.#connected) return;
    this.#renderQueued = true;
    queueMicrotask(() => {
      this.#renderQueued = false;
      if (this.#connected) this.render();
    });
  }

  protected abstract render(): void;

  connectedCallback(): void {
    upgradeOwnProperties(this);
    this.#connected = true;
    if (!this.#explicitController && !this.#controller) {
      this.#unsubscribeContext = requestVoiceController(this, (controller) => {
        if (!this.#explicitController) this.#setController(controller);
      });
    } else if (this.#controller && !this.#unsubscribeState) {
      this.#subscribeController(this.#controller);
    }
    this.render();
  }

  disconnectedCallback(): void {
    this.#connected = false;
    this.#unsubscribeState?.();
    this.#unsubscribeState = undefined;
    this.#unsubscribeContext?.();
    this.#unsubscribeContext = undefined;
  }

  attributeChangedCallback(): void {
    this.requestUpdate();
  }
}
