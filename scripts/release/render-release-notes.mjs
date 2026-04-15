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

const markdown = readFileSync(inputPath, 'utf8')
const notes = renderReleaseNotes({ markdown, version, repoUrl })

if (outputPath) {
  writeFileSync(resolve(outputPath), `${notes}\n`, 'utf8')
} else {
  process.stdout.write(notes)
}
