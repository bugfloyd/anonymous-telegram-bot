import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { i2osp, modPow, os2ip, verify, type IssuerKey } from '../crypto/blindrsa';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import type { Session } from '../net/session';
import { challenge, EPOCH_SECONDS, epochOf, TOKEN_INPUT_LENGTH } from '../protocol/token';
import { openDatabase } from '../storage/db';
import { countTokens, refillTokens, returnToken, takeToken, type TokenIssuer } from './wallet';

let n = 0;
const NOW = 1_800_000_000;

/** An issuer with a fresh key, and a fake session whose POST /tokens signs like the server. */
async function issuerAndSession(): Promise<{ issuer: TokenIssuer; session: Session; signed: () => number }> {
  const pair = await crypto.subtle.generateKey(
    { name: 'RSA-PSS', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-384' },
    true,
    ['sign', 'verify'],
  );
  const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const big = (s: string) => os2ip(fromBase64Url(s));
  const key: IssuerKey = { key: pair.publicKey, n: big(jwk.n!), e: big(jwk.e!), k: 256 };
  const spki = new Uint8Array(await crypto.subtle.exportKey('spki', pair.publicKey));
  const keyId = new Uint8Array(await crypto.subtle.digest('SHA-256', spki));
  let count = 0;
  const session: Session = {
    use: () => {},
    async call(_method, path, body, check) {
      if (path !== '/tokens') throw new Error(path);
      const blinded = (body as { blinded: string[] }).blinded;
      count += blinded.length;
      const d = big(jwk.d!);
      return check({ signatures: blinded.map((b) => toBase64Url(i2osp(modPow(os2ip(fromBase64Url(b)), d, key.n), 256))) });
    },
  };
  return { issuer: { key, keyId }, session, signed: () => count };
}

describe('token wallet', () => {
  it('fills with valid tokens and hands out each once', async () => {
    const db = await openDatabase(`wallet-test-${n++}`);
    const { issuer, session, signed } = await issuerAndSession();
    expect(await refillTokens(db, session, issuer, 3, NOW)).toBe(3);
    expect(signed()).toBe(3);
    expect(await countTokens(db, NOW)).toBe(3);

    const seen = new Set<string>();
    for (let i = 0; i < 3; i++) {
      const token = (await takeToken(db, NOW))!;
      expect(seen.has(token)).toBe(false);
      seen.add(token);
      const raw = fromBase64Url(token);
      expect(raw).toHaveLength(TOKEN_INPUT_LENGTH + 256);
      expect(raw.slice(66, 98)).toEqual(issuer.keyId);
      expect(await verify(issuer.key, raw.slice(0, TOKEN_INPUT_LENGTH), raw.slice(TOKEN_INPUT_LENGTH))).toBe(true);
    }
    expect(await takeToken(db, NOW)).toBeNull();
  });

  it("keeps last week's tokens and drops older ones", async () => {
    const db = await openDatabase(`wallet-test-${n++}`);
    const { issuer, session } = await issuerAndSession();
    await refillTokens(db, session, issuer, 2, NOW);
    expect(await countTokens(db, NOW + EPOCH_SECONDS)).toBe(2);
    expect(await countTokens(db, NOW + 2 * EPOCH_SECONDS)).toBe(0);
    expect(await takeToken(db, NOW + 2 * EPOCH_SECONDS)).toBeNull();
  });

  it('takes the oldest first and can take one back', async () => {
    const db = await openDatabase(`wallet-test-${n++}`);
    const { issuer, session } = await issuerAndSession();
    await refillTokens(db, session, issuer, 1, NOW - EPOCH_SECONDS);
    await refillTokens(db, session, issuer, 1, NOW);
    const lastWeek = await challenge(epochOf(NOW) - 1);
    const thisWeek = await challenge(epochOf(NOW));
    const first = (await takeToken(db, NOW))!;
    expect(fromBase64Url(first).slice(34, 66)).toEqual(lastWeek);
    await returnToken(db, first, epochOf(NOW) - 1);
    expect(await takeToken(db, NOW)).toBe(first);
    expect(fromBase64Url((await takeToken(db, NOW))!).slice(34, 66)).toEqual(thisWeek);
  });
});
