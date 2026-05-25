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

function toReleaseNote(rawLine, index) {
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

  return {
    title: `Update ${index + 1}`,
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

// Like `extractLatestReleaseNotes` but keeps the website anchored to the
// current minor release line. Publishing a normal 0.30.1 patch does not replace
// the highlight reel for v0.30, but an explicit `### Highlights` section in a
// later rollup entry (for example 0.38.7) is allowed to become the public
// "What is new in v0.38" source.
export function extractLatestMinorReleaseNotes(markdown, options = {}) {
  const { limit = 3, fallbackVersion = '0.0.0' } = options
  const sections = parseChangelogSections(markdown)
  const firstVersionedSection = sections.find(section => minorKey(section.version))
  const currentMinorKey = firstVersionedSection ? minorKey(firstVersionedSection.version) : ''
  const currentMinorSections = currentMinorKey
    ? sections.filter(section => minorKey(section.version) === currentMinorKey)
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
