import { describe, expect, it } from "vitest";
import {
  VOICE_UI_CATALOGS,
  VOICE_UI_LOCALES,
  isRtlLocale,
  resolveVoiceUiLocale,
  voiceUiMessages
} from "../src/i18n/index.js";
import { en } from "../src/i18n/catalogs.js";

describe("locale resolution", () => {
  it("resolves exact, primary-subtag, zh special case, and en fallback", () => {
    expect(resolveVoiceUiLocale("de-AT")).toBe("de");
    expect(resolveVoiceUiLocale("zh-Hans")).toBe("zh-Hans");
    expect(resolveVoiceUiLocale("ZH-hans")).toBe("zh-Hans");
    expect(resolveVoiceUiLocale("zh-TW")).toBe("zh-Hans");
    expect(resolveVoiceUiLocale("fr-FR")).toBe("en");
    expect(resolveVoiceUiLocale(undefined)).toBe("en");
    expect(resolveVoiceUiLocale("ar-EG")).toBe("ar");
  });

  it("merges English underneath and host overrides on top", () => {
    const withOverride = voiceUiMessages("de", { "sk.voice.agent.you": "Ich" });
    expect(withOverride["sk.voice.agent.you"]).toBe("Ich");
    expect(withOverride["sk.voice.agent.exit"]).toBe("Sprachmodus beenden");
  });

  it("flags RTL primary subtags only", () => {
    expect(isRtlLocale("ar")).toBe(true);
    expect(isRtlLocale("ar-EG")).toBe(true);
    expect(isRtlLocale("de")).toBe(false);
    expect(isRtlLocale(undefined)).toBe(false);
  });
});

describe("catalog completeness", () => {
  const keys = Object.keys(en).sort();
  for (const locale of VOICE_UI_LOCALES) {
    it(`${locale} carries every message id with non-empty text`, () => {
      const catalog = VOICE_UI_CATALOGS[locale];
      expect(Object.keys(catalog).sort()).toEqual(keys);
      for (const value of Object.values(catalog)) {
        expect(value.length).toBeGreaterThan(0);
      }
    });
  }
});
