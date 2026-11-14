import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { prepareSetup, readAccount, register } from '../account/account';
import { openDatabase } from '../storage/db';
import { ApiError, checkObject, type Api } from './api';
import { createSession } from './session';

let n = 0;
const NOW = 1_800_000_000;

/** A server that answers logins and records what each call carried. */
function server(options: { expireFirst?: boolean; replaced?: boolean } = {}) {
  const seen: { method: string; path: string; auth: string | undefined; signature: string | undefined }[] = [];
  let logins = 0;
  let expired = options.expireFirst ?? false;
  const api: Api = {
    post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
    async request(method, path, init, check) {
      if (path === '/register' || path === '/session') {
        if (path === '/session') logins++;
        return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: `v1.t${logins}.m`, expires: NOW + 900 });
      }
      seen.push({ method, path, auth: init.headers?.['X-Session'], signature: init.headers?.['X-Device-Signature'] });
      if (expired) {
        expired = false;
        throw new ApiError(401, 'session_expired');
      }
      if (options.replaced) throw new ApiError(410, 'account_replaced');
      return check({});
    },
  };
  return { api, seen, logins: () => logins };
}

async function registeredDb(api: Api) {
  const db = await openDatabase(`session-test-${n++}`);
  const code = await prepareSetup(db);
  await register(db, api, { initData: 'd', startParam: code }, { replace: false, now: () => NOW });
  return db;
}

describe('session', () => {
  it('logs in once and reuses the token', async () => {
    const s = server();
    const session = createSession(await registeredDb(s.api), s.api, () => NOW);
    await session.call('GET', '/account', undefined, checkObject);
    await session.call('GET', '/account', undefined, checkObject);
    expect(s.logins()).toBe(1);
    expect(s.seen.map((c) => c.auth)).toEqual(['v1.t1.m', 'v1.t1.m']);
    expect(s.seen[0]!.signature).toBeUndefined();
  });

  it('logs in again after an expired token, once', async () => {
    const s = server({ expireFirst: true });
    const session = createSession(await registeredDb(s.api), s.api, () => NOW);
    await session.call('GET', '/account', undefined, checkObject);
    expect(s.logins()).toBe(2);
    expect(s.seen.map((c) => c.auth)).toEqual(['v1.t1.m', 'v1.t2.m']);
  });

  it('signs sensitive requests with a fresh nonce each time', async () => {
    const s = server();
    const session = createSession(await registeredDb(s.api), s.api, () => NOW);
    await session.call('POST', '/links', { id: 'x' }, checkObject, { sign: true });
    await session.call('POST', '/links', { id: 'x' }, checkObject, { sign: true });
    const [a, b] = s.seen.map((c) => c.signature!);
    expect(a).toMatch(new RegExp(`^${NOW}\\.[A-Za-z0-9_-]{22}\\.[A-Za-z0-9_-]{86}$`));
    expect(a!.split('.')[1]).not.toBe(b!.split('.')[1]);
  });

  it('records a replaced account', async () => {
    const s = server({ replaced: true });
    const db = await registeredDb(s.api);
    const session = createSession(db, s.api, () => NOW);
    await expect(session.call('GET', '/account', undefined, checkObject)).rejects.toMatchObject({ code: 'account_replaced' });
    expect(await readAccount(db)).toMatchObject({ state: 'replaced' });
  });
});
