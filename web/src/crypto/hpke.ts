// HPKE (RFC 9180), Base mode, single shot: DHKEM(P-256, HKDF-SHA256),
// HKDF-SHA256, AES-GCM. Written against WebCrypto and checked with the RFC's
// test vectors (hpke-vectors.json) rather than taken from a library: the most
// common JavaScript one is unaudited and had a nonce-reuse bug in 2025.
//
// Only what the envelope needs: one Seal per context (sequence number 0), no
// PSK or Auth modes, no exporter.

import { concat, utf8 } from './encoding';

export const KEM_P256_SHA256 = 0x0010;
export const KDF_HKDF_SHA256 = 0x0001;
export const AEAD_AES_128_GCM = 0x0001;
export const AEAD_AES_256_GCM = 0x0002;
export type AeadId = typeof AEAD_AES_128_GCM | typeof AEAD_AES_256_GCM;

const N_SECRET = 32; // DHKEM(P-256) shared secret length
const N_H = 32; // SHA-256 output length
const N_N = 12; // AES-GCM nonce length
const N_K: Record<AeadId, number> = { [AEAD_AES_128_GCM]: 16, [AEAD_AES_256_GCM]: 32 };
const MODE_BASE = 0x00;
const ECDH: EcKeyImportParams = { name: 'ECDH', namedCurve: 'P-256' };

const HPKE_V1 = utf8('HPKE-v1');
const KEM_SUITE = concat(utf8('KEM'), i2osp(KEM_P256_SHA256, 2));

function hpkeSuite(aead: AeadId): Uint8Array {
  return concat(utf8('HPKE'), i2osp(KEM_P256_SHA256, 2), i2osp(KDF_HKDF_SHA256, 2), i2osp(aead, 2));
}

function i2osp(n: number, length: number): Uint8Array {
  const out = new Uint8Array(length);
  for (let i = length - 1; i >= 0; i--, n >>>= 8) out[i] = n & 0xff;
  return out;
}

async function hmac(key: Uint8Array, data: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  // HKDF's empty salt is HashLen zero bytes (RFC 5869); WebCrypto refuses
  // empty HMAC keys, and the two give the same HMAC.
  const raw = key.length === 0 ? new Uint8Array(N_H) : Uint8Array.from(key);
  const k = await crypto.subtle.importKey('raw', raw, { name: 'HMAC', hash: 'SHA-256' }, false, ['sign']);
  return new Uint8Array(await crypto.subtle.sign('HMAC', k, Uint8Array.from(data)));
}

function labeledExtract(salt: Uint8Array, suite: Uint8Array, label: string, ikm: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return hmac(salt, concat(HPKE_V1, suite, utf8(label), ikm));
}

async function labeledExpand(prk: Uint8Array, suite: Uint8Array, label: string, info: Uint8Array, length: number): Promise<Uint8Array<ArrayBuffer>> {
  const labeledInfo = concat(i2osp(length, 2), HPKE_V1, suite, utf8(label), info);
  const out = new Uint8Array(length);
  let block: Uint8Array = new Uint8Array(0);
  for (let i = 1, filled = 0; filled < length; i++) {
    block = await hmac(prk, concat(block, labeledInfo, new Uint8Array([i])));
    out.set(block.subarray(0, Math.min(N_H, length - filled)), filled);
    filled += N_H;
  }
  return out;
}

async function extractAndExpand(dh: Uint8Array, kemContext: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const prk = await labeledExtract(new Uint8Array(0), KEM_SUITE, 'eae_prk', dh);
  return labeledExpand(prk, KEM_SUITE, 'shared_secret', kemContext, N_SECRET);
}

async function dh(privateKey: CryptoKey, publicKey: CryptoKey): Promise<Uint8Array> {
  return new Uint8Array(await crypto.subtle.deriveBits({ name: 'ECDH', public: publicKey }, privateKey, 256));
}

