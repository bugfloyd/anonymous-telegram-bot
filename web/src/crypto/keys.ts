// P-256 keys in WebCrypto.
//
// Device keys are generated non-extractable: they can be used, but never read
// out, not even by our own code. Keys that must be copied to a new device
// (the account key, link keys, persona keys) are generated once as
// extractable, sealed into the vault (vault.ts), and then used through a
// non-extractable working copy.

import { concat, toBase64Url } from './encoding';
import { message } from './signing';

/** Algorithm suite: P-256, HKDF-SHA256, AES-256-GCM. Recorded with every key. */
export const SUITE = 0x01;

const SIGNING: EcKeyGenParams = { name: 'ECDSA', namedCurve: 'P-256' };
const AGREEMENT: EcKeyGenParams = { name: 'ECDH', namedCurve: 'P-256' };

/** A new non-extractable ECDSA key pair (a device signing key). */
export function generateSigningKey(): Promise<CryptoKeyPair> {
  return crypto.subtle.generateKey(SIGNING, false, ['sign', 'verify']);
}

/** A new non-extractable ECDH key pair (a device encryption key). */
export function generateAgreementKey(): Promise<CryptoKeyPair> {
  return crypto.subtle.generateKey(AGREEMENT, false, ['deriveBits']);
}

/** A new ECDSA key pair whose private key can be sealed and copied. */
export function generateCopyableSigningKey(): Promise<CryptoKeyPair> {
  return crypto.subtle.generateKey(SIGNING, true, ['sign', 'verify']);
}

/** The 65-byte uncompressed public point, as the backend parses it. */
export async function exportPublicKey(publicKey: CryptoKey): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.exportKey('raw', publicKey));
}

/** Imports a 65-byte public point for verifying signatures. */
export function importSigningPublicKey(raw: Uint8Array<ArrayBuffer>): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', raw, SIGNING, true, ['verify']);
}

/**
 * A key's fingerprint: SHA-256 over a labelled message holding the suite and
 * the public point, written as 43 base64url characters. It's what links carry,
 * so a server that swaps keys is caught.
 */
export async function fingerprint(publicKey: CryptoKey): Promise<string> {
  const raw = await exportPublicKey(publicKey);
  const digest = await crypto.subtle.digest('SHA-256', message('incognitochat/v2/fingerprint', new Uint8Array([SUITE]), raw));
  return toBase64Url(new Uint8Array(digest));
}

/** Prefixes data with the suite byte, for anything stored or sent with a key. */
export function withSuite(bytes: Uint8Array): Uint8Array<ArrayBuffer> {
  return concat(new Uint8Array([SUITE]), bytes);
}
