import fs from "node:fs";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const modulePath = fileURLToPath(import.meta.url);
const repoRoot = path.resolve(path.dirname(modulePath), "..");
const rootPackagePath = path.join(repoRoot, "package.json");

export function parseArgs(argv) {
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

// The TypeScript client manifests that carry the SpeechKit product line.
// packages/voice-ui-lab and apps/assistant-web are deliberately absent: both
// are private:true and neither is a delivered artifact.
export const CLIENT_MANIFESTS = [
  "clients/typescript/package.json",
  "clients/typescript/packages/client/package.json",
  "clients/typescript/packages/voice-ui/package.json",
  "clients/typescript/packages/voiceagent-client/package.json",
];

export function resolveTargets(rawTargets) {
  const requested = new Set(
    (rawTargets ?? "frontend,windows")
      .split(",")
      .map((value) => value.trim().toLowerCase())
      .filter(Boolean),
  );

  if (requested.has("all")) {
    return new Set(["frontend", "windows", "android", "clients"]);
  }

  const allowed = new Set(["frontend", "windows", "android", "clients"]);
  for (const target of requested) {
    if (!allowed.has(target)) {
      throw new Error(`Unknown sync target: ${target}`);
    }
  }
  return requested;
}

export function resolveVersionMetadata(version) {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/.exec(version);
  if (!match) {
    throw new Error(`Invalid semver version: ${version}`);
  }

  const major = Number.parseInt(match[1], 10);
  const minor = Number.parseInt(match[2], 10);
  const patch = Number.parseInt(match[3], 10);
  const releaseVersion = `${major}.${minor}.${patch}`;

  return {
    packageVersion: version,
    releaseVersion,
    windowsManifestVersion: `${releaseVersion}.0`,
    androidVersionCode: major * 10000 + minor * 100 + patch,
    previewLabel: version.includes("-") ? `v${major}.${minor} Preview` : "",
  };
}

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

export function syncVersion(argv = process.argv.slice(2)) {
  const args = parseArgs(argv);
  const rootPackage = JSON.parse(fs.readFileSync(rootPackagePath, "utf8"));
  const version = args.get("version") ?? rootPackage.version;
  const targets = resolveTargets(args.get("targets"));
  const metadata = resolveVersionMetadata(version);

  updateJson("package.json", (data) => {
    data.version = metadata.packageVersion;
  });

  if (fs.existsSync(path.join(repoRoot, "package-lock.json"))) {
    updateJson("package-lock.json", (data) => {
      data.version = metadata.packageVersion;
      if (data.packages && data.packages[""]) {
        data.packages[""].version = metadata.packageVersion;
      }
    });
  }

  // CI-CD-PLATFORM-STANDARD.md §4.2: .kombify/VERSION is the line anchor, not
  // a release counter. Below 1.0.0 the Delivery Platform reads the declared
  // value and derives the delivered version from the first-parent commit count
  // since the anchor moved. The anchor standing still while the line advances
  // is the correct steady state, and a second writer re-anchors the derivation
  // and throws away every iteration accumulated since the last bump.
  //
  // Two kinds of caller reach this write, and they need opposite behaviour:
  //   - A distribution lane (publish-oss.yml, release-android.yml) stamps the
  //     already-resolved delivery version into the runner checkout so the build
  //     files carry it. §4.2 requires that stamp; it is a scratch write inside
  //     a throwaway checkout and is never committed.
  //   - A release-authoring caller (release-please.yml) prepares a commit. It
  //     must not write the anchor, because that is the forbidden second minter.
  //     Such callers pass --preserve-product-version=true.
  if (
    args.get("preserve-product-version") !== "true" &&
    fs.existsSync(path.join(repoRoot, ".kombify/VERSION"))
  ) {
    updateText(".kombify/VERSION", [[/^.*$/m, metadata.packageVersion]]);
  }

  if (targets.has("frontend")) {
    updateJson("frontend/app/package.json", (data) => {
      data.version = metadata.packageVersion;
    });

    if (fs.existsSync(path.join(repoRoot, "frontend/app/package-lock.json"))) {
      updateJson("frontend/app/package-lock.json", (data) => {
        data.version = metadata.packageVersion;
        if (data.packages && data.packages[""]) {
          data.packages[""].version = metadata.packageVersion;
        }
      });
    }
  }

  if (targets.has("windows")) {
    updateJson("cmd/speechkit/winres.json", (data) => {
      data.RT_MANIFEST["#1"]["0409"].identity.version = metadata.windowsManifestVersion;
    });

    // AppVersion is injected via -ldflags in build.ps1 from package.json.
    // No source file edit needed.

    updateText("installer/speechkit.nsi", [
      [/!define VERSION ".*"/, `!define VERSION "${metadata.packageVersion}"`],
      [/WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\kombify SpeechKit" "DisplayVersion" ".*"/, 'WriteRegStr HKCU "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\kombify SpeechKit" "DisplayVersion" "${VERSION}"'],
    ]);
  }

  // CI-CD-PLATFORM-STANDARD.md §4.2: every distribution lane hands the
  // resolved version to the build explicitly. The published client packages
  // are not an independently versioned library monorepo — every artifact on
  // the registry carries the SpeechKit product line — so the lanes that
  // publish them, and the OSS export that mirrors their manifests, stamp the
  // delivery version here instead of trusting the committed default.
  if (targets.has("clients")) {
    for (const manifest of CLIENT_MANIFESTS) {
      if (!fs.existsSync(path.join(repoRoot, manifest))) {
        continue;
      }
      updateJson(manifest, (data) => {
        data.version = metadata.packageVersion;
      });
    }
  }

  if (targets.has("android")) {
    if (fs.existsSync(path.join(repoRoot, "android/app/build.gradle.kts"))) {
      updateText("android/app/build.gradle.kts", [
        [/versionCode = \d+/, `versionCode = ${metadata.androidVersionCode}`],
        [/versionName = ".*"/, `versionName = "${metadata.packageVersion}"`],
      ]);
    }
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  syncVersion();
}
