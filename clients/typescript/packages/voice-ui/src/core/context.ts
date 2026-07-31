import type { VoiceUiController } from "./controller.js";
import { upgradeOwnProperties } from "./upgrade.js";

/**
 * Minimal implementation of the WCCG community context protocol
 * (https://github.com/webcomponents-cg/community-protocols/blob/main/proposals/context.md)
 * so hosts can provide one controller to a subtree without prop-drilling:
 * plain pages wrap kit elements in `<speechkit-voice-provider>`; framework
 * hosts may instead assign the `controller` property directly on each element.
 */
export const VOICE_CONTROLLER_CONTEXT = "speechkit-voice-controller" as const;

export type ContextCallback<T> = (value: T, unsubscribe?: () => void) => void;

export class ContextRequestEvent<T> extends Event {
  readonly context: string;
  readonly callback: ContextCallback<T>;
  readonly subscribe?: boolean;

  constructor(context: string, callback: ContextCallback<T>, subscribe?: boolean) {
    super("context-request", { bubbles: true, composed: true });
    this.context = context;
    this.callback = callback;
    if (subscribe !== undefined) this.subscribe = subscribe;
  }
}

/**
 * Requests the shared controller from an ancestor provider. Returns an
 * unsubscribe function (no-op when no provider answered synchronously).
 */
export function requestVoiceController(
  host: HTMLElement,
  onController: (controller: VoiceUiController | null) => void
): () => void {
  let unsubscribe: (() => void) | undefined;
  host.dispatchEvent(
    new ContextRequestEvent<VoiceUiController | null>(
      VOICE_CONTROLLER_CONTEXT,
      (value, unsub) => {
        unsubscribe = unsub;
        onController(value);
      },
      true
    )
  );
  return () => unsubscribe?.();
}

/**
 * `<speechkit-voice-provider>` — provides a {@link VoiceUiController} to all
 * descendant kit elements via the context protocol. Reassigning `controller`
 * re-notifies every subscribed descendant.
 */
export class SpeechKitVoiceProviderElement extends HTMLElement {
  static readonly tagName = "speechkit-voice-provider";

  #controller: VoiceUiController | null = null;
  #subscribers = new Set<ContextCallback<VoiceUiController | null>>();
  #onContextRequest = (event: Event): void => {
    const request = event as ContextRequestEvent<VoiceUiController | null>;
    if (request.context !== VOICE_CONTROLLER_CONTEXT) return;
    if (request.target === this) return;
    event.stopPropagation();
    if (request.subscribe) {
      const callback = request.callback;
      this.#subscribers.add(callback);
      callback(this.#controller, () => this.#subscribers.delete(callback));
      return;
    }
    request.callback(this.#controller);
  };

  get controller(): VoiceUiController | null {
    return this.#controller;
  }

  set controller(value: VoiceUiController | null) {
    this.#controller = value;
    for (const callback of this.#subscribers) callback(value);
  }

  connectedCallback(): void {
    upgradeOwnProperties(this);
    this.addEventListener("context-request", this.#onContextRequest);
  }

  disconnectedCallback(): void {
    this.removeEventListener("context-request", this.#onContextRequest);
    this.#subscribers.clear();
  }
}
