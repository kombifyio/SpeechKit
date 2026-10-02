import type { VoiceUiMessageCatalog, VoiceUiMessageId } from "../i18n/index.js";
import type { SpeechKitVoiceDenial } from "./voice-surface.js";

/**
 * User-facing notice kinds. Every reason code (kit, adapter, server, or host)
 * folds into one kind with a localized one-line message and a localized hint;
 * raw codes and URLs never reach the default view.
 */
export type VoiceNoticeKind =
  | "mic_denied"
  | "mic_unavailable"
  | "mic_busy"
  | "mic_unsupported"
  | "connection"
  | "not_available"
  | "quota"
  | "consent"
  | "generic";

const RETRYABLE_KINDS: ReadonlySet<VoiceNoticeKind> = new Set([
  "mic_denied",
  "mic_unavailable",
  "mic_busy",
  "connection",
  "generic"
]);

/** Maps a reason/error code (any casing, any producer) to a notice kind. */
export function voiceNoticeKind(code: string | undefined | null): VoiceNoticeKind {
  const value = (code ?? "").toLowerCase();
  if (!value) return "generic";
  const mic = /(^|[_.\-])(mic|microphone|audio_capture|getusermedia)/.test(value);
  if (mic || /not_?allowed|permission/.test(value)) {
    if (/unsupported|not_supported|insecure/.test(value)) return "mic_unsupported";
    if (/unavailable|not_?found|missing|no_device|no_input/.test(value)) return "mic_unavailable";
    if (/in_use|busy|not_?readable|track_start/.test(value)) return "mic_busy";
    if (/denied|not_?allowed|permission|blocked|refused/.test(value) || mic) return "mic_denied";
  }
  if (/^no_microphone$/.test(value)) return "mic_unavailable";
  if (/unsupported|insecure_context/.test(value)) return "mic_unsupported";
  if (/consent/.test(value)) return "consent";
  if (/quota|rate_?limit|limit_reached|too_many|usage_limit|budget/.test(value)) return "quota";
  if (/(server|service|upstream|gateway|provider)_?unavailable/.test(value)) return "connection";
  if (
    /entitle|plan|subscription|upgrade|forbidden|capability|not_available|unavailable|locked|feature|controller_missing|disabled/.test(
      value
    )
  ) {
    return "not_available";
  }
  if (
    /network|connect|socket|timeout|timed_out|offline|unreachable|session|server|gateway|upstream|transport|ticket|closed/.test(
      value
    )
  ) {
    return "connection";
  }
  return "generic";
}

/** Kind for a denial envelope (reason code first, then the error code). */
export function voiceNoticeKindForDenial(denial: SpeechKitVoiceDenial | undefined): VoiceNoticeKind {
  if (!denial) return "generic";
  const byReason = voiceNoticeKind(denial.reason_code);
  return byReason === "generic" ? voiceNoticeKind(denial.error_code) : byReason;
}

/** Whether a Retry affordance makes sense for the kind (absent an explicit `retryable`). */
export function isRetryableNoticeKind(kind: VoiceNoticeKind): boolean {
  return RETRYABLE_KINDS.has(kind);
}

export function voiceNoticeMessage(kind: VoiceNoticeKind, messages: VoiceUiMessageCatalog): string {
  return messages[`sk.voice.notice.${kind}` as VoiceUiMessageId];
}

export function voiceNoticeHint(kind: VoiceNoticeKind, messages: VoiceUiMessageCatalog): string {
  return messages[`sk.voice.notice.${kind}.hint` as VoiceUiMessageId];
}

const URL_PATTERN = /\b(?:[a-z][a-z0-9+.-]*:\/\/|www\.)\S+|\b[a-z0-9-]+(?:\.[a-z0-9-]+)+\/\S*/gi;

/** Removes URLs and host/path fragments from free text before it is shown. */
export function stripUrls(text: string): string {
  return text.replace(URL_PATTERN, "").replace(/\s{2,}/g, " ").trim();
}
