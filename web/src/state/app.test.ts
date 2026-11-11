import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { readAccount } from '../account/account';
import { ApiError, NetworkError, type Api } from '../net/api';
import { parseLaunch, type Launch } from '../platform/launch';
import type { Bridge, OutgoingEvents } from '../platform/telegram';
import { getMeta, openDatabase } from '../storage/db';
import type { SyncEvent } from '../sync/locks';
import { confirmationMatches, createApp, type Deps, type Screen } from './app';

let n = 0;
const config = { apiUrl: 'https://api.example', botUsername: 'test_bot', appName: 'dev' };
const tick = () => new Promise((r) => setTimeout(r, 5));

type Outcome = 'ok' | ApiError | NetworkError;

/**
 * A fake server: accepts registrations and logins (or fails them as told),
 * and stores link bundles exactly as clients send them, like the real one.
 */
function fakeServer(outcomes: { register?: Outcome[]; session?: Outcome[]; createLink?: Outcome[] } = {}) {
  const calls: string[] = [];
  const bundles = new Map<string, { signingKey: string; keys: unknown[] }>();
  const unsigned: string[] = [];
  let canCreateLinks = true;
  const take = (queue: Outcome[] | undefined) => {
    const outcome = queue?.shift() ?? 'ok';
    if (outcome !== 'ok') throw outcome;
  };
  const api: Api = {
    post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
    async request(method, path, init, check) {
      const body = init.body ? (JSON.parse(init.body) as Record<string, unknown>) : {};
      calls.push(`${method} ${path}${body.replace ? '?replace' : ''}`);
      const signed = !!init.headers?.['X-Device-Signature'] && !!init.headers?.Authorization;
      if (path === '/register') {
        take(outcomes.register);
        return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: 'v1.p.m', expires: Math.floor(Date.now() / 1000) + 900 });
      }
      if (path === '/session') {
        take(outcomes.session);
        return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: 'v1.p.m', expires: Math.floor(Date.now() / 1000) + 900 });
      }
      if (path === '/account') return check({ canCreateLinks, admin: false, links: bundles.size, maxLinks: 5 });
      if (method === 'POST' && path === '/links') {
        if (!signed) unsigned.push(path);
        take(outcomes.createLink);
        bundles.set(body.id as string, { signingKey: body.signingKey as string, keys: body.keys as unknown[] });
        return check({ id: body.id });
      }
      const id = path.split('/')[2]!;
      if (method === 'GET' && bundles.has(id)) return check(bundles.get(id));
      if (method === 'DELETE' && bundles.has(id)) {
        if (!signed) unsigned.push(path);
        bundles.delete(id);
        return check({});
      }
      throw new ApiError(404, 'not_found');
    },
  };
  return {
    api,
    calls,
    bundles,
    unsigned,
    closeInvitations: () => (canCreateLinks = false),
  };
}

function fakeBridge() {
  const posted: { type: keyof OutgoingEvents; data: unknown }[] = [];
  const bridge: Bridge = {
    transport: 'webview-proxy',
    post: (type, ...data) => void posted.push({ type, data: data[0] }),
    on: () => () => {},
  };
  return { bridge, posted };
}

function launchWith(startParam: string, extra = ''): Launch {
  const initData = `auth_date=1&start_param=${startParam}&user=${encodeURIComponent(`{"id":1${extra}}`)}`;
  return parseLaunch('tgWebAppData=' + encodeURIComponent(initData));
}

async function setup(partial: Partial<Deps> & { api?: Api | null }) {
  const db = partial.db ?? (await openDatabase(`app-test-${n++}`));
  const broadcasts: SyncEvent[] = [];
  const app = createApp({
    config,
    api: null,
    launch: parseLaunch(''),
    bridge: null,
    broadcast: (e) => broadcasts.push(e),
    closeDelayMs: 0,
    ...partial,
    db,
  });
  return { app, db, broadcasts };
}

/** Sets up a registered device on a fresh database. */
async function registered(api: Api, extra: Partial<Deps> = {}) {
  const db = await openDatabase(`app-test-${n++}`);
  await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db })).app.setUp();
  const code = 'reg' + (await readAccount(db))!.fingerprint;
  const reg = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
  await reg.app.start();
  // Then a normal launch (or one opened from a link).
  const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db, ...extra });
  await app.start();
  return { app, db, code };
}

