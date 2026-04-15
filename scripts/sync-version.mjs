import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const rootPackagePath = path.join(repoRoot, "package.json");

function parseArgs(argv) {
  const args = new Map();
  for (const entry of argv) {
    if (!entry.startsWith("--")) {
      continue;
    }
    const trimmed = entry.slice(2);
    const separatorIndex = trimmed.indexOf("=");
    if (separatorIndex === -1) {
      args.set(trimmed, "true");
      continue;
    }
    const key = trimmed.slice(0, separatorIndex);
    const value = trimmed.slice(separatorIndex + 1);
    args.set(key, value);
  }
  return args;
}

function resolveTargets(rawTargets) {
  const requested = new Set(
    (rawTargets ?? "frontend,windows")
      .split(",")
      .map((value) => value.trim().toLowerCase())
      .filter(Boolean),
  );

  if (requested.has("all")) {
    return new Set(["frontend", "windows", "android"]);
  }

  const allowed = new Set(["frontend", "windows", "android"]);
  for (const target of requested) {
    if (!allowed.has(target)) {
      throw new Error(`Unknown sync target: ${target}`);
    }
  }
  return requested;
}

const args = parseArgs(process.argv.slice(2));
const rootPackage = JSON.parse(fs.readFileSync(rootPackagePath, "utf8"));
const version = args.get("version") ?? rootPackage.version;
const targets = resolveTargets(args.get("targets"));

const [majorRaw, minorRaw, patchRaw] = version.split(".");
const major = Number.parseInt(majorRaw ?? "0", 10);
const minor = Number.parseInt(minorRaw ?? "0", 10);
const patch = Number.parseInt(patchRaw ?? "0", 10);
const androidVersionCode = major * 10000 + minor * 100 + patch;
const windowsManifestVersion = `${version}.0`;

function updateJson(filePath, updater) {
  const fullPath = path.join(repoRoot, filePath);
  const data = JSON.parse(fs.readFileSync(fullPath, "utf8"));
  updater(data);
  fs.writeFileSync(fullPath, `${JSON.stringify(data, null, 2)}\n`);
}

function updateText(filePath, replacements) {
  const fullPath = path.join(repoRoot, filePath);
  let text = fs.readFileSync(fullPath, "utf8");
  for (const [pattern, replacement] of replacements) {
    text = text.replace(pattern, replacement);
  }
  fs.writeFileSync(fullPath, text);
}

if (fs.existsSync(path.join(repoRoot, "package-lock.json"))) {
  updateJson("package-lock.json", (data) => {
    data.version = version;
    if (data.packages && data.packages[""]) {
      data.packages[""].version = version;
    }
  });
}

if (targets.has("frontend")) {
  updateJson("frontend/app/package.json", (data) => {
    data.version = version;
  });

  if (fs.existsSync(path.join(repoRoot, "frontend/app/package-lock.json"))) {
    updateJson("frontend/app/package-lock.json", (data) => {
      data.version = version;
      if (data.packages && data.packages[""]) {
        data.packages[""].version = version;
      }
    });
  }
}

if (targets.has("windows")) {
  updateJson("cmd/speechkit/winres.json", (data) => {
    data.RT_MANIFEST["#1"]["0409"].identity.version = windowsManifestVersion;
  });

  // AppVersion is injected via -ldflags in build.ps1 from package.json.
  // No source file edit needed.

  updateText("installer/speechkit.nsi", [
    [/!define VERSION ".*"/, `!define VERSION "${version}"`],
    [/WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\kombify SpeechKit" "DisplayVersion" ".*"/, 'WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\kombify SpeechKit" "DisplayVersion" "${VERSION}"'],
  ]);
}

if (targets.has("android")) {
  updateText("android/app/build.gradle.kts", [
    [/versionCode = \d+/, `versionCode = ${androidVersionCode}`],
    [/versionName = ".*"/, `versionName = "${version}"`],
  ]);
}
