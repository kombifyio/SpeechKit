/**
 * @kombifyio/speechkit-voice-ui — framework-neutral SpeechKit voice UI kit.
 *
 * Side-effect-free entry: exports types, reducers, i18n, and the element
 * classes plus `registerSpeechKitElements()`. Importing this module does NOT
 * register custom elements — use the `./define` entry (or call
 * `registerSpeechKitElements()` yourself) in client-side code.
 */

export {
  SPEECHKIT_VOICE_SURFACE_VERSION,
  SPEECHKIT_VOICE_UI_VERSION,
  SPEECHKIT_VOICE_MODES,
  SPEECHKIT_CAPTURE_POLICIES,
  SPEECHKIT_TRANSPORTS,
  SPEECHKIT_VOICE_EVENT_TYPES,
  isSpeechKitVoiceEventType,
  isSpeechKitVoiceMode,
  createSpeechKitVoiceSessionState,
  reduceSpeechKitVoiceEvent,
  type SpeechKitVoiceMode,
  type SpeechKitCapturePolicy,
  type SpeechKitTransport,
  type SpeechKitVoiceEventType,
  type SpeechKitVoiceSurface,
  type SpeechKitProviderKind,
  type SpeechKitMediaTransport,
  type SpeechKitVoiceCapabilities,
  type SpeechKitVoiceSessionContext,
  type SpeechKitVoiceProviderContext,
  type SpeechKitVoiceSurfaceContract,
  type SpeechKitVoiceDenial,
  type SpeechKitVoiceEvent,
  type SpeechKitVoiceSessionStatus,
  type SpeechKitVoiceSessionState
} from "./core/voice-surface.js";

export { reduceVoiceAgentTurns, type VoiceAgentTurn } from "./core/turns.js";

export {
  VOICE_ACTIVE_STATUSES,
  isVoiceSessionActive,
  type VoiceUiController
} from "./core/controller.js";

export {
  VOICE_CONTROLLER_CONTEXT,
  ContextRequestEvent,
  requestVoiceController,
  SpeechKitVoiceProviderElement,
  type ContextCallback
} from "./core/context.js";

export { SpeechKitElement } from "./core/element.js";

export { SmoothedLevel } from "./core/level.js";

export {
  VOICE_UI_CATALOGS,
  VOICE_UI_LOCALES,
  resolveVoiceUiLocale,
  voiceUiMessages,
  isRtlLocale,
  type VoiceUiLocale,
  type VoiceUiMessageCatalog,
  type VoiceUiMessageId
} from "./i18n/index.js";

export {
  SpeechKitVoiceButtonElement
} from "./elements/voice-button.js";
export {
  SpeechKitVoiceConsentElement,
  createLocalStorageConsentAdapter,
  VOICE_CONSENT_STORAGE_KEY,
  type VoiceConsentAdapter,
  type VoiceConsentDecision,
  type VoiceConsentScope
} from "./elements/voice-consent.js";
export { SpeechKitVoiceOverlayElement } from "./elements/voice-overlay.js";
export {
  SpeechKitVoiceVisualizerElement,
  sessionStatusToVisualizerState,
  type VoiceVisualizerState
} from "./elements/voice-visualizer.js";
export {
  SpeechKitVoiceAssistantElement,
  sessionStatusToAuraState,
  type VoiceAssistantSize,
  type VoiceAssistantFrame,
  type VoiceAssistantStatus,
  type VoiceAuraState
} from "./elements/voice-assistant.js";

import { SpeechKitVoiceProviderElement } from "./core/context.js";
import { SpeechKitVoiceButtonElement } from "./elements/voice-button.js";
import { SpeechKitVoiceConsentElement } from "./elements/voice-consent.js";
import { SpeechKitVoiceOverlayElement } from "./elements/voice-overlay.js";
import { SpeechKitVoiceVisualizerElement } from "./elements/voice-visualizer.js";
import { SpeechKitVoiceAssistantElement } from "./elements/voice-assistant.js";

/** Registers all kit elements (idempotent). */
export function registerSpeechKitElements(): void {
  const definitions: Array<[string, CustomElementConstructor]> = [
    [SpeechKitVoiceProviderElement.tagName, SpeechKitVoiceProviderElement],
    [SpeechKitVoiceVisualizerElement.tagName, SpeechKitVoiceVisualizerElement],
    [SpeechKitVoiceConsentElement.tagName, SpeechKitVoiceConsentElement],
    [SpeechKitVoiceButtonElement.tagName, SpeechKitVoiceButtonElement],
    [SpeechKitVoiceOverlayElement.tagName, SpeechKitVoiceOverlayElement],
    [SpeechKitVoiceAssistantElement.tagName, SpeechKitVoiceAssistantElement]
  ];
  for (const [tag, ctor] of definitions) {
    if (!customElements.get(tag)) customElements.define(tag, ctor);
  }
}
