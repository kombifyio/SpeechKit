import {
  VOICE_UI_CATALOGS,
  VOICE_UI_LOCALES,
  en,
  type VoiceUiLocale,
  type VoiceUiMessageCatalog,
  type VoiceUiMessageId
} from "./catalogs.js";

export {
  VOICE_UI_CATALOGS,
  VOICE_UI_LOCALES,
  type VoiceUiLocale,
  type VoiceUiMessageCatalog,
  type VoiceUiMessageId
};

/**
 * Resolves a BCP-47 locale to a supported catalog locale: exact match first
 * (case-insensitive), then the primary language subtag, then `en`.
 */
export function resolveVoiceUiLocale(locale?: string): VoiceUiLocale {
  const normalized = locale?.trim().toLowerCase();
  if (!normalized) return "en";
  const exact = VOICE_UI_LOCALES.find((candidate) => candidate.toLowerCase() === normalized);
  if (exact) return exact;
  const primary = normalized.split("-")[0];
  if (primary === "zh") return "zh-Hans";
  const byPrimary = VOICE_UI_LOCALES.find((candidate) => candidate.toLowerCase() === primary);
  return byPrimary ?? "en";
}

/**
 * Returns the catalog for a locale with English merged underneath as the
 * fallback for missing keys, then per-key host `overrides` on top.
 */
export function voiceUiMessages(
  locale?: string,
  overrides?: Partial<Record<VoiceUiMessageId, string>>
): VoiceUiMessageCatalog {
  const resolved = VOICE_UI_CATALOGS[resolveVoiceUiLocale(locale)];
  if (resolved === en && !overrides) return en;
  return { ...en, ...resolved, ...overrides };
}

const RTL_PRIMARY_SUBTAGS = new Set(["ar", "fa", "he", "ur"]);

/** True when the locale's primary subtag is a right-to-left script language. */
export function isRtlLocale(locale?: string): boolean {
  const primary = locale?.trim().toLowerCase().split("-")[0];
  return primary !== undefined && RTL_PRIMARY_SUBTAGS.has(primary);
}
