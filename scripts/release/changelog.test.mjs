import test from 'node:test'
import assert from 'node:assert/strict'

import {
  extractLatestReleaseNotes,
  parseChangelogSections,
  renderReleaseNotes,
} from './changelog.mjs'

const sampleChangelog = `# Changelog

## [Unreleased]

## [0.18.0] - 2026-04-14

### Highlights

- **Local onboarding**: Continue while downloads run in the background.
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
