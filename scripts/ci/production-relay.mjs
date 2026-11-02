import { createHash } from 'node:crypto';
import { writeFile, appendFile } from 'node:fs/promises';
import { keyFingerprint, seal } from './production-relay-crypto.mjs';

const env = name => { const v=process.env[name]; if (!v) throw new Error(`${name} required`); return v; };
const publicKey=Buffer.from(env('RECIPIENT_KEY'),'base64url').toString('utf8');
const tag=env('RELEASE_TAG');
const context={nonce:env('NONCE'),runId:env('GITHUB_RUN_ID'),attempt:env('GITHUB_RUN_ATTEMPT'),imageId:env('IMAGE_ID'),parentRun:env('PARENT_RUN'),sourceSha:env('SOURCE_SHA'),tag,keyFingerprint:keyFingerprint(publicKey)};
if (!/^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(tag)) throw new Error('Invalid release tag');
const image='kombifyio/speechkit-server';
const token=env('GITHUB_TOKEN');
const auth=await fetch(`https://ghcr.io/token?service=ghcr.io&scope=repository:${image}:pull`,{signal:AbortSignal.timeout(30000),headers:{Authorization:`Basic ${Buffer.from(`${env('GITHUB_ACTOR')}:${token}`).toString('base64')}`}});
if (!auth.ok) throw new Error(`Registry authorization failed: ${auth.status}`);
const bearer=(await auth.json()).token;
if (!bearer) throw new Error('Registry token missing');
const accept=['application/vnd.oci.image.manifest.v1+json','application/vnd.oci.image.index.v1+json','application/vnd.docker.distribution.manifest.v2+json','application/vnd.docker.distribution.manifest.list.v2+json'].join(',');
async function manifest(ref) {
  const response=await fetch(`https://ghcr.io/v2/${image}/manifests/${encodeURIComponent(ref)}`,{signal:AbortSignal.timeout(30000),headers:{Authorization:`Bearer ${bearer}`,Accept:accept}});
  if (response.status===404) return null;
  if (!response.ok) throw new Error(`Registry read failed: ${response.status}`);
  const bytes=Buffer.from(await response.arrayBuffer());
  const digest=`sha256:${createHash('sha256').update(bytes).digest('hex')}`;
  if (response.headers.get('docker-content-digest') !== digest) throw new Error('Registry manifest digest mismatch');
  const value=JSON.parse(bytes.toString('utf8'));
  if (Array.isArray(value.manifests)) {
    const children=value.manifests.filter(x=>x.platform?.os==='linux' && x.platform?.architecture==='amd64');
    if (children.length!==1) throw new Error('Unexpected production platform');
    const child=await manifest(children[0].digest);
    if (!child || child.digest!==children[0].digest) throw new Error('Registry child mismatch');
    return {digest,config:child.config};
  }
  if (!/^sha256:[a-f0-9]{64}$/.test(value.config?.digest || '')) throw new Error('Missing image config digest');
  return {digest,config:value.config.digest};
}
async function probe() {
  const [commit,version]=await Promise.all([manifest(`git-${context.sourceSha}`),manifest(`${tag}-kombify`)]);
  for (const found of [commit,version]) if (found && found.config!==context.imageId) throw new Error('Conflicting immutable production tag');
  if (commit && version && commit.digest!==version.digest) throw new Error('Production tag manifests differ');
  return {commit,version};
}
async function receipt(digest) {
  await writeFile(`${env('RUNNER_TEMP')}/production-image-receipt.json`,JSON.stringify({v:1,kind:'speechkit-production-image',context,tag,digest,image_ref:`ghcr.io/${image}:${tag}-kombify@${digest}`}));
}
if (process.argv[2]==='prepare') {
  const found=await probe();
  if (found.commit && found.version) {
    await receipt(found.commit.digest);
    await appendFile(env('GITHUB_OUTPUT'),'state=complete\n');
  } else {
    if (found.commit || found.version) throw new Error('Partial existing production publication');
    await writeFile(`${env('RUNNER_TEMP')}/production-capability.json`,JSON.stringify(seal(token,publicKey,context)),{mode:0o600});
    await appendFile(env('GITHUB_OUTPUT'),'state=waiting\n');
  }
} else if (process.argv[2]==='wait') {
  const deadline=Date.now()+7*60*1000;
  while (Date.now()<deadline) {
    const found=await probe();
    if (found.commit && found.version) { await receipt(found.commit.digest); process.exit(0); }
    await new Promise(resolve=>setTimeout(resolve,5000));
  }
  throw new Error('Production publication did not complete before timeout');
} else throw new Error('Unknown relay action');
