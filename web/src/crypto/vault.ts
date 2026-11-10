// The vault seals keys that must be copyable to a new device, under a
// non-extractable AES-GCM key that never leaves this device.
//
// Honest limit: any script running on the app's origin can ask the vault key
// to unseal. The non-extractable flag really protects only per-device keys;
// sealing keeps copyable keys out of plain sight in storage and limits them to
// this origin.

import { concat, randomBytes } from './encoding';

const AES: AesKeyGenParams = { name: 'AES-GCM', length: 256 };
const IV_LENGTH = 12;

/** A new non-extractable vault key. */
export function generateVaultKey(): Promise<CryptoKey> {
  return crypto.subtle.generateKey(AES, false, ['wrapKey', 'unwrapKey']);
}

/**
 * Seals an extractable private key (PKCS8) under the vault key. The key's name
 * is authenticated with it, so sealed keys can't be swapped between names.
 */
export async function seal(vaultKey: CryptoKey, privateKey: CryptoKey, name: string): Promise<Uint8Array<ArrayBuffer>> {
  const iv = randomBytes(IV_LENGTH);
  const wrapped = await crypto.subtle.wrapKey('pkcs8', privateKey, vaultKey, {
    name: 'AES-GCM',
    iv,
    additionalData: new TextEncoder().encode(name),
  });
  return concat(iv, new Uint8Array(wrapped));
}

/**
 * Unseals a private key as a non-extractable working copy with the given
 * algorithm and usages. Fails if the bytes or the name don't match.
 */
export function unseal(
  vaultKey: CryptoKey,
  sealed: Uint8Array<ArrayBuffer>,
  name: string,
  algorithm: EcKeyImportParams,
  usages: KeyUsage[],
): Promise<CryptoKey> {
  return crypto.subtle.unwrapKey(
    'pkcs8',
    sealed.slice(IV_LENGTH),
    vaultKey,
    { name: 'AES-GCM', iv: sealed.slice(0, IV_LENGTH), additionalData: new TextEncoder().encode(name) },
    algorithm,
    false,
    usages,
  );
}
