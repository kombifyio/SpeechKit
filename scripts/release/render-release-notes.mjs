import { execFileSync } from 'node:child_process'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { renderReleaseNotes } from './changelog.mjs'

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

const args = process.argv.slice(2)
const version = readOption(args, '--version')
if (!version) {
  throw new Error('Missing required --version argument.')
}

const inputPath = resolve(readOption(args, '--input', 'CHANGELOG.md'))
const outputPath = readOption(args, '--output')
const repoUrl = readOption(args, '--repo-url', 'https://github.com/kombifyio/SpeechKit')
let compareUrl = readOption(args, '--compare-url')
const previousTag = readOption(args, '--previous-tag')
const remoteTagsUrl = readOption(args, '--remote-tags-url')

function normalizeTag(tag) {
  return tag.startsWith('v') ? tag : `v${tag}`
}

function versionParts(tag) {
  const match = tag.replace(/^v/, '').match(/^(\d+)\.(\d+)\.(\d+)(?:-(.+))?$/)
  if (!match) {
    return { core: [0, 0, 0], prerelease: [] }
  }
  return {
    core: match.slice(1, 4).map(part => Number.parseInt(part, 10)),
    prerelease: match[4] ? match[4].split(/[.-]/) : [],
  }
}

function comparePrereleasePart(left, right) {
  const leftNumber = /^\d+$/.test(left) ? Number.parseInt(left, 10) : undefined
  const rightNumber = /^\d+$/.test(right) ? Number.parseInt(right, 10) : undefined

  if (leftNumber !== undefined && rightNumber !== undefined) {
    return leftNumber - rightNumber
  }
  if (leftNumber !== undefined) {
    return -1
  }
  if (rightNumber !== undefined) {
    return 1
  }
  return left.localeCompare(right)
}

function compareTags(left, right) {
  const leftParts = versionParts(left)
  const rightParts = versionParts(right)
  for (let index = 0; index < 3; index += 1) {
    const diff = leftParts.core[index] - rightParts.core[index]
    if (diff !== 0) {
      return diff
    }
  }

  if (leftParts.prerelease.length === 0 && rightParts.prerelease.length > 0) {
    return 1
  }
  if (leftParts.prerelease.length > 0 && rightParts.prerelease.length === 0) {
    return -1
  }

  const length = Math.max(leftParts.prerelease.length, rightParts.prerelease.length)
  for (let index = 0; index < length; index += 1) {
    const leftPart = leftParts.prerelease[index]
    const rightPart = rightParts.prerelease[index]
    if (leftPart === undefined) {
      return -1
    }
    if (rightPart === undefined) {
      return 1
    }
    const diff = comparePrereleasePart(leftPart, rightPart)
    if (diff !== 0) {
      return diff
    }
  }

  return left.localeCompare(right)
}

function parseTags(output) {
  return output
    .split('\n')
    .map(line => line.trim())
    .filter(Boolean)
    .map(line => {
      const refMatch = line.match(/refs\/tags\/(v[^\s]+)$/)
      return refMatch ? refMatch[1] : line
    })
    .filter(tag => /^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(tag))
}

function resolvePreviousTag({ currentTag, remoteUrl }) {
  let output = ''
  try {
    output = remoteUrl
      ? execFileSync('git', ['ls-remote', '--tags', '--refs', remoteUrl, 'v*'], {
          encoding: 'utf8',
        })
      : execFileSync('git', ['tag', '-l', 'v*'], { encoding: 'utf8' })
  } catch {
    return undefined
  }

  return parseTags(output)
    .filter(tag => tag !== currentTag && compareTags(tag, currentTag) < 0)
    .sort(compareTags)
    .at(-1)
}

const currentTag = normalizeTag(version)
const resolvedPreviousTag = previousTag
  ? normalizeTag(previousTag)
  : resolvePreviousTag({ currentTag, remoteUrl: remoteTagsUrl })

if (!compareUrl && repoUrl && resolvedPreviousTag && resolvedPreviousTag !== currentTag) {
  compareUrl = `${repoUrl}/compare/${resolvedPreviousTag}...${currentTag}`
}

const markdown = readFileSync(inputPath, 'utf8')
const notes = renderReleaseNotes({ markdown, version, repoUrl, compareUrl })

if (outputPath) {
  writeFileSync(resolve(outputPath), `${notes}\n`, 'utf8')
} else {
  process.stdout.write(notes)
}
