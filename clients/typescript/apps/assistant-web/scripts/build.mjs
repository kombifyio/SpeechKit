#!/usr/bin/env node
/**
 * Builds the self-contained /assistant page for speechkit-server.
 *
 * esbuild bundles src/main.ts (voice-ui kit + voiceagent browser client) into
 * one IIFE, which is inlined together with the kit's tokens.css and the page
 * CSS into src/template.html. The result is written to
 * internal/server/assistantui/assets/assistant.html, which the server embeds
 * via go:embed and serves with hash-pinned CSP (the template deliberately
 * uses plain <style>/<script> blocks — the server's CSP hasher matches those
 * literal tags).
 *
 * `--check` verifies the committed output matches the build (CI drift gate).
 */
import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { buildSync } from "esbuild";

const appRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = join(appRoot, "..", "..", "..", "..");
const outPath = join(repoRoot, "internal", "server", "assistantui", "assets", "assistant.html");

const result = buildSync({
  entryPoints: [join(appRoot, "src", "main.ts")],
  bundle: true,
  format: "iife",
  platform: "browser",
  target: ["es2022"],
  minify: true,
  legalComments: "none",
  write: false,
  loader: { ".png": "dataurl" }
});
const script = result.outputFiles[0].text.trim();

const tokensCss = readFileSync(
  join(appRoot, "..", "..", "packages", "voice-ui", "src", "tokens", "tokens.css"),
  "utf8"
);
const pageCss = readFileSync(join(appRoot, "src", "page.css"), "utf8");
const style = `${tokensCss}\n${pageCss}`.trim();

const template = readFileSync(join(appRoot, "src", "template.html"), "utf8");
const html = template
  .replace("/*__APP_STYLE__*/", () => style)
  .replace("/*__APP_SCRIPT__*/", () => script);

if (html.includes("__APP_STYLE__") || html.includes("__APP_SCRIPT__")) {
  console.error("assistant-web build: placeholder replacement failed");
  process.exit(1);
}

if (process.argv.includes("--check")) {
  let current = "";
  try {
    current = readFileSync(outPath, "utf8");
  } catch {
    // Missing file fails below.
  }
  if (current.replaceAll("\r\n", "\n") !== html.replaceAll("\r\n", "\n")) {
    console.error(
      "assistant.html is out of date. Run: pnpm --filter @kombifyio/speechkit-assistant-web build"
    );
    process.exit(1);
  }
  console.log("assistant.html is up to date.");
} else {
  mkdirSync(dirname(outPath), { recursive: true });
  writeFileSync(outPath, html);
  console.log(`wrote ${outPath} (${(html.length / 1024).toFixed(1)} KB)`);
}
