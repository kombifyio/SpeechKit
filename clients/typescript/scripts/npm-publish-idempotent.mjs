import { createHash } from 'node:crypto'
import { execFile } from 'node:child_process'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { resolve } from 'node:path'
import { promisify } from 'node:util'
import { pathToFileURL } from 'node:url'

const execFileAsync = promisify(execFile)

export const sha512Integrity = (contents) =>
  `sha512-${createHash('sha512').update(contents).digest('base64')}`

export const requireExactPublishedTarball = ({
  spec,
  candidateIntegrity,
  publishedIntegrity,
  publishedTarball,
  publishedTarballIntegrity,
}) => {
  if (!publishedIntegrity) {
    throw new Error(`${spec} exists but the registry did not return dist.integrity.`)
  }
  if (publishedIntegrity !== candidateIntegrity) {
    throw new Error(
      `${spec} already exists with different content: registry dist.integrity ${publishedIntegrity} does not match candidate ${candidateIntegrity}.`,
    )
  }
  if (!publishedTarball) {
    throw new Error(`${spec} exists but the registry did not return dist.tarball.`)
  }
  if (publishedTarballIntegrity !== candidateIntegrity) {
    throw new Error(
      `${spec} registry metadata matches the candidate, but the downloaded tarball digest ${publishedTarballIntegrity} does not match ${candidateIntegrity}.`,
    )
  }
}

export const reconcilePublishedPackage = async ({ candidate, readPublished, downloadPublished, publish, requireProvenance = false }) => {
  const verifyPublished = async (published) => {
    if (requireProvenance && !published.attestations?.provenance?.predicateType) {
      throw new Error(`${candidate.spec} has no registry provenance attestation; matching tarball bytes are insufficient.`)
    }
    const downloaded = await downloadPublished(candidate.spec, published.tarball)
    requireExactPublishedTarball({
      spec: candidate.spec,
      candidateIntegrity: candidate.integrity,
      publishedIntegrity: published.integrity,
      publishedTarball: published.tarball,
      publishedTarballIntegrity: downloaded.integrity,
    })
  }

  const existing = await readPublished(candidate.spec)
  if (existing) {
    await verifyPublished(existing)
    return { result: 'already-matched', integrity: candidate.integrity }
  }

  try {
    await publish(candidate)
  } catch (publishError) {
    const raced = await readPublished(candidate.spec)
    if (!raced) throw publishError
    await verifyPublished(raced)
    return { result: 'already-matched-after-race', integrity: candidate.integrity }
  }

  const published = await readPublished(candidate.spec)
  if (!published) {
    throw new Error(`${candidate.spec} was accepted by npm publish but is not visible in the registry.`)
  }
  await verifyPublished(published)
  return { result: 'published', integrity: candidate.integrity }
}

const parseArgs = (argv) => {
  const values = new Map()
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index]
    if (!argument.startsWith('--')) throw new Error(`Unexpected argument: ${argument}`)
    const key = argument.slice(2)
    const value = argv[index + 1]
    if (!value || value.startsWith('--')) throw new Error(`Missing value for --${key}`)
    values.set(key, value)
    index += 1
  }
  for (const required of ['package-dir', 'registry', 'tag']) {
    if (!values.has(required)) throw new Error(`Missing required --${required} argument.`)
  }
  return {
    packageDir: resolve(values.get('package-dir')),
    registry: values.get('registry'),
    tag: values.get('tag'),
  }
}

const runNpm = async (args, options = {}) => {
  try {
    return await execFileAsync('npm', args, {
      cwd: options.cwd,
      env: process.env,
      maxBuffer: 10 * 1024 * 1024,
    })
  } catch (error) {
    const detail = [error.stderr, error.stdout].filter(Boolean).join('\n').trim()
    const wrapped = new Error(`npm ${args[0]} failed${detail ? `:\n${detail}` : '.'}`)
    wrapped.cause = error
    wrapped.stderr = error.stderr
    wrapped.stdout = error.stdout
    throw wrapped
  }
}

const parseSinglePack = async ({ stdout, directory, expectedSpec }) => {
  const entries = JSON.parse(stdout)
  if (!Array.isArray(entries) || entries.length !== 1) {
    throw new Error(`npm pack for ${expectedSpec ?? 'candidate'} returned an unexpected result.`)
  }
  const [entry] = entries
  const path = resolve(directory, entry.filename)
  const integrity = sha512Integrity(await readFile(path))
  if (entry.integrity !== integrity) {
    throw new Error(
      `npm pack reported ${entry.integrity} for ${entry.name}@${entry.version}, but the tarball digest is ${integrity}.`,
    )
  }
  const spec = `${entry.name}@${entry.version}`
  if (expectedSpec && spec !== expectedSpec) {
    throw new Error(`npm pack returned ${spec}; expected ${expectedSpec}.`)
  }
  return { spec, path, integrity }
}

const isNotFound = (error) => /(?:E404|404 Not Found)/u.test(`${error.stderr ?? ''}\n${error.message}`)

const main = async () => {
  const { packageDir, registry, tag } = parseArgs(process.argv.slice(2))
  const candidateDirectory = await mkdtemp(resolve(tmpdir(), 'speechkit-npm-candidate-'))
  const publishedDirectory = await mkdtemp(resolve(tmpdir(), 'speechkit-npm-published-'))
  const registryArgument = `--registry=${registry}`

  try {
    const candidatePack = await runNpm([
      'pack',
      packageDir,
      '--json',
      '--pack-destination',
      candidateDirectory,
    ])
    const candidate = await parseSinglePack({ stdout: candidatePack.stdout, directory: candidateDirectory })

    const readPublished = async (spec) => {
      try {
        const { stdout } = await runNpm(['view', spec, 'dist', '--json', registryArgument])
        const dist = JSON.parse(stdout)
        if (!dist || typeof dist !== 'object' || Array.isArray(dist)) {
          throw new Error(`${spec} exists but npm view returned invalid dist metadata.`)
        }
        return dist
      } catch (error) {
        if (isNotFound(error)) return null
        throw error
      }
    }

    const downloadPublished = async (spec, tarball) => {
      const packed = await runNpm([
        'pack',
        tarball,
        '--json',
        '--pack-destination',
        publishedDirectory,
        registryArgument,
      ])
      return parseSinglePack({ stdout: packed.stdout, directory: publishedDirectory, expectedSpec: spec })
    }

    const result = await reconcilePublishedPackage({
      candidate,
      readPublished,
      downloadPublished,
      requireProvenance: process.env.NPM_CONFIG_PROVENANCE === 'true',
      publish: async ({ path }) => {
        await runNpm(['publish', path, '--tag', tag, registryArgument])
      },
    })

    console.log(`${candidate.spec}: ${result.result}; exact tarball integrity verified.`)
  } finally {
    await Promise.all([
      rm(candidateDirectory, { recursive: true, force: true }),
      rm(publishedDirectory, { recursive: true, force: true }),
    ])
  }
}

if (import.meta.url === pathToFileURL(process.argv[1]).href) {
  main().catch((error) => {
    console.error(`::error title=Package publication integrity mismatch::${error.message}`)
    process.exitCode = 1
  })
}
