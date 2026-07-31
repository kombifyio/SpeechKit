#!/usr/bin/env node
/**
 * Generates src/tokens/tokens.css from src/tokens/tokens.json (SSOT).
 * `--check` verifies the committed CSS matches the generator output (wired
 * into the build so the two files can never drift).
 */
import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const tokensPath = join(root, "src", "tokens", "tokens.json");
const cssPath = join(root, "src", "tokens", "tokens.css");

const tokens = JSON.parse(readFileSync(tokensPath, "utf8"));
const prefix = tokens.prefix;

function block(vars, indent) {
  return Object.entries(vars)
    .map(([name, value]) => `${indent}${prefix}${name}: ${value};`)
    .join("\n");
}

function renderTokensCss(doc) {
  const shared = { ...doc.shared };
  const light = doc.themes.light;
  const dark = doc.themes.dark;
  return `/* GENERATED FILE — do not edit. Source: tokens.json (scripts/build-tokens.mjs). */

:root {
${block(shared, "  ")}
${block(light, "  ")}
}

@media (prefers-color-scheme: dark) {
  :root:not([data-sk-theme="light"]) {
${block(dark, "    ")}
  }
}

[data-sk-theme="dark"] {
${block(dark, "  ")}
}

[data-sk-theme="light"] {
${block(light, "  ")}
}
`;
}

const rendered = renderTokensCss(tokens);

if (process.argv.includes("--check")) {
  let current = "";
  try {
    current = readFileSync(cssPath, "utf8");
  } catch {
    // Missing file fails the check below.
  }
  // Newline-insensitive: git may materialize CRLF on Windows checkouts.
  if (current.replaceAll("\r\n", "\n") !== rendered) {
    console.error("tokens.css is out of date. Run: node scripts/build-tokens.mjs");
    process.exit(1);
  }
  console.log("tokens.css is up to date.");
} else {
  writeFileSync(cssPath, rendered);
  console.log(`wrote ${cssPath}`);
}
