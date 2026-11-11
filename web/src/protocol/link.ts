// Links (design §8), byte-compatible with the backend (backend/internal/links).
//
// A link is t.me/<bot>/<app>?startapp=<payload>, where
//   payload = "1" ‖ link ID (8 bytes, 11 base64url characters)
//             ‖ fingerprint of the link signing key (43 characters)
// 55 characters, fixed length, no separator (base64url uses both - and _).
//
// The bundle the server returns for a link ID is only trusted once its signing
// key matches the fingerprint and every encryption key is signed by it: a
// server that swaps keys is caught.

import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { fingerprint, importAgreementPublicKey, importSigningPublicKey, SUITE } from '../crypto/keys';
import { LABEL_LINK_KEY, verify } from '../crypto/signing';

export const LINK_VERSION = '1';
const ID_LENGTH = 11;
const FP_LENGTH = 43;
export const PAYLOAD_LENGTH = 1 + ID_LENGTH + FP_LENGTH;

/** How long a new encryption key is offered for. */
export const KEY_VALIDITY_SECONDS = 5 * 7 * 24 * 3600;

export interface LinkRef {
  id: string;
  fingerprint: string;
}

/** A link ID: 8 random bytes. */
export function newLinkId(): string {
  return toBase64Url(crypto.getRandomValues(new Uint8Array(8)));
}

export function encodePayload(ref: LinkRef): string {
  return LINK_VERSION + ref.id + ref.fingerprint;
}

/** Reads a start parameter as a link, or returns null. */
export function parsePayload(startParam: string): LinkRef | null {
  if (startParam.length !== PAYLOAD_LENGTH || !startParam.startsWith(LINK_VERSION)) return null;
  const id = startParam.slice(1, 1 + ID_LENGTH);
  const fp = startParam.slice(1 + ID_LENGTH);
  if (!canonical(id, 8) || !canonical(fp, 32)) return null;
  return { id, fingerprint: fp };
}

export function linkUrl(botUsername: string, appName: string, ref: LinkRef): string {
  return `https://t.me/${botUsername}/${appName}?startapp=${encodePayload(ref)}`;
}

/** The signed parts of an encryption key (backend: links.KeyParts). */
export function keyParts(key: Uint8Array, from: number, until: number): Uint8Array[] {
  return [new Uint8Array([SUITE]), key, u64(from), u64(until)];
}

/** An encryption key in a bundle, as the API sends it. */
export interface BundleKey {
  key: Uint8Array<ArrayBuffer>;
  from: number;
  until: number;
  signature: Uint8Array<ArrayBuffer>;
}

export interface Bundle {
  signingKey: Uint8Array<ArrayBuffer>;
  keys: BundleKey[];
}

/** Checks the shape of GET /links/{id}. Says nothing about whether to trust it. */
export function checkBundle(value: unknown): Bundle {
  if (!value || typeof value !== 'object') throw new Error('not an object');
  const { signingKey, keys } = value as Record<string, unknown>;
  if (typeof signingKey !== 'string' || !Array.isArray(keys) || keys.length === 0 || keys.length > 4) throw new Error('bad bundle');
  return {
    signingKey: fromBase64Url(signingKey),
    keys: keys.map((k: unknown) => {
      const { key, from, until, signature } = (k ?? {}) as Record<string, unknown>;
      if (typeof key !== 'string' || typeof signature !== 'string' || !Number.isSafeInteger(from) || !Number.isSafeInteger(until)) {
        throw new Error('bad key');
      }
      return { key: fromBase64Url(key), from: from as number, until: until as number, signature: fromBase64Url(signature) };
    }),
  };
}

export class BundleMismatch extends Error {
  constructor(reason: string) {
    super(`link bundle rejected: ${reason}`);
    this.name = 'BundleMismatch';
  }
}

/**
 * Verifies a bundle against the link it was fetched for and returns the
 * encryption key to use now (the newest current one). Throws BundleMismatch
 * if anything doesn't add up.
 */
export async function verifyBundle(ref: LinkRef, bundle: Bundle, nowSeconds: number): Promise<{ key: CryptoKey; raw: Uint8Array }> {
  let signingKey: CryptoKey;
  try {
    signingKey = await importSigningPublicKey(bundle.signingKey);
  } catch {
    throw new BundleMismatch('signing key is not a P-256 point');
  }
  if ((await fingerprint(signingKey)) !== ref.fingerprint) throw new BundleMismatch('fingerprint');

  let best: BundleKey | null = null;
  for (const k of bundle.keys) {
    if (!(await verify(signingKey, k.signature, LABEL_LINK_KEY, ...keyParts(k.key, k.from, k.until)))) {
      throw new BundleMismatch('key signature');
    }
    if (k.from <= nowSeconds && nowSeconds < k.until && (!best || k.from > best.from)) best = k;
  }
  if (!best) throw new BundleMismatch('no current key');
  try {
    return { key: await importAgreementPublicKey(best.key), raw: best.key };
  } catch {
    throw new BundleMismatch('encryption key is not a P-256 point');
  }
}

/** Whether text is the canonical unpadded base64url encoding of `bytes` bytes. */
function canonical(text: string, bytes: number): boolean {
  if (!/^[A-Za-z0-9_-]+$/.test(text)) return false;
  try {
    const decoded = fromBase64Url(text);
    return decoded.length === bytes && toBase64Url(decoded) === text;
  } catch {
    return false;
  }
}

function u64(n: number): Uint8Array {
  if (!Number.isSafeInteger(n) || n < 0) throw new Error('not a u64');
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, BigInt(n));
  return out;
}
