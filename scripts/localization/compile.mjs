#!/usr/bin/env node
// Native catalogs remain the source. Paraglide compiles a reproducible web projection.
import { mkdir, readFile, readdir, writeFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const surface = process.argv[2];
if (!['desktop', 'voice-ui'].includes(surface)) throw new Error('Expected desktop or voice-ui');
const desktop = surface === 'desktop';
const check = process.argv.includes('--check');
async function writeProjection(path, content) {
  if (!check) return writeFile(path, content);
  let current;
  try { current = await readFile(path, 'utf8'); } catch { current = ''; }
  if (current.replaceAll('\r\n', '\n') !== content) {
    throw new Error(`Stale localization export ${path}; run localization:compile and commit its exports`);
  }
}
const directory = join(root, desktop ? 'frontend/app' : 'clients/typescript/packages/voice-ui');
const locales = ['en', 'de', 'es', 'zh-Hans', 'hi', 'ar'];
const json = async (path) => JSON.parse(await readFile(path, 'utf8'));
const catalogs = {};
for (const locale of locales) {
  if (desktop) {
    const catalog = await json(join(root, 'pkg/speechkit/localization/catalogs', `${locale}.json`));
    const source = join(directory, 'src/i18n/locales', locale);
    for (const name of (await readdir(source)).filter((name) => name.endsWith('.json')).sort()) {
      for (const [id, text] of Object.entries(await json(join(source, name)))) {
        if (id in catalog) throw new Error(`Duplicate message ${id} in ${locale}/${name}`);
        catalog[id] = text;
      }
    }
    catalogs[locale] = catalog;
  } else catalogs[locale] = await json(join(directory, 'locales', `${locale}.json`));
}
const ids = [...new Set(Object.values(catalogs).flatMap(Object.keys))].sort();
const plural = /_(zero|one|two|few|many|other)$/;
const english = catalogs.en;
for (const id of ids) {
  // Arabic has additional CLDR categories. Other locales use their own `other`
  // form for those extra entries; runtime selection still uses Intl.PluralRules.
  const fallbackId = id.replace(plural, '_other');
  if (!(id in english) && !plural.test(id)) throw new Error(`No English source for ${id}`);
  for (const locale of locales) {
    if (!(id in catalogs[locale])) {
      if (!plural.test(id) || !(fallbackId in catalogs[locale])) {
        throw new Error(`${locale} is missing ${id}`);
      }
      catalogs[locale][id] = catalogs[locale][fallbackId];
    }
  }
}
const variables = (text) => [...new Set([...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]))].sort();
for (const id of ids) {
  for (const locale of locales) {
    if (typeof catalogs[locale][id] !== 'string' || !catalogs[locale][id].trim()) throw new Error(`Empty ${locale}/${id}`);
    const expected = variables(english[id]);
    const actual = variables(catalogs[locale][id]);
    if (JSON.stringify(actual) !== JSON.stringify(expected)) {
      // CLDR singular/dual/zero prose can make the count implicit. Keep the
      // existing translation; the compiler still declares the shared input.
      if (!plural.test(id) || actual.some((name) => !expected.includes(name)) || expected.some((name) => name !== 'count' && !actual.includes(name))) {
        throw new Error(`Placeholder mismatch in ${locale}/${id}`);
      }
      console.warn(`Implicit count in ${locale}/${id}; translation QA remains pending`);
    }
  }
}
if (desktop) {
  await mkdir(join(directory, 'messages'), { recursive: true });
  for (const locale of locales) {
    await writeProjection(join(directory, 'messages', `${locale}.json`), JSON.stringify(Object.fromEntries(ids.map((id) => [id, catalogs[locale][id]])), null, 2) + '\n');
  }
}
const compilerDirectory = process.argv.includes('--compiler-from-desktop') ? join(root, 'frontend/app') : directory;
const require = createRequire(join(compilerDirectory, 'package.json'));
const { compile } = await import(pathToFileURL(require.resolve('@inlang/paraglide-js')).href);
await compile({
  project: join(directory, 'project.inlang'),
  outdir: join(directory, 'src/paraglide'),
  outputStructure: 'locale-modules',
  emitTsDeclarations: true,
  strategy: ['baseLocale'],
  disableAsyncLocalStorage: true,
});
// Paraglide's locale modules expose compiler-safe symbols. Keep semantic IDs at
// the application boundary; fail on collisions or a changed compiler convention.
const symbol = (id) => {
  const lower = id.toLowerCase().replace(/[^a-z0-9_]/g, '_');
  const upper = [...id].filter((char) => /[A-Z]/.test(char)).length;
  return lower + (upper ? upper : '');
};
if (new Set(ids.map(symbol)).size !== ids.length) throw new Error('Paraglide message symbol collision');
await mkdir(join(directory, 'src/paraglide/catalogs'), { recursive: true });
for (const locale of locales) {
  const compiled = await import(pathToFileURL(join(directory, 'src/paraglide/messages', `${locale}.js`)).href);
  const entries = ids.map((id) => {
    if (typeof compiled[symbol(id)] !== 'function') throw new Error(`Paraglide did not emit ${locale}/${id}`);
    const params = variables(english[id]);
    const inputs = params.map((name) => `${JSON.stringify(name)}: inputs[${JSON.stringify(name)}] ?? ${JSON.stringify(`{${name}}`)}`).join(', ');
    if (!desktop && compiled[symbol(id)]({}) !== catalogs[locale][id]) throw new Error(`Compiled text changed ${locale}/${id}`);
    return params.length
      ? `  ${JSON.stringify(id)}: (inputs: Record<string, string | number> = {}) => m.${symbol(id)}({${inputs}}),`
      : `  ${JSON.stringify(id)}: m.${symbol(id)},`;
  });
  await writeFile(join(directory, 'src/paraglide/catalogs', `${locale}.ts`), `// Generated by scripts/localization/compile.mjs.\nimport * as m from '../messages/${locale}.js';\nexport const catalog = {\n${entries.join('\n')}\n};\n`);
}
if (!desktop) {
  const renderedEntries = ids.map((id) => `    ${JSON.stringify(id)}: messages[${JSON.stringify(id)}]({}),`).join('\n');
  const output = `// Generated from locales/en.json by scripts/localization/compile.mjs.\nexport const VOICE_UI_MESSAGE_IDS = ${JSON.stringify(ids, null, 2)} as const;\nexport type VoiceUiMessageId = (typeof VOICE_UI_MESSAGE_IDS)[number];\nexport type VoiceUiMessageCatalog = Readonly<Record<VoiceUiMessageId, string>>;\nexport function renderCatalog(messages: Record<VoiceUiMessageId, (inputs: Record<string, never>) => string>): VoiceUiMessageCatalog {\n  return {\n${renderedEntries}\n  };\n}\n`;
  await writeProjection(join(directory, 'src/i18n/message-ids.ts'), output);
}
console.log(`${surface}: compiled ${ids.length} semantic messages in ${locales.length} locales`);
