import assert from 'node:assert/strict'
import test from 'node:test'

import {
  reconcilePublishedPackage,
  requireExactPublishedTarball,
  sha512Integrity,
} from './npm-publish-idempotent.mjs'

const candidate = {
  spec: '@kombifyio/speechkit-voice-ui@0.54.10',
  integrity: sha512Integrity(Buffer.from('candidate tarball')),
}
const exactPublished = {
  integrity: candidate.integrity,
  tarball: 'https://npm.pkg.github.com/example.tgz',
}

test('an existing version is successful only when metadata and downloaded tarball match', async () => {
  let publishCalls = 0
  const result = await reconcilePublishedPackage({
    candidate,
    requireProvenance: true,
    readPublished: async () => ({
      ...exactPublished,
      attestations: { provenance: { predicateType: 'https://slsa.dev/provenance/v1' } },
    }),
    downloadPublished: async () => ({ integrity: candidate.integrity }),
    publish: async () => {
      publishCalls += 1
    },
  })

  assert.equal(result.result, 'already-matched')
  assert.equal(publishCalls, 0)
})

test('different registry metadata fails closed before publish', async () => {
  await assert.rejects(
    reconcilePublishedPackage({
      candidate,
      readPublished: async () => ({
        ...exactPublished,
        integrity: sha512Integrity(Buffer.from('other tarball')),
      }),
      downloadPublished: async () => ({ integrity: candidate.integrity }),
      publish: async () => assert.fail('publish must not run'),
    }),
    /already exists with different content/u,
  )
})

test('public reconciliation rejects identical bytes without provenance before any publication', async () => {
  await assert.rejects(reconcilePublishedPackage({
    candidate,
    requireProvenance: true,
    readPublished: async () => exactPublished,
    downloadPublished: async () => ({ integrity: candidate.integrity }),
    publish: async () => assert.fail('unattested existing versions must not permit publication'),
  }))
})

test('a tarball that disagrees with matching registry metadata fails closed', () => {
  assert.throws(
    () =>
      requireExactPublishedTarball({
        spec: candidate.spec,
        candidateIntegrity: candidate.integrity,
        publishedIntegrity: candidate.integrity,
        publishedTarball: exactPublished.tarball,
        publishedTarballIntegrity: sha512Integrity(Buffer.from('tampered tarball')),
      }),
    /downloaded tarball digest/u,
  )
})

test('missing registry integrity fails closed', () => {
  assert.throws(
    () =>
      requireExactPublishedTarball({
        spec: candidate.spec,
        candidateIntegrity: candidate.integrity,
        publishedIntegrity: undefined,
        publishedTarball: exactPublished.tarball,
        publishedTarballIntegrity: candidate.integrity,
      }),
    /did not return dist\.integrity/u,
  )
})

test('missing registry tarball location fails closed', () => {
  assert.throws(
    () =>
      requireExactPublishedTarball({
        spec: candidate.spec,
        candidateIntegrity: candidate.integrity,
        publishedIntegrity: candidate.integrity,
        publishedTarball: undefined,
        publishedTarballIntegrity: candidate.integrity,
      }),
    /did not return dist\.tarball/u,
  )
})

test('a new version is published and then verified from the registry', async () => {
  let published = false
  const result = await reconcilePublishedPackage({
    candidate,
    readPublished: async () => (published ? exactPublished : null),
    downloadPublished: async () => ({ integrity: candidate.integrity }),
    publish: async () => {
      published = true
    },
  })

  assert.equal(result.result, 'published')
  assert.equal(published, true)
})

test('a concurrent exact publication is accepted after npm publish loses the race', async () => {
  let reads = 0
  const result = await reconcilePublishedPackage({
    candidate,
    readPublished: async () => (++reads === 1 ? null : exactPublished),
    downloadPublished: async () => ({ integrity: candidate.integrity }),
    publish: async () => {
      throw new Error('version already exists')
    },
  })

  assert.equal(result.result, 'already-matched-after-race')
})

test('a publish failure without a visible exact version remains an error', async () => {
  const publishError = new Error('registry denied publication')
  await assert.rejects(
    reconcilePublishedPackage({
      candidate,
      readPublished: async () => null,
      downloadPublished: async () => assert.fail('no published tarball exists'),
      publish: async () => {
        throw publishError
      },
    }),
    (error) => error === publishError,
  )
})