function home(screen: Screen) {
  if (screen.kind !== 'home') throw new Error(`expected home, got ${screen.kind}`);
  return screen;
}

function payloadOf(url: string): string {
  return new URL(url).searchParams.get('startapp')!;
}

describe('setup', () => {
  it('says so when the build has no server', async () => {
    const { app } = await setup({ config: null });
    await app.start();
    expect(app.screen.value.kind).toBe('unconfigured');
  });

  it('asks to open Telegram when set up outside it', async () => {
    const { app } = await setup({ api: fakeServer().api });
    await app.start();
    expect(app.screen.value.kind).toBe('outside-telegram');
  });

  it('sets up across two launches: reopen with the code, then register', async () => {
    const { api, calls } = fakeServer();
    const first = fakeBridge();
    const { app, db } = await setup({ api, bridge: first.bridge, launch: launchWith('') });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'welcome', mismatch: false });

    await app.setUp();
    await tick();
    expect(app.screen.value.kind).toBe('reopening');
    const open = first.posted.find((p) => p.type === 'web_app_open_tg_link');
    const code = (await readAccount(db))!.fingerprint;
    expect(open?.data).toEqual({ path_full: `/test_bot/dev?startapp=reg${code}` });
    expect(first.posted.at(-1)?.type).toBe('web_app_close');

    // The registration launch, a second copy sharing the same storage.
    const second = fakeBridge();
    const { app: reopened, broadcasts } = await setup({ api, bridge: second.bridge, launch: launchWith('reg' + code), db });
    await reopened.start();
    expect(home(reopened.screen.value)).toMatchObject({ offline: false, links: [], account: { canCreateLinks: true } });
    expect(calls).toEqual(['POST /register', 'GET /account']);
    expect(broadcasts).toContainEqual({ type: 'account-changed' });
    expect(second.posted.map((p) => p.type)).toContain('web_app_request_write_access');
    expect(await readAccount(db)).toMatchObject({ state: 'registered' });
  });

  it("doesn't ask for write access the user already gave", async () => {
    const { api } = fakeServer();
    const db = await openDatabase(`app-test-${n++}`);
    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db });
    await app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    const second = fakeBridge();
    const { app: reopened } = await setup({ api, bridge: second.bridge, launch: launchWith(code, ',"allows_write_to_pm":true'), db });
    await reopened.start();
    expect(second.posted.map((p) => p.type)).not.toContain('web_app_request_write_access');
  });

  it('refuses a registration launch for another setup', async () => {
    const { api, calls } = fakeServer();
    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith('reg' + 'x'.repeat(43)) });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'welcome', mismatch: true });
    expect(calls).toEqual([]);
  });

  it('needs the typed confirmation to replace an existing account', async () => {
    const { api, calls } = fakeServer({ register: [new ApiError(409, 'account_exists')] });
    const db = await openDatabase(`app-test-${n++}`);
    await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db })).app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;

    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'confirm-replace', busy: false });
    await app.confirmReplace();
    expect(app.screen.value.kind).toBe('home');
    expect(calls.filter((c) => c.includes('register'))).toEqual(['POST /register', 'POST /register?replace']);
  });

  it('reopens for fresh launch data when the old one expired', async () => {
    const { api } = fakeServer({ register: [new ApiError(401, 'init_data_rejected')] });
    const db = await openDatabase(`app-test-${n++}`);
    await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db })).app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;

    const bridge = fakeBridge();
    const { app } = await setup({ api, bridge: bridge.bridge, launch: launchWith(code), db });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'error', error: 'expired' });
    await app.retry();
    expect(app.screen.value.kind).toBe('reopening');
    // Same keys, same code.
    expect(bridge.posted.find((p) => p.type === 'web_app_open_tg_link')?.data).toEqual({ path_full: `/test_bot/dev?startapp=${code}` });
  });

  it('logs in when registered, and shows a replaced account loudly', async () => {
    // The first login is the one registered() makes.
    const { api } = fakeServer({ session: ['ok', 'ok', new NetworkError('down'), new ApiError(410, 'account_replaced')] });
    const { db } = await registered(api);

    const { app, broadcasts } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db });
    await app.start();
    expect(home(app.screen.value).offline).toBe(false);
    await app.start();
    expect(home(app.screen.value).offline).toBe(true);
    await app.start();
    expect(app.screen.value.kind).toBe('replaced');
    expect(broadcasts).toContainEqual({ type: 'account-changed' });
    // It stays replaced without asking the server again.
    await app.start();
    expect(app.screen.value.kind).toBe('replaced');
  });
});

