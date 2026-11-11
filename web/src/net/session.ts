// Logged-in requests. Keeps the session token in memory (never in storage),
// logs in again when it expires, and signs sensitive requests with the device
// key on top of the token.

import { logIn, markReplaced, readAccount, signRequest } from '../account/account';
import type { Database } from '../storage/db';
import { ApiError, type Api, type Method, type TokenResponse } from './api';

/** Renew this long before the server's expiry, to allow for clock skew. */
const MARGIN_SECONDS = 60;

export interface Session {
  /**
   * Sends a logged-in request. sign adds a device signature (needed to
   * create, rotate or revoke links). Retries once after an expired token.
   */
  call<T>(method: Method, path: string, body: unknown, check: (value: unknown) => T, options?: { sign?: boolean }): Promise<T>;
  /** Uses a token just received (from registration). */
  use(token: TokenResponse): void;
}

export function createSession(db: Database, api: Api, now: () => number = () => Math.floor(Date.now() / 1000)): Session {
  let current: TokenResponse | null = null;

  async function token(): Promise<string> {
    if (!current || current.expires - MARGIN_SECONDS <= now()) current = await logIn(db, api, now);
    return current.token;
  }

  return {
    use(t) {
      current = t;
    },
    async call(method, path, body, check, options = {}) {
      const text = body === undefined ? '' : JSON.stringify(body);
      for (let attempt = 0; ; attempt++) {
        const headers: Record<string, string> = { Authorization: `Bearer ${await token()}` };
        if (options.sign) headers['X-Device-Signature'] = await signRequest(db, method, path, text, now);
        try {
          return await api.request(method, path, text === '' ? { headers } : { body: text, headers }, check);
        } catch (err) {
          if (err instanceof ApiError && err.code === 'session_expired' && attempt === 0) {
            current = null;
            continue;
          }
          if (err instanceof ApiError && err.code === 'account_replaced') {
            const account = await readAccount(db);
            if (account?.state === 'registered') await markReplaced(db, account.accountId);
          }
          throw err;
        }
      }
    },
  };
}
