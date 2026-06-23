function escapeRegExp(value) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function normalizeVersion(version) {
  return version.startsWith('v') ? version.slice(1) : version
}

function stripFullChangelogLink(body) {
  return body.replace(/\n?\*\*Full Changelog\*\*:.*$/m, '').trim()
}

function getSubsectionBody(body, heading) {
  const headingExpression = new RegExp(`^### ${escapeRegExp(heading)}\\s*$`, 'm')
  const headingMatch = headingExpression.exec(body)
  if (!headingMatch) {
    return ''
  }

  const afterHeading = body.slice(headingMatch.index + headingMatch[0].length).trimStart()
  const nextHeadingMatch = /^### /m.exec(afterHeading)
  if (!nextHeadingMatch) {
    return afterHeading.trim()
  }

  return afterHeading.slice(0, nextHeadingMatch.index).trim()
}

function extractBulletBlocks(body) {
  const bullets = []
  let current = ''

  for (const line of body.split('\n')) {
    const trimmed = line.trim()
    if (trimmed.startsWith('- ')) {
      if (current) {
        bullets.push(current)
      }
      current = trimmed
      continue
    }

    if (!current || !trimmed || trimmed.startsWith('### ')) {
      continue
    }

    current = `${current} ${trimmed}`
  }

  if (current) {
    bullets.push(current)
  }

  return bullets
}

function toReleaseNote(rawLine) {
  const raw = rawLine.slice(2).trim()
  const boldMatch = raw.match(/^\*\*(.+?)\*\*(?::|\.)?\s*(.+)$/)
  if (boldMatch) {
    return {
      title: boldMatch[1].trim().replace(/[.:]+$/, ''),
      body: boldMatch[2].trim(),
    }
  }

  const colonIndex = raw.indexOf(':')
  if (colonIndex > 0) {
    return {
      title: raw.slice(0, colonIndex).trim(),
      body: raw.slice(colonIndex + 1).trim(),
    }
  }

  // No explicit "**Title**:" or "Title:" form. Derive a short lead-in title
  // instead of an opaque "Update N" label so the surface degrades
  // gracefully. Curated `### Highlights` (required by the website build and
  // the release lint via validateMarketingNotes) is what should actually
  // feed the public marketing surface — this path is a safety net only.
  const words = raw.split(/\s+/)
  const lead = words.slice(0, 7).join(' ').replace(/[.,;:]+$/, '')
  return {
    title: words.length > 7 ? `${lead}…` : lead,
    body: raw,
  }
}

export function parseChangelogSections(markdown) {
  const headerExpression = /^## \[([^\]]+)\](?: - (.+))?$/gm
  const matches = [...markdown.matchAll(headerExpression)]
  const sections = []

  for (let index = 0; index < matches.length; index += 1) {
    const match = matches[index]
    const version = match[1]?.trim()
    if (!version || version === 'Unreleased') {
      continue
    }

    const sectionStart = (match.index ?? 0) + match[0].length
    const sectionEnd = matches[index + 1]?.index ?? markdown.length
    sections.push({
      version,
      date: match[2]?.trim() ?? '',
      body: markdown.slice(sectionStart, sectionEnd).trim(),
    })
  }

  return sections
}

export function extractLatestReleaseNotes(markdown, options = {}) {
  const { limit = 3, fallbackVersion = '0.0.0' } = options
  const sections = parseChangelogSections(markdown)
  if (sections.length === 0) {
    return { version: fallbackVersion, notes: [] }
  }

  const latest = sections[0]
  const highlightsBody = getSubsectionBody(latest.body, 'Highlights')
  const sourceBody = highlightsBody || latest.body
  const bulletLines = extractBulletBlocks(sourceBody).slice(0, limit)

  return {
    version: latest.version,
    notes: bulletLines.map(toReleaseNote),
  }
}

