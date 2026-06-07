// Lint a single CHANGELOG.md entry against the public style rules
// codified in docs/changelog-style.md.
//
// Usage:
//   node scripts/release/lint-changelog.mjs --version vX.Y.Z [--input CHANGELOG.md]
//   node scripts/release/lint-changelog.mjs --unreleased [--input CHANGELOG.md]
//
// The linter is intentionally conservative — every rule fires on
// substrings that almost never appear in legitimate public copy. False
// positives are easier to fix than a leak that lands on the public
// release page.

import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import {
  parseChangelogSections,
  isMinorBaselineVersion,
  getHighlightNotesForVersion,
  findMarketingNoteProblems,
} from './changelog.mjs'

// Forbidden patterns. Each entry is {pattern, label, hint}. `pattern`
// is a RegExp tested against the entry body; `label` is the short
// category shown in the error message; `hint` tells the maintainer
// what to write instead.
//
// Keep this list in sync with docs/changelog-style.md.
const FORBIDDEN_PATTERNS = [
  {
    pattern: /\bSK-\d+(?:\.\d+){0,3}\b/g,
    label: 'internal tracker ID',
    hint: 'Drop the issue ID. The changelog is read by users, not maintainers.',
  },
  {
    pattern: /\bBeads\b/gi,
    label: 'internal tracker name',
    hint: 'Refer to the user-visible change directly instead of naming the tracker.',
  },
  {
    pattern: /\baudit Phase [A-Z](?:\d+)?\b/gi,
    label: 'internal audit phase reference',
    hint: 'Describe the user impact; the internal phase label belongs in PR descriptions.',
  },
  {
    pattern: /\bpost-audit (?:improvement )?plan\b/gi,
    label: 'internal planning vocabulary',
    hint: 'Say what changed for the user, not which internal plan drove it.',
  },
  {
    pattern: /\b(?:happy-booping-dewdrop|bubbly-toucan|memoized-emerson|elegant-squirrel)[A-Za-z0-9_-]*\b/g,
    label: 'internal plan filename',
    hint: 'Internal plan filenames must never appear in the public changelog.',
  },
  {
    pattern: /\b(?:mako1|kombination|kombify-Core|kombify-Administration|KombiverseLabs|Soulcreek)\b/g,
    label: 'private host or account name',
    hint: 'These names identify private accounts or repos and must never reach the public release notes.',
  },
  {
    pattern: /\b[Cc]:\\Users\\[^\s`]+/g,
    label: 'absolute Windows developer path',
    hint: 'Strip developer-machine paths; they leak the maintainer username.',
  },
  {
    pattern: /\b(?:cmd|internal|pkg)\/[a-zA-Z0-9_/-]+(?:\/[a-zA-Z0-9_-]+)*\.go\b/g,
    label: 'internal Go source path',
    hint: 'Describe the behaviour change. Source paths belong in commit messages.',
    // pkg/speechkit/... is the public surface — allow references to the package, but not to
    // specific files inside it. The pattern above already requires a .go suffix, so a bare
    // "pkg/speechkit/**" mention in prose is fine.
  },
  {
    pattern: /\binternal\/[a-zA-Z0-9_/-]+\b/g,
    label: 'internal package path',
    hint: 'The user cannot import internal/ packages. Describe the user-visible effect instead.',
  },
  {
    pattern: /\bcmd\/[a-zA-Z0-9_/-]+\b/g,
    label: 'internal command package path',
    hint: 'Refer to the binary or feature by its user-visible name, not its source directory.',
  },
  {
    pattern: /\*appState\b/g,
    label: 'internal Go type reference',
    hint: 'Describe the behaviour without naming internal types.',
  },
  {
    pattern: /\b(?:runDesktopApp|startupTracker|recoverHook|desktopQuickNoteService|quickActionCoordinator|wailsQuickNoteHost|handleSecondDesktopInstance|tryAcquireSingleton)\b/g,
    label: 'internal Go symbol name',
    hint: 'Use a user-facing description of the feature instead of the function/type name.',
  },
  {
    pattern: /\bLocal\\com\.kombify\.speechkit\.[A-Za-z0-9._-]+/g,
    label: 'private mutex or kernel-object name',
    hint: 'Internal singleton/mutex identifiers leak implementation detail; just describe the guarantee.',
  },
  {
    pattern: /`?desktop\.(?:startup|window|tray|hook|singleton)(?:\.[a-z_]+)*`?/g,
    label: 'internal slog event name',
    hint: 'The log event schema can change; describe what is logged, not the field names.',
  },
  {
    pattern: /\bdoppler\b/gi,
    label: 'private secrets-tooling reference',
    hint: 'Doppler is internal-only. The public changelog must never mention it.',
  },
  {
    pattern: /\b(?:CLAUDE|AGENTS)\.md\b/g,
    label: 'internal maintainer-docs filename',
    hint: 'Maintainer-instruction files are not part of the public surface.',
  },
]

function readOption(argumentsList, name, fallback = '') {
  const prefix = `${name}=`
  const directMatch = argumentsList.find(argument => argument.startsWith(prefix))
  if (directMatch) {
    return directMatch.slice(prefix.length)
  }
  const index = argumentsList.indexOf(name)
  if (index >= 0 && argumentsList[index + 1]) {
    return argumentsList[index + 1]
  }
  return fallback
}

function normalizeVersion(version) {
  return version.startsWith('v') ? version.slice(1) : version
}

// Extract the body of the section we are linting. For `--version`, we
// match `## [X.Y.Z]`. For `--unreleased`, we match `## [Unreleased]`
// and stop at the first version section.
function extractTargetBody(markdown, { version, unreleased }) {
  if (unreleased) {
    const headerMatch = /^## \[Unreleased\]\s*$/m.exec(markdown)
    if (!headerMatch) {
      throw new Error(
        'CHANGELOG.md has no `## [Unreleased]` section. Add one before opening a release PR.',
      )
    }
    const start = headerMatch.index + headerMatch[0].length
    const rest = markdown.slice(start)
    const nextHeader = /^## \[/m.exec(rest)
    return rest.slice(0, nextHeader ? nextHeader.index : rest.length).trim()
  }

  if (!version) {
    throw new Error('Either --version vX.Y.Z or --unreleased must be provided.')
  }

  const normalized = normalizeVersion(version)
  const sections = parseChangelogSections(markdown)
  const section = sections.find(candidate => candidate.version === normalized)
  if (!section) {
    throw new Error(`Release ${version} was not found in CHANGELOG.md.`)
  }
  return section.body
}

// Build the list of violations for a given body. Returns an array of
// {pattern, label, hint, line, snippet} so the caller can format them.
export function findChangelogViolations(body) {
  const lines = body.split('\n')
  const violations = []
  for (const rule of FORBIDDEN_PATTERNS) {
    rule.pattern.lastIndex = 0
    let match
    while ((match = rule.pattern.exec(body)) !== null) {
      const matchedText = match[0]
      const offset = match.index
      // Map the offset back to a line + snippet.
      let cursor = 0
      let lineNumber = 0
      for (let i = 0; i < lines.length; i += 1) {
        const nextCursor = cursor + lines[i].length + 1
        if (offset < nextCursor) {
          lineNumber = i + 1
          break
        }
        cursor = nextCursor
      }
      violations.push({
        label: rule.label,
        hint: rule.hint,
        match: matchedText,
        line: lineNumber,
        snippet: lines[lineNumber - 1].trim(),
      })
      // RegExp without /g would loop forever; we have /g everywhere.
      if (!rule.pattern.global) break
    }
  }
  return violations
}

function formatViolations(violations, header) {
  const out = [`Changelog style violations in ${header}:`]
  for (const violation of violations) {
    out.push('')
    out.push(`  line ${violation.line}: ${violation.label}`)
    out.push(`    matched: ${JSON.stringify(violation.match)}`)
    out.push(`    snippet: ${violation.snippet}`)
    out.push(`    hint:    ${violation.hint}`)
  }
  out.push('')
  out.push('See docs/changelog-style.md for the full style guide.')
  return out.join('\n')
}

// CLI entry-point. Skips execution when imported as a module (Node 22+
// import.meta.main is not universally available, so we check argv[1]).
const isMain = process.argv[1] && resolve(process.argv[1]).endsWith('lint-changelog.mjs')

if (isMain) {
  const args = process.argv.slice(2)
  const inputPath = resolve(readOption(args, '--input', 'CHANGELOG.md'))
  const version = readOption(args, '--version')
  const unreleased = args.includes('--unreleased')

  let markdown
  try {
    markdown = readFileSync(inputPath, 'utf8')
  } catch (error) {
    process.stderr.write(`Failed to read ${inputPath}: ${error.message}\n`)
    process.exit(2)
  }

  let body
  let header
  try {
    body = extractTargetBody(markdown, { version, unreleased })
    header = unreleased ? '[Unreleased]' : `[${normalizeVersion(version)}]`
  } catch (error) {
    process.stderr.write(`${error.message}\n`)
    process.exit(2)
  }

  const violations = findChangelogViolations(body)
  if (violations.length > 0) {
    process.stderr.write(`${formatViolations(violations, header)}\n`)
    process.exit(1)
  }

  // Minor-baseline releases (X.Y.0) feed the website's "What is new in vX.Y"
  // marketing cards, so they must ship a curated `### Highlights` section.
  // Enforce it here (release-time) in addition to the website build guard.
  if (version && isMinorBaselineVersion(normalizeVersion(version))) {
    const notes = getHighlightNotesForVersion(markdown, version)
    const problems =
      notes.length === 0
        ? ['no `### Highlights` section — minor baseline releases must list 2-4 short marketing bullets']
        : findMarketingNoteProblems(notes, { version: normalizeVersion(version) })
    if (problems.length > 0) {
      process.stderr.write(
        `Website release highlights for ${header} are not marketing-ready:\n  - ${problems.join('\n  - ')}\n` +
          'Fix the `### Highlights` section in CHANGELOG.md (see docs/changelog-style.md).\n',
      )
      process.exit(1)
    }
  }

  process.stdout.write(`CHANGELOG.md ${header}: clean (${body.split('\n').length} lines scanned).\n`)
}
