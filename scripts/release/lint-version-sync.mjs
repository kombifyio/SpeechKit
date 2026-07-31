// Verify that the root package.json version matches the most recent
// non-Unreleased entry in CHANGELOG.md. Numeric pre-1.0 feedback builds may
// opt into a newer derived patch within the same authored minor line; minor
// changes and every 1.0+ release remain exact.
//
// Usage:
//   node scripts/release/lint-version-sync.mjs [--allow-derived-pre-1-patch]
//
// Exits 0 if the versions are aligned, 1 if they drift, 2 on
// configuration problems.

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath, pathToFileURL } from 'node:url'

import { parseChangelogSections } from './changelog.mjs'

const moduleDir = dirname(fileURLToPath(import.meta.url))
const repoRoot = resolve(moduleDir, '..', '..')

function readPackageVersion() {
  const pkgPath = resolve(repoRoot, 'package.json')
  const pkg = JSON.parse(readFileSync(pkgPath, 'utf8'))
  if (!pkg.version) {
    throw new Error(`No version field in ${pkgPath}`)
  }
  return pkg.version
}

function readLatestChangelogVersion() {
  const changelogPath = resolve(repoRoot, 'CHANGELOG.md')
  const sections = parseChangelogSections(readFileSync(changelogPath, 'utf8'))
  if (sections.length === 0) {
    throw new Error(`No release entries found in ${changelogPath}`)
  }
  return sections[0].version
}

function parseNumericVersion(version) {
  const match = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.exec(version)
  if (!match) {
    return undefined
  }
  return match.slice(1).map(part => Number.parseInt(part, 10))
}

export function evaluateVersionAlignment({
  packageVersion,
  changelogVersion,
  allowDerivedPre1Patch = false,
}) {
  if (packageVersion === changelogVersion) {
    return { ok: true, mode: 'exact' }
  }

  const packageParts = parseNumericVersion(packageVersion)
  const changelogParts = parseNumericVersion(changelogVersion)
  if (
    allowDerivedPre1Patch &&
    packageParts &&
    changelogParts &&
    packageParts[0] === 0 &&
    changelogParts[0] === 0 &&
    packageParts[1] === changelogParts[1] &&
    packageParts[2] > changelogParts[2]
  ) {
    return { ok: true, mode: 'derived-pre-1-patch' }
  }

  return { ok: false, mode: 'drift' }
}

function run(argv = process.argv.slice(2)) {
  const packageVersion = readPackageVersion()
  const changelogVersion = readLatestChangelogVersion()
  const alignment = evaluateVersionAlignment({
    packageVersion,
    changelogVersion,
    allowDerivedPre1Patch: argv.includes('--allow-derived-pre-1-patch'),
  })

  if (!alignment.ok) {
    process.stderr.write(
      [
        `Version drift detected:`,
        `  package.json version: ${packageVersion}`,
        `  CHANGELOG.md top entry: ${changelogVersion}`,
        ``,
        `These must match before tagging a release, otherwise the Website`,
        `build will surface the wrong version and the documented version`,
        `bump in CHANGELOG.md will not propagate to consumers.`,
        ``,
        `To fix, either:`,
        `  - Move the [Unreleased] entry under a ## [${packageVersion}] - YYYY-MM-DD`,
        `    header that matches package.json, or`,
        `  - Run \`node scripts/sync-version.mjs --version=${changelogVersion}\``,
        `    to bump every manifest to the version named in the changelog.`,
        ``,
      ].join('\n'),
    )
    process.exit(1)
  }

  if (alignment.mode === 'derived-pre-1-patch') {
    process.stdout.write(
      `Numeric pre-1.0 patch accepted: package.json ${packageVersion} derives from authored CHANGELOG.md line ${changelogVersion}.\n`,
    )
    return
  }

  process.stdout.write(
    `Version aligned: package.json and CHANGELOG.md both at ${packageVersion}.\n`,
  )
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    run()
  } catch (error) {
    process.stderr.write(`${error.message}\n`)
    process.exit(2)
  }
}