describe('links', () => {
  it('creates a link with signed requests, and others can verify it', async () => {
    const server = fakeServer();
    const owner = await registered(server.api);
    await owner.app.createLink();
    const screen = home(owner.app.screen.value);
    expect(screen.notice).toBe('created');
    expect(screen.links).toHaveLength(1);
    const url = screen.links[0]!.url;
    expect(url).toMatch(/^https:\/\/t\.me\/test_bot\/dev\?startapp=1[A-Za-z0-9_-]{54}$/);
    expect(server.unsigned).toEqual([]);

    // Someone else opens it: the bundle must match the fingerprint in the link.
    const sender = await registered(server.api, { launch: launchWith(payloadOf(url)) });
    expect(sender.app.screen.value).toEqual({ kind: 'link', status: 'verified' });

    // The owner opening their own link is told so.
    const { app: ownerAgain } = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith(payloadOf(url)), db: owner.db });
    await ownerAgain.start();
    expect(ownerAgain.screen.value).toEqual({ kind: 'link', status: 'own' });
  });

  it('catches a server that swaps the keys', async () => {
    const server = fakeServer();
    const owner = await registered(server.api);
    await owner.app.createLink();
    const payload = payloadOf(home(owner.app.screen.value).links[0]!.url);

    // The server serves another link's bundle under this link's ID.
    const other = await registered(server.api);
    await other.app.createLink();
    const id = payload.slice(1, 12);
    const otherId = [...server.bundles.keys()].find((k) => k !== id)!;
    server.bundles.set(id, server.bundles.get(otherId)!);

    const sender = await registered(server.api, { launch: launchWith(payload) });
    expect(sender.app.screen.value).toEqual({ kind: 'link', status: 'mismatch' });
  });

  it('revokes a link, after which it is not found', async () => {
    const server = fakeServer();
    const owner = await registered(server.api);
    await owner.app.createLink();
    const link = home(owner.app.screen.value).links[0]!;
    await owner.app.revokeLink(link.id);
    expect(home(owner.app.screen.value)).toMatchObject({ notice: 'revoked', links: [] });
    expect(server.bundles.size).toBe(0);
    expect(server.unsigned).toEqual([]);

    const sender = await registered(server.api, { launch: launchWith(payloadOf(link.url)) });
    expect(sender.app.screen.value).toEqual({ kind: 'link', status: 'not-found' });
  });

  it('says when an invitation is needed', async () => {
    const server = fakeServer({ createLink: [new ApiError(403, 'invitation_required')] });
    const owner = await registered(server.api);
    await owner.app.createLink();
    expect(home(owner.app.screen.value)).toMatchObject({ notice: 'invitation', links: [] });
    server.closeInvitations();
    await owner.app.home();
    expect(home(owner.app.screen.value).account?.canCreateLinks).toBe(false);
  });

  it('continues to a link opened before setup', async () => {
    const server = fakeServer();
    const owner = await registered(server.api);
    await owner.app.createLink();
    const payload = payloadOf(home(owner.app.screen.value).links[0]!.url);

    // A new user opens the link, then sets up through the registration launch.
    const db = await openDatabase(`app-test-${n++}`);
    const first = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith(payload), db });
    await first.app.start();
    expect(first.app.screen.value.kind).toBe('welcome');
    expect(await getMeta(db, 'pending-link')).toBe(payload);
    await first.app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    const second = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
    await second.app.start();
    expect(second.app.screen.value).toEqual({ kind: 'link', status: 'verified' });
    expect(await getMeta(db, 'pending-link')).toBeUndefined();
  });
});

describe('typed confirmation', () => {
  it('matches ignoring case and extra spaces, in both languages', () => {
    expect(confirmationMatches('  Start   OVER ', 'start over')).toBe(true);
    expect(confirmationMatches('شروع  دوباره', 'شروع دوباره')).toBe(true);
    expect(confirmationMatches('start', 'start over')).toBe(false);
    expect(confirmationMatches('', '')).toBe(false);
  });
});
