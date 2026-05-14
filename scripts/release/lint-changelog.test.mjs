// Tests for the changelog linter. Run via `node --test scripts/release/lint-changelog.test.mjs`.

import { test } from 'node:test'
import assert from 'node:assert/strict'

import { findChangelogViolations } from './lint-changelog.mjs'

test('clean entry produces no violations', () => {
  const body = `
Windows desktop client stability patch. No public API change.

### Fixed
- Duplicate text injection when two SpeechKit instances were active
  at once.
- A crash in any Wails event-loop callback no longer takes down the
  desktop client.

### Added
- Structured startup, window, and tray telemetry in the local log
  file so runtime issues can be diagnosed from logs.
`.trim()

  const violations = findChangelogViolations(body)
  assert.equal(violations.length, 0, JSON.stringify(violations, null, 2))
})

test('Beads issue ID is flagged', () => {
  const violations = findChangelogViolations(
    '- Closes SK-004.5.10 by adding a singleton mutex.',
  )
  assert.ok(
    violations.some(violation => violation.match === 'SK-004.5.10'),
    'expected SK-004.5.10 to be flagged',
  )
})

test('multi-level Beads ID variants are flagged', () => {
  for (const id of ['SK-001', 'SK-004.6.5', 'SK-004.6.6']) {
    const violations = findChangelogViolations(`- Fixes ${id}.`)
    assert.ok(
      violations.some(violation => violation.match === id),
      `expected ${id} to be flagged`,
    )
  }
})

test('"Beads" tracker name is flagged', () => {
  const violations = findChangelogViolations(
    '- Recorded as a follow-up in Beads SK-004.6.5.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal tracker name'),
    'expected the Beads tracker name to be flagged',
  )
})

test('audit phase reference is flagged', () => {
  const violations = findChangelogViolations(
    '- Lands Audit Phase C2 PR 3 (`internal/quicknote`).',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal audit phase reference'),
  )
})

test('internal plan filename is flagged', () => {
  const violations = findChangelogViolations(
    '- Phase C item C3 in the post-audit improvement plan (`happy-booping-dewdrop.md`).',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal plan filename'),
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal planning vocabulary'),
  )
})

test('private host names are flagged', () => {
  for (const name of ['mako1', 'kombination', 'KombiverseLabs', 'Soulcreek']) {
    const violations = findChangelogViolations(`- Synced via ${name} machine.`)
    assert.ok(
      violations.some(violation => violation.match === name),
      `expected ${name} to be flagged`,
    )
  }
})

test('absolute Windows developer paths are flagged', () => {
  const violations = findChangelogViolations(
    '- Pulled plan from `C:\\Users\\maintainer\\.claude\\plans\\foo.md`.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'absolute Windows developer path'),
  )
})

test('internal Go source files are flagged', () => {
  const violations = findChangelogViolations(
    '- Updated `cmd/speechkit/desktop_windows.go` to wrap hooks.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal Go source path'),
    'expected the .go source path to be flagged',
  )
})

test('internal package directory is flagged', () => {
  const violations = findChangelogViolations(
    '- Moved files into `internal/quicknote/`.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal package path'),
  )
})

test('cmd directory references are flagged', () => {
  const violations = findChangelogViolations(
    '- Reorganised `cmd/speechkit-mcp/` into subpackages.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'internal command package path'),
  )
})

test('public pkg/speechkit prose references are allowed', () => {
  const violations = findChangelogViolations(
    '- No public API change. `pkg/speechkit/**` remains backward-compatible.',
  )
  // Should match no rule — pkg/ is a public surface and the .go-suffix
  // rule guards the more specific case.
  assert.equal(
    violations.filter(violation => violation.label.includes('path')).length,
    0,
    'public pkg/speechkit prose must not be flagged',
  )
})

test('internal Go symbol names are flagged', () => {
  for (const symbol of ['runDesktopApp', 'recoverHook', 'startupTracker', '*appState']) {
    const violations = findChangelogViolations(`- Added defer ${symbol} guard.`)
    assert.ok(
      violations.some(violation => violation.match.includes(symbol.replace(/^\*/, ''))),
      `expected ${symbol} to be flagged`,
    )
  }
})

test('private mutex name is flagged', () => {
  const violations = findChangelogViolations(
    '- Claim `Local\\com.kombify.speechkit.singleton-belt` before app start.',
  )
  assert.ok(
    violations.some(violation => violation.label === 'private mutex or kernel-object name'),
  )
})

test('internal slog event names are flagged', () => {
  for (const event of [
    'desktop.startup',
    'desktop.window.closing',
    'desktop.tray.dashboard_requested',
    'desktop.singleton.duplicate_instance',
    'desktop.hook.panic',
  ]) {
    const violations = findChangelogViolations(`- Emits \`${event}\` events.`)
    assert.ok(
      violations.some(violation => violation.label === 'internal slog event name'),
      `expected ${event} to be flagged`,
    )
  }
})

test('Doppler is flagged', () => {
  const violations = findChangelogViolations('- Reads secrets via Doppler CLI.')
  assert.ok(
    violations.some(violation => violation.label === 'private secrets-tooling reference'),
  )
})

test('maintainer-only doc names are flagged', () => {
  for (const name of ['CLAUDE.md', 'AGENTS.md']) {
    const violations = findChangelogViolations(`- See ${name} for guidance.`)
    assert.ok(
      violations.some(violation => violation.match === name),
    )
  }
})

test('public framework and OS terms are not flagged', () => {
  const body = `
- Belt-and-suspenders for Wails event-loop callbacks.
- Uses Windows named mutex with per-session scope.
- Go 1.26 toolchain compatibility verified.
- Voice agent reconnect after WebSocket disconnect now backs off.
`
  const violations = findChangelogViolations(body)
  assert.equal(violations.length, 0, JSON.stringify(violations, null, 2))
})

test('the historical v0.32.2 bad entry is rejected by the linter', () => {
  // This is a regression test: the exact bad entry the maintainer
  // shipped on 2026-05-13 must trip multiple rules so the linter
  // would have blocked it pre-tag.
  const body = `
v0.32.2 closes the runtime-stability findings from Beads SK-004.5.10:
the duplicate-text-injection symptom reported in live testing is
root-caused (Wails v3 alpha's \`SingleInstance\` activation fires inside
\`app.Run()\`, which is the last bootstrap stage — by then a racing
second instance has already registered global Windows hotkeys via
\`RegisterHotKey\`). The fix is belt-and-suspenders: claim a session-
scoped Windows named mutex in \`runDesktopApp\` before any global
resource is acquired.

### Fixed
- **Duplicate text injection eliminated.** A new
  \`Local\\com.kombify.speechkit.singleton-belt\` Windows named mutex.
- **Wails event-loop callback panics no longer crash the desktop
  client.** A \`defer recoverHook(name)\` is installed in every
  registered callback.

### Notes
- The 2026-05-13 audit Phase C2 PR 3 (\`internal/quicknote\`) was
  attempted in the same session but deferred. Recorded as a follow-up
  in Beads SK-004.6.5.
`
  const violations = findChangelogViolations(body)
  const labels = new Set(violations.map(violation => violation.label))
  assert.ok(labels.has('internal tracker ID'))
  assert.ok(labels.has('internal tracker name'))
  assert.ok(labels.has('internal audit phase reference'))
  assert.ok(labels.has('internal Go symbol name'))
  assert.ok(labels.has('private mutex or kernel-object name'))
  assert.ok(labels.has('internal package path'))
})
