import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { VOICE_UI_CATALOGS, VOICE_UI_LOCALES } from "../src/i18n/catalogs.js";

const localesDir = join(dirname(fileURLToPath(import.meta.url)), "..", "locales");

describe("locale JSON parity copies", () => {
  for (const locale of VOICE_UI_LOCALES) {
    it(`locales/${locale}.json equals the TS catalog`, () => {
      const json = JSON.parse(readFileSync(join(localesDir, `${locale}.json`), "utf8"));
      expect(json).toEqual(VOICE_UI_CATALOGS[locale]);
    });
  }
});
