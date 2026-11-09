import {
  createCipheriv,
  createDecipheriv,
  createHash,
  createPublicKey,
  privateDecrypt,
  publicEncrypt,
  randomBytes,
  constants,
} from "node:crypto";

export function keyFingerprint(publicKey) {
  return createHash("sha256")
    .update(createPublicKey(publicKey).export({ type: "spki", format: "der" }))
    .digest("hex");
}
export function binding(input) {
  const { nonce, runId, attempt, imageId, parentRun, sourceSha, tag, keyFingerprint: fp } = input;
  if (
    !/^[A-F0-9]{32}$/.test(nonce) ||
    !/^\d+$/.test(String(runId)) ||
    !/^\d+$/.test(String(attempt)) ||
    !/^sha256:[a-f0-9]{64}$/.test(imageId) ||
    !/^\d+$/.test(String(parentRun)) ||
    !/^[a-f0-9]{40}$/.test(sourceSha) ||
    !/^v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$/.test(tag) ||
    !/^[a-f0-9]{64}$/.test(fp)
  )
    throw new Error("Invalid relay binding");
  return Buffer.from(
    JSON.stringify([
      "speechkit-production-relay.v1",
      nonce,
      String(runId),
      String(attempt),
      imageId,
      String(parentRun),
      sourceSha,
      tag,
      fp,
    ]),
  );
}
export function seal(token, publicKey, context) {
  if (keyFingerprint(publicKey) !== context.keyFingerprint)
    throw new Error("Recipient key mismatch");
  const key = randomBytes(32),
    iv = randomBytes(12);
  try {
    const cipher = createCipheriv("aes-256-gcm", key, iv);
    cipher.setAAD(binding(context));
    const ciphertext = Buffer.concat([cipher.update(Buffer.from(token, "utf8")), cipher.final()]);
    return {
      v: 1,
      context,
      iv: iv.toString("base64"),
      ciphertext: ciphertext.toString("base64"),
      tag: cipher.getAuthTag().toString("base64"),
      wrappedKey: publicEncrypt(
        { key: publicKey, oaepHash: "sha256", padding: constants.RSA_PKCS1_OAEP_PADDING },
        key,
      ).toString("base64"),
    };
  } finally {
    key.fill(0);
  }
}
export function open(envelope, privateKey, expected) {
  if (envelope.v !== 1 || !binding(envelope.context).equals(binding(expected)))
    throw new Error("Relay context mismatch");
  const key = privateDecrypt(
    { key: privateKey, oaepHash: "sha256", padding: constants.RSA_PKCS1_OAEP_PADDING },
    Buffer.from(envelope.wrappedKey, "base64"),
  );
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, Buffer.from(envelope.iv, "base64"));
    decipher.setAAD(binding(expected));
    decipher.setAuthTag(Buffer.from(envelope.tag, "base64"));
    return Buffer.concat([
      decipher.update(Buffer.from(envelope.ciphertext, "base64")),
      decipher.final(),
    ]);
  } finally {
    key.fill(0);
  }
}
