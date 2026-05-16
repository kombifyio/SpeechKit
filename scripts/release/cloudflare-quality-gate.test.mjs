import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { test } from 'node:test';
import { resolve } from 'node:path';

test('Cloudflare quality gate refuses blocking mode before promotion', () => {
  const result = spawnSync(process.execPath, [
    resolve('scripts/cloudflare-quality-gate.mjs'),
    '--worker-url',
    'https://quality-gate.example.test',
    '--ref',
    'main',
    '--release-version',
    '0.34.1',
    '--require-pass',
  ], {
    encoding: 'utf8',
    env: {
      ...process.env,
      SPEECHKIT_CLOUDFLARE_GATE_PROMOTED: '',
      SPEECHKIT_CLOUDFLARE_GATE_TOKEN: 'test-token',
      QUALITY_GATE_API_TOKEN: 'test-token',
    },
  });

  assert.equal(result.status, 1, result.stderr || result.stdout);
  assert.match(result.stderr, /shadow-only/);
});
