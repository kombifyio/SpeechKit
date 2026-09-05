import { execFileSync } from 'node:child_process'
import { readFile } from 'node:fs/promises'
import { fileURLToPath, pathToFileURL } from 'node:url'

export const publicPackages = ['client', 'voiceagent-client', 'voice-ui']

// Provider-control boundary: a token cannot make a private or mismatched
// build origin eligible for public npm provenance.
export function publicationBlockers({ repository, visibility, manifests, tokenConfigured }) {
  const blockers = []
  if (visibility !== 'public') blockers.push('The workflow build repository must be public for npm provenance.')
  for (const manifest of manifests) {
    if (manifest.repository?.url !== `git+https://github.com/${repository}.git`) {
      blockers.push(`${manifest.name}: package repository does not match the workflow build repository.`)
    }
    if (manifest.private || manifest.publishConfig?.access !== 'public' || manifest.publishConfig?.provenance !== true) {
      blockers.push(`${manifest.name}: public access and provenance must remain enabled.`)
    }
  }
  if (!tokenConfigured) blockers.push('NPM_PUBLISH_TOKEN is unavailable to this job; metadata presence elsewhere is insufficient.')
  return blockers
}

async function main() {
  const manifests = await Promise.all(publicPackages.map(async (name) =>
    JSON.parse(await readFile(new URL(`../packages/${name}/package.json`, import.meta.url), 'utf8')),
  ))
  const tokenConfigured = process.env.NPM_TOKEN_CONFIGURED === 'true'
  const blockers = publicationBlockers({
    repository: process.env.GITHUB_REPOSITORY,
    visibility: process.env.PUBLISH_REPOSITORY_VISIBILITY,
    manifests,
    tokenConfigured,
  })
  const packages = []
  for (const manifest of manifests) {
    const response = await fetch(`https://registry.npmjs.org/${encodeURIComponent(manifest.name)}`, {
      signal: AbortSignal.timeout(15_000),
      redirect: 'error',
    })
    packages.push({ name: manifest.name, anonymous_registry_status: response.status })
    await response.body?.cancel()
    if (![200, 404].includes(response.status)) {
      blockers.push(`${manifest.name}: anonymous registry inspection failed with HTTP ${response.status}.`)
    }
  }

  let authentication = 'not-checked'
  if (tokenConfigured) {
    if (!process.env.npm_execpath) throw new Error('Run this operation through npm run release:readiness.')
    try {
      // Capture both streams: do not log the npm identity, token, or npm's
      // configuration diagnostics. whoami performs no registry mutation.
      execFileSync(process.execPath, [
        process.env.npm_execpath, 'whoami', '--registry=https://registry.npmjs.org',
      ], { stdio: 'pipe', timeout: 30_000 })
      authentication = 'authenticated'
    } catch {
      authentication = 'failed'
      blockers.push('Read-only npm authentication failed; the account owner must inspect token validity and policy.')
    }
  }
  console.log(JSON.stringify({
    packages,
    authentication,
    blockers,
    publication_authorization: 'unverified: authentication does not prove package creation/write permission or 2FA policy',
    published_by_this_operation: false,
  }, null, 2))
  if (blockers.length) process.exitCode = 1
}

if (process.argv[1] && fileURLToPath(import.meta.url) === fileURLToPath(pathToFileURL(process.argv[1]))) {
  main().catch((error) => {
    console.error(`Public package readiness could not complete: ${error.message}. No publication was attempted.`)
    process.exitCode = 1
  })
}