// A "minor baseline" version is one whose patch component is 0 — i.e.
// 0.30.0 or 1.0.0, but not 0.30.1. Pre-release suffixes (e.g. `-rc1`)
// are tolerated so a release-candidate of a new minor still counts.
export function isMinorBaselineVersion(version) {
  return /^\d+\.\d+\.0(?:[-+].*)?$/.test(version)
}

function minorKey(version) {
  const match = /^(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/.exec(version)
  if (!match) {
    return ''
  }
  return `${match[1]}.${match[2]}`
}

function compareVersionNumbers(a, b) {
  const parse = value => {
    const match = /^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$/.exec(String(value).trim())
    return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : [0, 0, 0]
  }
  const left = parse(a)
  const right = parse(b)
  for (let index = 0; index < left.length; index += 1) {
    if (left[index] !== right[index]) {
      return left[index] > right[index] ? 1 : -1
    }
  }
  return 0
}

// Like `extractLatestReleaseNotes` but keeps the website anchored to the
// current minor release line. Publishing a normal 0.30.1 patch does not replace
// the highlight reel for v0.30, but an explicit `### Highlights` section in a
// later rollup entry (for example 0.38.7) is allowed to become the public
// "What is new in v0.38" source.
export function extractLatestMinorReleaseNotes(markdown, options = {}) {
  const { limit = 3, fallbackVersion = '0.0.0', anchorVersion = '' } = options
  const sections = parseChangelogSections(markdown)
  const normalizedAnchor = anchorVersion ? normalizeVersion(anchorVersion) : ''
  const anchorMinorKey = normalizedAnchor ? minorKey(normalizedAnchor) : ''
  const firstVersionedSection = sections.find(section => minorKey(section.version))
  const currentMinorKey = anchorMinorKey || (firstVersionedSection ? minorKey(firstVersionedSection.version) : '')
  const currentMinorSections = currentMinorKey
    ? sections.filter(section => {
        if (minorKey(section.version) !== currentMinorKey) {
          return false
        }
        return !normalizedAnchor || compareVersionNumbers(section.version, normalizedAnchor) <= 0
      })
    : []
  const latestWithHighlights = currentMinorSections.find(
    section => getSubsectionBody(section.body, 'Highlights'),
  )
  const minorBaseline = currentMinorSections.find(section => isMinorBaselineVersion(section.version))
  const latest = latestWithHighlights || minorBaseline
  if (!latest) {
    // Fall back to the absolute latest entry if no minor baseline exists.
    // This keeps the helper safe during early development before the
    // first X.Y.0 lands in the changelog.
    return extractLatestReleaseNotes(markdown, { limit, fallbackVersion })
  }

  const highlightsBody = getSubsectionBody(latest.body, 'Highlights')
  const sourceBody = highlightsBody || latest.body
  const bulletLines = extractBulletBlocks(sourceBody).slice(0, limit)

  return {
    version: latest.version,
    notes: bulletLines.map(toReleaseNote),
  }
}

// Reduce an X.Y.Z[-...] version string to its "X.Y" minor label.
// Used by the website so the marketing surface tracks the release
// line, not the patch revision.
export function toMinorVersionLabel(version) {
  const match = /^(\d+)\.(\d+)/.exec(version)
  if (!match) {
    return version
  }
  return `${match[1]}.${match[2]}`
}

// Compare the "X.Y" minor release lines of two version strings (a leading
// "v" is tolerated). Returns 1 when `a` is a newer minor line than `b`,
// -1 when older, and 0 when they share the same minor line. Unparseable
// inputs collapse to 0.0 so an unknown value never reports as "ahead" of a
// real release. Used by the website release verification to catch the case
// where the marketing surface advertises a version whose download artifacts
// have not actually been published yet.
export function compareMinorLines(a, b) {
  const parse = value => {
    const match = /^v?(\d+)\.(\d+)/.exec(String(value).trim())
    return match ? [Number(match[1]), Number(match[2])] : [0, 0]
  }
  const [aMajor, aMinor] = parse(a)
  const [bMajor, bMinor] = parse(b)
  if (aMajor !== bMajor) {
    return aMajor > bMajor ? 1 : -1
  }
  if (aMinor !== bMinor) {
    return aMinor > bMinor ? 1 : -1
  }
  return 0
}

export function renderReleaseNotes({ markdown, version, repoUrl, compareUrl }) {
  const sections = parseChangelogSections(markdown)
  const normalizedVersion = normalizeVersion(version)
  const sectionIndex = sections.findIndex(section => section.version === normalizedVersion)

  if (sectionIndex === -1) {
    throw new Error(`Release ${version} was not found in CHANGELOG.md.`)
  }

  const section = sections[sectionIndex]
  const previousSection = sections[sectionIndex + 1]
  const notes = [stripFullChangelogLink(section.body)]

  if (compareUrl) {
    notes.push(`**Full Changelog**: ${compareUrl}`)
  } else if (repoUrl && previousSection) {
    notes.push(
      `**Full Changelog**: ${repoUrl}/compare/v${previousSection.version}...v${section.version}`,
    )
  }

  return notes.filter(Boolean).join('\n\n').trim()
}

// Marketing surfaces (the website "What is new in vX.Y" cards) must show
// short, curated, titled highlights — never an auto-degraded dump of
// technical changelog prose. These caps keep the cards scannable.
export const MAX_HIGHLIGHT_TITLE_LENGTH = 70
export const MAX_HIGHLIGHT_BODY_LENGTH = 240

// getHighlightNotesForVersion returns the parsed { title, body } notes from
// the `### Highlights` subsection of a specific release entry, or [] when the
// entry is missing or has no Highlights section.
export function getHighlightNotesForVersion(markdown, version) {
  const normalized = normalizeVersion(version)
  const section = parseChangelogSections(markdown).find(item => item.version === normalized)
  if (!section) {
    return []
  }
  const highlightsBody = getSubsectionBody(section.body, 'Highlights')
  if (!highlightsBody) {
    return []
  }
  return extractBulletBlocks(highlightsBody).map(toReleaseNote)
}

// findMarketingNoteProblems returns a list of human-readable problems with the
// notes a marketing surface is about to render. Empty array == website-ready.
export function findMarketingNoteProblems(notes, { version = '' } = {}) {
  const problems = []
  if (!Array.isArray(notes) || notes.length === 0) {
    problems.push('no highlight bullets were found — add a `### Highlights` section with 2-4 short bullets')
    return problems
  }
  notes.forEach((note, index) => {
    const title = (note?.title ?? '').trim()
    const body = (note?.body ?? '').trim()
    if (!title || /^Update \d+$/.test(title) || title.endsWith('…')) {
      problems.push(
        `note ${index + 1}: missing or auto-generated title ("${title}") — write the bullet as "- **Short Title**: benefit"`,
      )
    } else if (title.length > MAX_HIGHLIGHT_TITLE_LENGTH) {
      problems.push(`note ${index + 1}: title is ${title.length} chars (max ${MAX_HIGHLIGHT_TITLE_LENGTH}) — shorten the heading`)
    }
    if (body.length > MAX_HIGHLIGHT_BODY_LENGTH) {
      problems.push(
        `note ${index + 1}: body is ${body.length} chars (max ${MAX_HIGHLIGHT_BODY_LENGTH}) — keep website cards short; move detail into ### Added/Fixed/Changed`,
      )
    }
  })
  return problems
}

// validateMarketingNotes throws a descriptive error when the notes a marketing
// surface is about to render are not curated and short. Used by the website
// build and the release lint so a degraded "Update 1/2/3" reel fails loudly
// instead of shipping. Returns the notes unchanged when they pass.
export function validateMarketingNotes(notes, { version = '' } = {}) {
  const problems = findMarketingNoteProblems(notes, { version })
  if (problems.length > 0) {
    const scope = version ? ` for ${version}` : ''
    throw new Error(
      `Website release highlights${scope} are not marketing-ready:\n  - ${problems.join('\n  - ')}\n` +
        'Fix the `### Highlights` section in CHANGELOG.md (see docs/changelog-style.md).',
    )
  }
  return notes
}
