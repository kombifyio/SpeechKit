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
  RecordingIndicator,
  indicatorBarHeights,
  sessionStatusToIndicatorState,
  type RecordingIndicatorState
} from "./core/indicator.js";

export {
  checkMicrophonePermission,
  requestMicrophone,
  releaseMediaStream,
  microphoneReasonFromError,
  type CheckMicrophonePermissionOptions,
  type MicrophonePermissionState,
  type MicrophoneReasonCode,
  type MicrophoneRequestResult
} from "./core/permission.js";

export {
  createVoiceConsentStore,
  VOICE_CONSENT_DEFAULT_VERSION,
  type VoiceConsentRecord,
  type VoiceConsentStore,
  type VoiceConsentStoreOptions
} from "./core/consent.js";

export {
  voiceNoticeKind,
  voiceNoticeKindForDenial,
  voiceNoticeMessage,
  voiceNoticeHint,
  isRetryableNoticeKind,
  type VoiceNoticeKind
} from "./core/notice.js";

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
  SpeechKitVoiceNoticeElement,
  type VoiceNoticeTone
} from "./elements/voice-notice.js";
export { SpeechKitLiveTranscriptElement } from "./elements/live-transcript.js";
export {
  SpeechKitVoiceDialogElement,
  type VoiceDialogAnchor
} from "./elements/voice-dialog.js";
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
  type VoiceAssistantVariant,
  type VoiceAssistantStatus,
  type VoiceAuraState
} from "./elements/voice-assistant.js";

export {
  SEMANTIC_VOICE_MARKS,
  isSemanticVoiceMark,
  resolveMarkSrc,
  semanticMarkRatio,
  type SemanticVoiceMark,
  type SemanticMarkAssets
} from "./marks.js";

import { SpeechKitVoiceProviderElement } from "./core/context.js";
import { SpeechKitVoiceButtonElement } from "./elements/voice-button.js";
import { SpeechKitVoiceConsentElement } from "./elements/voice-consent.js";
import { SpeechKitVoiceOverlayElement } from "./elements/voice-overlay.js";
import { SpeechKitVoiceVisualizerElement } from "./elements/voice-visualizer.js";
import { SpeechKitVoiceAssistantElement } from "./elements/voice-assistant.js";
import { SpeechKitVoiceNoticeElement } from "./elements/voice-notice.js";
import { SpeechKitLiveTranscriptElement } from "./elements/live-transcript.js";
import { SpeechKitVoiceDialogElement } from "./elements/voice-dialog.js";

/** Registers all kit elements (idempotent). */
export function registerSpeechKitElements(): void {
  const definitions: Array<[string, CustomElementConstructor]> = [
    [SpeechKitVoiceProviderElement.tagName, SpeechKitVoiceProviderElement],
    [SpeechKitVoiceNoticeElement.tagName, SpeechKitVoiceNoticeElement],
    [SpeechKitLiveTranscriptElement.tagName, SpeechKitLiveTranscriptElement],
    [SpeechKitVoiceVisualizerElement.tagName, SpeechKitVoiceVisualizerElement],
    [SpeechKitVoiceConsentElement.tagName, SpeechKitVoiceConsentElement],
    [SpeechKitVoiceButtonElement.tagName, SpeechKitVoiceButtonElement],
    [SpeechKitVoiceOverlayElement.tagName, SpeechKitVoiceOverlayElement],
    [SpeechKitVoiceDialogElement.tagName, SpeechKitVoiceDialogElement],
    [SpeechKitVoiceAssistantElement.tagName, SpeechKitVoiceAssistantElement]
  ];
  for (const [tag, ctor] of definitions) {
    if (!customElements.get(tag)) customElements.define(tag, ctor);
  }
}