/** Encap: a fresh ephemeral key (or the given one, for tests) against pkR. */
export async function encap(
  pkRm: Uint8Array<ArrayBuffer>,
  ephemeral?: CryptoKeyPair,
): Promise<{ sharedSecret: Uint8Array<ArrayBuffer>; enc: Uint8Array<ArrayBuffer> }> {
  const pkR = await crypto.subtle.importKey('raw', pkRm, ECDH, true, []);
  const e = ephemeral ?? (await crypto.subtle.generateKey(ECDH, false, ['deriveBits']));
  const enc = new Uint8Array(await crypto.subtle.exportKey('raw', e.publicKey));
  const sharedSecret = await extractAndExpand(await dh(e.privateKey, pkR), concat(enc, pkRm));
  return { sharedSecret, enc };
}

/** Decap: the shared secret for enc, with the recipient's key pair. */
export async function decap(enc: Uint8Array<ArrayBuffer>, skR: CryptoKey, pkRm: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const pkE = await crypto.subtle.importKey('raw', enc, ECDH, true, []);
  return extractAndExpand(await dh(skR, pkE), concat(enc, pkRm));
}

/** KeySchedule for mode_base, without PSK. */
export async function keySchedule(
  sharedSecret: Uint8Array,
  info: Uint8Array,
  aead: AeadId,
): Promise<{ key: Uint8Array<ArrayBuffer>; baseNonce: Uint8Array<ArrayBuffer>; context: Uint8Array<ArrayBuffer>; secret: Uint8Array<ArrayBuffer> }> {
  const suite = hpkeSuite(aead);
  const pskIdHash = await labeledExtract(new Uint8Array(0), suite, 'psk_id_hash', new Uint8Array(0));
  const infoHash = await labeledExtract(new Uint8Array(0), suite, 'info_hash', info);
  const context = concat(new Uint8Array([MODE_BASE]), pskIdHash, infoHash);
  const secret = await labeledExtract(sharedSecret, suite, 'secret', new Uint8Array(0));
  const key = await labeledExpand(secret, suite, 'key', context, N_K[aead]);
  const baseNonce = await labeledExpand(secret, suite, 'base_nonce', context, N_N);
  return { key, baseNonce, context, secret };
}

async function aesGcm(key: Uint8Array<ArrayBuffer>, usage: 'encrypt' | 'decrypt'): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', key, { name: 'AES-GCM' }, false, [usage]);
}

/** Single-shot Seal: encrypts pt to the public key pkRm. */
export async function seal(
  pkRm: Uint8Array<ArrayBuffer>,
  info: Uint8Array,
  aad: Uint8Array<ArrayBuffer>,
  pt: Uint8Array<ArrayBuffer>,
  options: { aead?: AeadId; ephemeral?: CryptoKeyPair } = {},
): Promise<{ enc: Uint8Array<ArrayBuffer>; ct: Uint8Array<ArrayBuffer> }> {
  const aead = options.aead ?? AEAD_AES_256_GCM;
  const { sharedSecret, enc } = await encap(pkRm, options.ephemeral);
  const { key, baseNonce } = await keySchedule(sharedSecret, info, aead);
  const ct = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: baseNonce, additionalData: aad },
    await aesGcm(key, 'encrypt'),
    pt,
  );
  return { enc, ct: new Uint8Array(ct) };
}

/** Single-shot Open. Throws if anything doesn't match. */
export async function open(
  enc: Uint8Array<ArrayBuffer>,
  skR: CryptoKey,
  pkRm: Uint8Array,
  info: Uint8Array,
  aad: Uint8Array<ArrayBuffer>,
  ct: Uint8Array<ArrayBuffer>,
  options: { aead?: AeadId } = {},
): Promise<Uint8Array<ArrayBuffer>> {
  const aead = options.aead ?? AEAD_AES_256_GCM;
  const sharedSecret = await decap(enc, skR, pkRm);
  const { key, baseNonce } = await keySchedule(sharedSecret, info, aead);
  const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: baseNonce, additionalData: aad }, await aesGcm(key, 'decrypt'), ct);
  return new Uint8Array(pt);
}
