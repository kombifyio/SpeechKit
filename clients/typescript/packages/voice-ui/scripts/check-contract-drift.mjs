#!/usr/bin/env node
/**
 * Drift gate for the vendored `speechkit.voice_surface.v1` contract fixture.
 *
 * The kit ships its own copy (public package; the canonical fixture lives in
 * the private desktop frontend). In the private repo this script asserts byte
 * equality against `frontend/app/src/lib/speechkit-voice-surface.v1.json`; in
 * the public mirror (where that tree is absent) it skips cleanly.
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
  "frontend",
  "app",
  "src",
  "lib",
  "speechkit-voice-surface.v1.json"
);

if (!existsSync(canonical)) {
  console.log("contract drift check skipped (canonical fixture not present — public mirror).");
  process.exit(0);
}

// Newline-insensitive: git may materialize CRLF on Windows checkouts.
const vendoredBytes = readFileSync(vendored, "utf8").replaceAll("\r\n", "\n");
const canonicalBytes = readFileSync(canonical, "utf8").replaceAll("\r\n", "\n");

if (vendoredBytes !== canonicalBytes) {
  console.error(
    "speechkit-voice-surface.v1.json drifted from frontend/app/src/lib/. " +
      "Re-copy the canonical fixture into spec/fixtures/ (additive contract changes only)."
  );
  process.exit(1);
}
console.log("contract fixture matches the canonical copy.");
