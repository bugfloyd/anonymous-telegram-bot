// The account secret (design §6, §9, §11): 32 random bytes shared by an
// account's devices and nobody else. It derives:
//   - sender tags: HMAC(secret, Message("incognitochat/v2/sender-tag", link fp)),
//     the same for every conversation an account starts through one link, so
//     the link's owner can block the person and not just a conversation;
//   - labels: short names like "🦊 K3F9" for the people one talks to, computed
//     on the device. The same person through the same link keeps their label.
//
// Kept as an HMAC key: sealed in the vault (copied to new devices later), used
// through a non-extractable working copy.

import { getKey, getSealed, putKey, putSealed, type Database } from '../storage/db';
import { message } from '../crypto/signing';
import { utf8 } from '../crypto/encoding';
import { seal, unseal } from '../crypto/vault';
import { withLock } from '../sync/locks';

const NAME = 'account-secret';
const KEY_VAULT = 'vault-key';
const HMAC: HmacImportParams = { name: 'HMAC', hash: 'SHA-256' };

export const LABEL_SENDER_TAG = 'incognitochat/v2/sender-tag';
export const LABEL_LABEL = 'incognitochat/v2/label';

// Frozen with v1's labels: changing or reordering either changes everyone's labels.
const EMOJI = [
  '🦊', '🐼', '🐙', '🦉', '🐢', '🦄', '🐝', '🦖', '🦋', '🐧', '🦁', '🦌', '🐨', '🐸', '🦀', '🐳',
  '🦒', '🦓', '🦔', '🦦', '🦩', '🦭', '🐞', '🦜', '🦚', '🦘', '🐘', '🐪', '🦢', '🐠', '🦈', '🐉',
  '🌵', '🌻', '🍄', '🍉', '🍋', '🍒', '🍍', '🥥', '🥑', '🍩', '🧁', '🎈', '🚀', '⚓', '🎲', '🔔',
  '💎', '🌙', '⭐', '🌈', '🍀', '🪁', '🧩', '🌊', '🔥', '⚡', '🎸', '🎨', '🧭', '🪐', '🍁', '🌸',
] as const;
/** Crockford base32: no I, L, O or U. */
const ALPHABET = '0123456789ABCDEFGHJKMNPQRSTVWXYZ';

/** The account secret's working copy, created on first use. */
export function accountSecret(db: Database): Promise<CryptoKey> {
  return withLock(NAME, async () => {
    const existing = await getKey(db, NAME);
    if (existing) return existing;
    const vaultKey = await getKey(db, KEY_VAULT);
    if (!vaultKey) throw new Error('no vault key');
    let sealed = await getSealed(db, NAME);
    if (!sealed) {
      const secret = await crypto.subtle.generateKey({ ...HMAC, length: 256 }, true, ['sign']);
      sealed = await seal(vaultKey, secret, NAME, 'raw');
      await putSealed(db, NAME, sealed);
    }
    const working = await unseal(vaultKey, sealed, NAME, HMAC, ['sign'], 'raw');
    await putKey(db, NAME, working);
    return working;
  });
}

async function hmac(secret: CryptoKey, label: string, ...parts: Uint8Array[]): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.sign('HMAC', secret, message(label, ...parts)));
}

/** Our sender tag for a link: 32 bytes. */
export function senderTag(secret: CryptoKey, linkFingerprint: string): Promise<Uint8Array<ArrayBuffer>> {
  return hmac(secret, LABEL_SENDER_TAG, utf8(linkFingerprint));
}

/**
 * A label: one of 64 emoji and 4 Crockford base32 characters (about 67
 * million). kind "anon" names someone who wrote to our link (from their sender
 * tag); kind "link" names a link we wrote to (from its fingerprint).
 */
export async function label(secret: CryptoKey, kind: 'anon' | 'link', data: Uint8Array): Promise<string> {
  const h = await hmac(secret, LABEL_LABEL, utf8(kind), data);
  const code = (h[1]! << 16) | (h[2]! << 8) | h[3]!;
  let out = '';
  for (let shift = 15; shift >= 0; shift -= 5) out += ALPHABET[(code >> shift) & 31];
  return `${EMOJI[h[0]! % 64]} ${out}`;
}
