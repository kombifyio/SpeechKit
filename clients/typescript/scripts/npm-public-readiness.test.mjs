import assert from 'node:assert/strict'
import test from 'node:test'
import { publicationBlockers } from './npm-public-readiness.mjs'

// White-box exception: this is the signing-provider eligibility boundary,
// tested without credentials or a live registry write.
test('public provenance cannot be enabled by token presence at the wrong build origin', () => {
  const manifest = {
    name: '@kombifyio/speechkit-client',
    repository: { url: 'git+https://github.com/kombifyio/SpeechKit.git' },
    publishConfig: { access: 'public', provenance: true },
  }
  const context = {
    repository: 'kombifyio/SpeechKit',
    visibility: 'public',
    manifests: [manifest],
    tokenConfigured: true,
  }
  assert.deepEqual(publicationBlockers(context), [])
  for (const change of [
    { visibility: 'internal' },
    { repository: 'example/private-source' },
    { tokenConfigured: false },
    { manifests: [{ ...manifest, publishConfig: { access: 'public', provenance: false } }] },
  ]) {
    assert.ok(publicationBlockers({ ...context, ...change }).length > 0)
  }
})
