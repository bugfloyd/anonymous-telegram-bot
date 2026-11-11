import { describe, expect, it } from 'vitest';
import { fromBase64Url, fromHex, toHex } from '../crypto/encoding';
import issuerKeys from '../../issuer-keys.json';
import { challenge, epochOf, TOKEN_INPUT_LENGTH, tokenInput } from './token';

describe('send tokens', () => {
  it('derive the weekly challenge exactly like the backend', async () => {
    // backend/internal/tokens: TestChallengeVector.
    expect(epochOf(1_800_000_000)).toBe(2976);
    expect(toHex(await challenge(2976))).toBe('3ff2ca2b7e3d94fa8b80f80f0f44814f65decd7bbd029649ed7f22149d457f93');
  });

  it('build a 98-byte input with a fresh nonce', async () => {
    const keyId = new Uint8Array(32).fill(7);
    const a = await tokenInput(2976, keyId);
    const b = await tokenInput(2976, keyId);
    expect(a).toHaveLength(TOKEN_INPUT_LENGTH);
    expect(toHex(a.slice(0, 2))).toBe('0002');
    expect(toHex(a.slice(34, 66))).toBe('3ff2ca2b7e3d94fa8b80f80f0f44814f65decd7bbd029649ed7f22149d457f93');
    expect(a.slice(66)).toEqual(keyId);
    expect(toHex(a.slice(2, 34))).not.toBe(toHex(b.slice(2, 34)));
  });

  it('publish only keys whose ID matches them', async () => {
    for (const [name, entry] of Object.entries(issuerKeys)) {
      if (name === '_comment' || typeof entry === 'string') continue;
      const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', fromBase64Url(entry.spki)));
      expect(digest).toEqual(fromHex(entry.keyId));
      const key = await crypto.subtle.importKey('spki', fromBase64Url(entry.spki), { name: 'RSA-PSS', hash: 'SHA-384' }, true, ['verify']);
      expect((key.algorithm as RsaHashedKeyAlgorithm).modulusLength).toBe(2048);
    }
  });
});
