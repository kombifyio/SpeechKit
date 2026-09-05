import { execFileSync } from 'node:child_process'
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { publicPackages } from './npm-public-readiness.mjs'

const args = new Map(process.argv.slice(2).map((arg) => {
  const match = /^--(packages|version|published)=(.+)$/.exec(arg)
  if (!match) throw new Error(`Unsupported argument: ${arg}`)
  return [match[1], match[2]]
}))
const selected = (args.get('packages') ?? publicPackages.join(',')).split(',')
if (!selected.length || selected.some((name) => !publicPackages.includes(name))) {
  throw new Error('Select only client, voiceagent-client, or voice-ui.')
}
if (selected.includes('voice-ui') && !selected.includes('voiceagent-client')) {
  throw new Error('The Voice UI adapter smoke requires voiceagent-client alongside voice-ui.')
}
const published = args.get('published') === 'true'
if (args.has('published') && !['true', 'false'].includes(args.get('published'))) {
  throw new Error('--published must be true or false.')
}
const version = args.get('version')
if (published && !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version ?? '')) {
  throw new Error('Registry smoke requires --version=<exact Delivery version>.')
}
if (!process.env.npm_execpath) throw new Error('Run through npm run pack:smoke.')

const root = await mkdtemp(join(tmpdir(), 'speechkit-consumer-'))
try {
  // npm must not inherit setup-node's authenticated config, the caller's
  // .npmrc, workspace links, or credentials during an anonymous install.
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) =>
    /^(path|pathext|systemroot|windir|comspec|tmp|temp|lang|lc_all)$/i.test(key),
  ))
  Object.assign(env, {
    HOME: root,
    USERPROFILE: root,
    NPM_CONFIG_USERCONFIG: join(root, 'user.npmrc'),
    NPM_CONFIG_GLOBALCONFIG: join(root, 'global.npmrc'),
    NPM_CONFIG_CACHE: join(root, 'cache'),
    NPM_CONFIG_REGISTRY: 'https://registry.npmjs.org',
  })
  await writeFile(env.NPM_CONFIG_USERCONFIG, '')
  await writeFile(env.NPM_CONFIG_GLOBALCONFIG, '')
  await writeFile(join(root, 'package.json'), '{"private":true,"type":"module"}\n')
  const npm = (argv) => execFileSync(process.execPath, [process.env.npm_execpath, ...argv], {
    cwd: root, env, encoding: 'utf8', timeout: 120_000, maxBuffer: 10 * 1024 * 1024,
  })
  const specs = []
  const expected = []
  let expectedVersion = version
  for (const name of selected) {
    const dir = fileURLToPath(new URL(`../packages/${name}/`, import.meta.url))
    const manifest = JSON.parse(await readFile(join(dir, 'package.json'), 'utf8'))
    expectedVersion ??= manifest.version
    const want = expectedVersion
    expected.push({ name: manifest.name, version: want })
    if (published) {
      specs.push(`${manifest.name}@${want}`)
    } else {
      if (manifest.version !== want) throw new Error(`${manifest.name} has not been stamped to ${want}.`)
      const [pack] = JSON.parse(npm(['pack', dir, '--ignore-scripts', '--json', '--pack-destination', root]))
      specs.push(join(root, pack.filename))
    }
  }
  // Exercise the optional Node adapter as a real standalone consumer.
  if (selected.includes('voiceagent-client')) specs.push('ws@8.20.0')
  npm(['install', '--ignore-scripts', '--no-audit', '--no-fund', '--save-exact', ...specs])
  for (const want of expected) {
    const installed = JSON.parse(await readFile(join(root, 'node_modules', ...want.name.split('/'), 'package.json'), 'utf8'))
    if (installed.name !== want.name || installed.version !== want.version) {
      throw new Error(`Installed package does not match ${want.name}@${want.version}.`)
    }
    if (published) {
      const attestations = JSON.parse(npm(['view', `${want.name}@${want.version}`, 'dist.attestations', '--json']) || 'null')
      if (!attestations?.provenance?.predicateType) {
        throw new Error(`${want.name}@${want.version} has no npm provenance attestation.`)
      }
    }
  }
  // Presence alone is not verification. npm checks registry signatures and
  // available provenance attestations for the freshly installed dependency tree.
  if (published) npm(['audit', 'signatures'])
  const imports = []
  if (selected.includes('client')) imports.push(['@kombifyio/speechkit-client', 'SpeechKitClient'])
  if (selected.includes('voiceagent-client')) imports.push(
    ['@kombifyio/speechkit-voiceagent-client', 'VoiceAgentSession'],
    ['@kombifyio/speechkit-voiceagent-client/browser', 'openBrowserSession'],
    ['@kombifyio/speechkit-voiceagent-client/node', 'openNodeSession'],
  )
  if (selected.includes('voice-ui')) imports.push(
    ['@kombifyio/speechkit-voice-ui/voiceagent-adapter', 'createVoiceAgentUiController'],
    ['@kombifyio/speechkit-voice-ui/marks', 'resolveMarkSrc'],
  )
  execFileSync(process.execPath, ['--input-type=module', '--eval', `
    for (const [specifier, entry] of ${JSON.stringify(imports)}) {
      const api = await import(specifier);
      if (typeof api[entry] !== 'function') throw new Error(specifier + ' is not consumable');
    }
  `], { cwd: root, env, stdio: 'inherit', timeout: 30_000 })
  console.log(`${published ? 'Anonymous registry' : 'Packed'} consumer imports passed: ${expected.map((pkg) => `${pkg.name}@${pkg.version}`).join(', ')}`)
} finally {
  await rm(root, { recursive: true, force: true })
}
