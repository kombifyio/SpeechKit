// Verify that the root package.json version matches the most recent
// non-Unreleased entry in CHANGELOG.md. Without this guard it is easy
// to ship a release where CHANGELOG.md says [0.32.2] but package.json
// still says 0.32.1 — which is exactly how the Website ended up stuck
// on a stale version after v0.32.2 tagged.
//
// Usage:
//   node scripts/release/lint-version-sync.mjs
//
// Exits 0 if the versions are aligned, 1 if they drift, 2 on
// configuration problems.

import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

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

try {
  const packageVersion = readPackageVersion()
  const changelogVersion = readLatestChangelogVersion()

  if (packageVersion !== changelogVersion) {
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

  process.stdout.write(
    `Version aligned: package.json and CHANGELOG.md both at ${packageVersion}.\n`,
  )
} catch (error) {
  process.stderr.write(`${error.message}\n`)
  process.exit(2)
}
