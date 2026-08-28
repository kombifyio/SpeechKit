#!/usr/bin/env node
/**
 * Drift gate for the vendored `speechkit.voice_surface.v1` contract fixture.
 *
 * The kit ships its own copy so the published package is self-contained. This
 * script asserts byte equality against the canonical fixture in
 * `docs/server/contracts/`, which is exported to the public mirror, so the
 * gate runs in both repositories instead of skipping in one.
 */
import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const vendored = join(root, "spec", "fixtures", "speechkit-voice-surface.v1.json");
const canonical = join(
  root,
  "..",
  "..",
  "..",
  "..",
  "docs",
  "server",
  "contracts",
  "speechkit-voice-surface.v1.json"
);

if (!existsSync(canonical)) {
  console.error(
    `canonical contract missing at ${canonical}. It is exported to the public ` +
      "mirror, so its absence is a broken export, not a reason to skip."
  );
  process.exit(1);
}

// Newline-insensitive: git may materialize CRLF on Windows checkouts.
const vendoredBytes = readFileSync(vendored, "utf8").replaceAll("\r\n", "\n");
const canonicalBytes = readFileSync(canonical, "utf8").replaceAll("\r\n", "\n");

if (vendoredBytes !== canonicalBytes) {
  console.error(
    "speechkit-voice-surface.v1.json drifted from docs/server/contracts/. " +
      "Re-copy the canonical fixture into spec/fixtures/ (additive contract changes only)."
  );
  process.exit(1);
}
console.log("contract fixture matches the canonical copy.");
