#!/usr/bin/env node
/**
 * Bundles the side-effectful define entry into single-file CDN artifacts for
 * plain-HTML consumers: ESM (`speechkit-voice-ui.min.js`) and IIFE
 * (`speechkit-voice-ui.iife.min.js`, global `SpeechKitVoiceUi`).
 */
import { build } from "esbuild";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const entry = join(root, "src", "define.ts");
const outDir = join(root, "dist", "cdn");

await build({
  entryPoints: [entry],
  bundle: true,
  minify: true,
  format: "esm",
  target: "es2022",
  outfile: join(outDir, "speechkit-voice-ui.min.js")
});

await build({
  entryPoints: [entry],
  bundle: true,
  minify: true,
  format: "iife",
  globalName: "SpeechKitVoiceUi",
  target: "es2022",
  outfile: join(outDir, "speechkit-voice-ui.iife.min.js")
});

console.log("wrote dist/cdn bundles");
