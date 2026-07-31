#!/usr/bin/env node
/**
 * Emits locales/<locale>.json from the built catalogs (dist/i18n/catalogs.js)
 * so native (Compose) spec parity ships the identical strings. Runs after tsc
 * in the build; `--check` verifies the committed JSON matches.
 */
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const outDir = join(root, "locales");
const catalogsUrl = pathToFileURL(join(root, "dist", "i18n", "catalogs.js")).href;

const { VOICE_UI_CATALOGS } = await import(catalogsUrl);

const check = process.argv.includes("--check");
let stale = false;
mkdirSync(outDir, { recursive: true });

for (const [locale, catalog] of Object.entries(VOICE_UI_CATALOGS)) {
  const path = join(outDir, `${locale}.json`);
  const rendered = `${JSON.stringify(catalog, null, 2)}\n`;
  if (check) {
    let current = "";
    try {
      current = readFileSync(path, "utf8");
    } catch {
      // Missing file counts as stale.
    }
    // Newline-insensitive: git may materialize CRLF on Windows checkouts.
    if (current.replaceAll("\r\n", "\n") !== rendered) {
      console.error(`locales/${locale}.json is out of date.`);
      stale = true;
    }
  } else {
    writeFileSync(path, rendered);
    console.log(`wrote locales/${locale}.json`);
  }
}

if (check) {
  if (stale) {
    console.error("Run: pnpm run build:locales (after tsc)");
    process.exit(1);
  }
  console.log("locale JSON copies are up to date.");
}
