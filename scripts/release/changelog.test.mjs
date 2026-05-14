import test from 'node:test'
import assert from 'node:assert/strict'

import {
  extractLatestMinorReleaseNotes,
  extractLatestReleaseNotes,
  isMinorBaselineVersion,
  parseChangelogSections,
  renderReleaseNotes,
  toMinorVersionLabel,
} from './changelog.mjs'

const sampleChangelog = `# Changelog

## [Unreleased]

## [0.18.0] - 2026-04-14

### Highlights

- **Local onboarding**: Continue while downloads run in the
  background.
- **Recommended local model**: Whisper Large v3 Turbo is now the default recommendation.
- **Release surface automation**: Website surfaces now derive data directly from the changelog.

### Added

- **StreamPlayer**: Buffered streaming audio output for voice agent playback.

### Fixed

- **Overlay centering**: Compact overlay anchor stays centered on scaled Windows displays.

## [0.17.0] - 2026-04-12

### Highlights

- **UI redesign**: Dashboard, Settings, and overlays were refreshed.

### Added

- **Marketing site**: Public site ships alongside the release.
`

test('parseChangelogSections ignores Unreleased and preserves order', () => {
  const sections = parseChangelogSections(sampleChangelog)

  assert.equal(sections.length, 2)
  assert.equal(sections[0].version, '0.18.0')
  assert.equal(sections[0].date, '2026-04-14')
  assert.match(sections[0].body, /### Highlights/)
  assert.equal(sections[1].version, '0.17.0')
})

test('renderReleaseNotes returns the selected release body plus compare link', () => {
  const notes = renderReleaseNotes({
    markdown: sampleChangelog,
    version: 'v0.18.0',
    repoUrl: 'https://github.com/kombifyio/SpeechKit',
  })

  assert.match(notes, /### Highlights/)
  assert.match(notes, /### Added/)
  assert.match(
    notes,
    /\*\*Full Changelog\*\*: https:\/\/github\.com\/kombifyio\/SpeechKit\/compare\/v0\.17\.0\.\.\.v0\.18\.0/,
  )
  assert.doesNotMatch(notes, /^## \[0\.18\.0\]/m)
})

test('renderReleaseNotes prefers an explicit compare URL', () => {
  const notes = renderReleaseNotes({
    markdown: sampleChangelog,
    version: 'v0.18.0',
    repoUrl: 'https://github.com/kombifyio/SpeechKit',
    compareUrl: 'https://github.com/kombifyio/SpeechKit/compare/v0.16.0...v0.18.0',
  })

  assert.match(
    notes,
    /\*\*Full Changelog\*\*: https:\/\/github\.com\/kombifyio\/SpeechKit\/compare\/v0\.16\.0\.\.\.v0\.18\.0/,
  )
  assert.doesNotMatch(notes, /compare\/v0\.17\.0\.\.\.v0\.18\.0/)
})

test('extractLatestReleaseNotes prefers Highlights bullets and limits output', () => {
  const latest = extractLatestReleaseNotes(sampleChangelog)

  assert.equal(latest.version, '0.18.0')
  assert.deepEqual(latest.notes, [
    {
      title: 'Local onboarding',
      body: 'Continue while downloads run in the background.',
    },
    {
      title: 'Recommended local model',
      body: 'Whisper Large v3 Turbo is now the default recommendation.',
    },
    {
      title: 'Release surface automation',
      body: 'Website surfaces now derive data directly from the changelog.',
    },
  ])
})

test('extractLatestReleaseNotes turns bold sentence bullets into titled notes', () => {
  const latest = extractLatestReleaseNotes(`# Changelog

## [0.19.0] - 2026-04-30

### Highlights

- **Agent-ready getting started.** The website gives coding agents copy-ready prompts.
- **Storage groundwork** Speech sessions can now link to audio assets.
`)

  assert.deepEqual(latest.notes, [
    {
      title: 'Agent-ready getting started',
      body: 'The website gives coding agents copy-ready prompts.',
    },
    {
      title: 'Storage groundwork',
      body: 'Speech sessions can now link to audio assets.',
    },
  ])
})

test('isMinorBaselineVersion only accepts X.Y.0 (with optional pre-release)', () => {
  assert.equal(isMinorBaselineVersion('0.30.0'), true)
  assert.equal(isMinorBaselineVersion('1.0.0'), true)
  assert.equal(isMinorBaselineVersion('0.30.0-rc1'), true)
  assert.equal(isMinorBaselineVersion('0.30.1'), false)
  assert.equal(isMinorBaselineVersion('0.30.10'), false)
  assert.equal(isMinorBaselineVersion('preview'), false)
})

test('toMinorVersionLabel reduces X.Y.Z to X.Y', () => {
  assert.equal(toMinorVersionLabel('0.30.1'), '0.30')
  assert.equal(toMinorVersionLabel('0.30.0'), '0.30')
  assert.equal(toMinorVersionLabel('1.2.3-rc1'), '1.2')
  assert.equal(toMinorVersionLabel('weird'), 'weird')
})

const patchOnTopChangelog = `# Changelog

## [Unreleased]

## [0.18.2] - 2026-04-20

### Highlights

- **Patch fix**: One small organizational thing.

## [0.18.1] - 2026-04-18

### Highlights

- **Another patch**: Just security stuff.

## [0.18.0] - 2026-04-14

### Highlights

- **Local onboarding**: Continue while downloads run in the
  background.
- **Recommended local model**: Whisper Large v3 Turbo is now the default recommendation.
- **Release surface automation**: Website surfaces now derive data directly from the changelog.
`

test('extractLatestMinorReleaseNotes skips patch releases and anchors on the latest X.Y.0 entry', () => {
  const latest = extractLatestMinorReleaseNotes(patchOnTopChangelog)

  assert.equal(latest.version, '0.18.0')
  assert.equal(latest.notes.length, 3)
  assert.equal(latest.notes[0].title, 'Local onboarding')
  assert.equal(latest.notes[2].title, 'Release surface automation')
})

test('extractLatestMinorReleaseNotes returns the same entry as extractLatestReleaseNotes when top entry is already a minor baseline', () => {
  const minorTop = extractLatestMinorReleaseNotes(sampleChangelog)
  const absoluteTop = extractLatestReleaseNotes(sampleChangelog)

  assert.equal(minorTop.version, absoluteTop.version)
  assert.deepEqual(minorTop.notes, absoluteTop.notes)
})

test('extractLatestMinorReleaseNotes falls back gracefully when no minor baseline exists', () => {
  const onlyPatchesChangelog = `# Changelog

## [0.0.1] - 2026-01-01

### Highlights

- **Bootstrap**: First commit.
`

  const result = extractLatestMinorReleaseNotes(onlyPatchesChangelog, { fallbackVersion: '0.0.0' })
  assert.equal(result.version, '0.0.1')
  assert.equal(result.notes.length, 1)
})
