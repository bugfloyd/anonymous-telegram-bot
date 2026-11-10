// Signed messages and P-256 signatures, byte-compatible with the backend
// (backend/internal/devicesig).
//
// A signed message is a label naming its purpose, then length-prefixed parts:
// a 2-byte label length, the label, then each part as a 4-byte length and its
// bytes (big-endian). A signature for one purpose can't be accepted for
// another, and bytes can't be moved between parts.

import { concat, utf8 } from './encoding';

export const LABEL_SESSION = 'incognitochat/v2/session';
export const LABEL_REGISTER = 'incognitochat/v2/register';

const ECDSA: EcdsaParams = { name: 'ECDSA', hash: 'SHA-256' };

/** Encodes a signed message. Parts may be strings (UTF-8) or bytes. */
export function message(label: string, ...parts: (string | Uint8Array)[]): Uint8Array<ArrayBuffer> {
  const labelBytes = utf8(label);
  if (labelBytes.length > 0xffff) throw new Error('label too long');
  const chunks: Uint8Array[] = [u16(labelBytes.length), labelBytes];
  for (const part of parts) {
    const bytes = typeof part === 'string' ? utf8(part) : part;
    chunks.push(u32(bytes.length), bytes);
  }
  return concat(...chunks);
}

/** Signs a message; WebCrypto returns r‖s (P1363, 64 bytes). */
export async function sign(privateKey: CryptoKey, label: string, ...parts: (string | Uint8Array)[]): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.sign(ECDSA, privateKey, message(label, ...parts)));
}

/** Verifies a signature made by sign. */
export async function verify(
  publicKey: CryptoKey,
  signature: Uint8Array<ArrayBuffer>,
  label: string,
  ...parts: (string | Uint8Array)[]
): Promise<boolean> {
  return crypto.subtle.verify(ECDSA, publicKey, signature, message(label, ...parts));
}

function u16(n: number): Uint8Array {
  return new Uint8Array([(n >>> 8) & 0xff, n & 0xff]);
}

function u32(n: number): Uint8Array {
  return new Uint8Array([(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff]);
}
