/** Paraglide-backed catalogs. JSON in locales/ is the native interchange source. */
import { catalog as enMessages } from "../paraglide/catalogs/en.js";
import { catalog as deMessages } from "../paraglide/catalogs/de.js";
import { catalog as esMessages } from "../paraglide/catalogs/es.js";
import { catalog as zhMessages } from "../paraglide/catalogs/zh-Hans.js";
import { catalog as hiMessages } from "../paraglide/catalogs/hi.js";
import { catalog as arMessages } from "../paraglide/catalogs/ar.js";
import { renderCatalog, type VoiceUiMessageId, type VoiceUiMessageCatalog } from "./message-ids.js";

export type { VoiceUiMessageId, VoiceUiMessageCatalog };
export const VOICE_UI_LOCALES = ["en", "de", "es", "zh-Hans", "hi", "ar"] as const;
export type VoiceUiLocale = (typeof VOICE_UI_LOCALES)[number];
export const en = renderCatalog(enMessages);
export const de = renderCatalog(deMessages);
export const es = renderCatalog(esMessages);
export const zhHans = renderCatalog(zhMessages);
export const hi = renderCatalog(hiMessages);
export const ar = renderCatalog(arMessages);
export const VOICE_UI_CATALOGS: Record<VoiceUiLocale, VoiceUiMessageCatalog> = {
  en, de, es, "zh-Hans": zhHans, hi, ar,
};
