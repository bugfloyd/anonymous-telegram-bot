// Anonymous send tokens (design §11, backend/internal/tokens).
//
//   token     = type (2) ‖ nonce (32) ‖ challenge (32) ‖ key ID (32) ‖ authenticator (256)
//   challenge = SHA-256(Message("incognitochat/v2/send-token", [suite], u64(epoch)))
//
// The authenticator is a blind RSA signature (crypto/blindrsa.ts) over the
// first 98 bytes. Epochs are weeks; a token is accepted in its own week and
// the next.

import { concat, randomBytes } from '../crypto/encoding';
import { SUITE } from '../crypto/keys';
import { message } from '../crypto/signing';

export const TOKEN_TYPE = 0x0002;
export const EPOCH_SECONDS = 7 * 24 * 3600;
export const LABEL_SEND_TOKEN = 'incognitochat/v2/send-token';
export const TOKEN_INPUT_LENGTH = 2 + 32 + 32 + 32;

export function epochOf(unixSeconds: number): number {
  return Math.floor(unixSeconds / EPOCH_SECONDS);
}

export async function challenge(epoch: number): Promise<Uint8Array<ArrayBuffer>> {
  const e = new Uint8Array(8);
  new DataView(e.buffer).setBigUint64(0, BigInt(epoch));
  return new Uint8Array(await crypto.subtle.digest('SHA-256', message(LABEL_SEND_TOKEN, new Uint8Array([SUITE]), e)));
}

/** A fresh token input for an epoch and issuer key. */
export async function tokenInput(epoch: number, keyId: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  if (keyId.length !== 32) throw new Error('bad key ID');
  return concat(new Uint8Array([TOKEN_TYPE >> 8, TOKEN_TYPE & 0xff]), randomBytes(32), await challenge(epoch), keyId);
}
