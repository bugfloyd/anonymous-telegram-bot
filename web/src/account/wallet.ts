// The send-token wallet: tokens fetched while logged in, spent one per
// message to another person (design §11). Kept on the device only.
//
// Tokens are fetched in batches, never right before a send: the issuer would
// otherwise see an account fetch one and, a moment later, an anonymous send.

import { blind, finalize, type IssuerKey } from '../crypto/blindrsa';
import { concat, fromBase64Url, toBase64Url } from '../crypto/encoding';
import type { Session } from '../net/session';
import { checkObject } from '../net/api';
import { epochOf, tokenInput } from '../protocol/token';
import { getMeta, putMeta, type Database } from '../storage/db';
import { withLock } from '../sync/locks';

const META_TOKENS = 'send-tokens';
const LOCK = 'send-tokens';
/** Batch size: the server signs at most 50 per request. */
export const TOKEN_BATCH = 20;
/** Refill when fewer than this many are left. */
export const LOW_WATER = 10;

export interface StoredToken {
  token: string;
  epoch: number;
}

export interface TokenIssuer {
  key: IssuerKey;
  keyId: Uint8Array<ArrayBuffer>;
}

const nowSeconds = () => Math.floor(Date.now() / 1000);

async function stored(db: Database): Promise<StoredToken[]> {
  const value = await getMeta<unknown>(db, META_TOKENS);
  if (!Array.isArray(value)) return [];
  return value.filter((t): t is StoredToken => !!t && typeof t.token === 'string' && Number.isSafeInteger(t.epoch));
}

/** Tokens still accepted: this week's and last week's. */
function usable(tokens: StoredToken[], now: number): StoredToken[] {
  const epoch = epochOf(now);
  return tokens.filter((t) => t.epoch === epoch || t.epoch === epoch - 1);
}

export async function countTokens(db: Database, now = nowSeconds()): Promise<number> {
  return usable(await stored(db), now).length;
}

/** Fetches a batch of tokens for this week: blinded, signed, unblinded. */
export function refillTokens(db: Database, session: Session, issuer: TokenIssuer, count = TOKEN_BATCH, now = nowSeconds()): Promise<number> {
  return withLock(LOCK, async () => {
    const epoch = epochOf(now);
    const inputs: Uint8Array<ArrayBuffer>[] = [];
    const states: Awaited<ReturnType<typeof blind>>['state'][] = [];
    const blinded: string[] = [];
    for (let i = 0; i < count; i++) {
      const input = await tokenInput(epoch, issuer.keyId);
      const b = await blind(issuer.key, input);
      inputs.push(input);
      states.push(b.state);
      blinded.push(toBase64Url(b.blinded));
    }
    const { signatures } = await session.call('POST', '/tokens', { blinded }, (v) => {
      const s = checkObject(v).signatures;
      if (!Array.isArray(s) || s.length !== count || !s.every((x) => typeof x === 'string')) throw new Error('bad signatures');
      return { signatures: s as string[] };
    });
    const fresh: StoredToken[] = [];
    for (let i = 0; i < count; i++) {
      const sig = await finalize(issuer.key, inputs[i]!, fromBase64Url(signatures[i]!), states[i]!);
      fresh.push({ token: toBase64Url(concat(inputs[i]!, sig)), epoch });
    }
    const kept = usable(await stored(db), now);
    await putMeta(db, META_TOKENS, [...kept, ...fresh]);
    return kept.length + fresh.length;
  });
}

/** Takes one token to spend (the oldest still accepted), or null if none are left. */
export function takeToken(db: Database, now = nowSeconds()): Promise<StoredToken | null> {
  return withLock(LOCK, async () => {
    const tokens = usable(await stored(db), now).sort((a, b) => a.epoch - b.epoch);
    const next = tokens.shift();
    await putMeta(db, META_TOKENS, tokens);
    return next ?? null;
  });
}

/** Puts back a token that wasn't spent (the send failed before reaching the server). */
export function returnToken(db: Database, token: string, epoch: number): Promise<void> {
  return withLock(LOCK, async () => {
    await putMeta(db, META_TOKENS, [{ token, epoch }, ...(await stored(db))]);
  });
}
