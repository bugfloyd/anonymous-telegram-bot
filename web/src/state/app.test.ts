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
const config = {
  apiUrl: 'https://api.example',
  botUsername: 'test_bot',
  appName: 'dev',
  issuer: { name: 'test', spki: new Uint8Array(0), keyId: new Uint8Array(32) },
};
const tick = () => new Promise((r) => setTimeout(r, 5));

type Outcome = 'ok' | ApiError | NetworkError;

/**
 * A fake server: accepts registrations and logins (or fails them as told),
 * stores link bundles exactly as clients send them, like the real one, and
 * keeps invitations for its one account.
 */
function fakeServer(outcomes: { register?: Outcome[]; session?: Outcome[]; createLink?: Outcome[]; elsewhere?: boolean } = {}) {
  const calls: string[] = [];
  const bundles = new Map<string, { signingKey: string; keys: unknown[]; mailbox: string }>();
  const unsigned: string[] = [];
  const state = {
    inviteOnly: false,
    member: false,
    admin: false,
    codes: new Map<string, { uses: number; left: number }>(),
    levels: new Map<number, number>(),
    granted: [] as { link: string; invitations: number }[],
  };
  let nextCode = 0;
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
      const needsSignature = () => {
        if (!signed) unsigned.push(`${method} ${path}`);
      };
      if (path === '/accounts/lookup') return check({ exists: outcomes.elsewhere === true });
      if (path === '/register') {
        take(outcomes.register);
        return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: 'v1.p.m', expires: Math.floor(Date.now() / 1000) + 900 });
      }
      if (path === '/session') {
        take(outcomes.session);
        return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: 'v1.p.m', expires: Math.floor(Date.now() / 1000) + 900 });
      }
      const canCreateLinks = !state.inviteOnly || state.member || state.admin;
      if (path === '/account') return check({ canCreateLinks, admin: state.admin, links: bundles.size, maxLinks: 5 });
      if (method === 'GET' && path === '/invitations') {
        const maxUses = state.admin ? 1000 : state.member ? 2 : 0;
        return check({
          inviteOnly: state.inviteOnly,
          member: state.member,
          admin: state.admin,
          level: state.member ? 1 : 0,
          unlimited: state.admin,
          remaining: state.admin ? 0 : maxUses,
          maxUses,
          codes: [...state.codes].map(([code, c]) => ({ code, ...c })),
        });
      }
      if (method === 'POST' && path === '/invitations') {
        needsSignature();
        const code = `aaaa-aaa${'abcdefghjk'[nextCode++]}`;
        state.codes.set(code, { uses: body.uses as number, left: body.uses as number });
        return check({ code });
      }
      if (path === '/invitations/redeem') {
        if (body.code === 'mmmm-nnnn') throw new ApiError(409, 'own_invitation');
        const c = state.codes.get(body.code as string);
        if (!c || c.left === 0) throw new ApiError(404, 'invalid_invitation');
        if (state.member) throw new ApiError(409, 'already_member');
        c.left--;
        state.member = true;
        return check({ level: 1 });
      }
      if (path === '/admin/levels' && method === 'GET') return check({ levels: [...state.levels].map(([level, invitations]) => ({ level, invitations })) });
      if (path === '/admin/levels' && method === 'PUT') {
        needsSignature();
        state.levels.set(body.level as number, body.invitations as number);
        return check({});
      }
      if (path === '/admin/grant') {
        needsSignature();
        if (!bundles.has(body.link as string)) throw new ApiError(404, 'not_found');
        state.granted.push({ link: body.link as string, invitations: body.invitations as number });
        return check({});
      }
      if (method === 'POST' && path === '/links') {
        needsSignature();
        take(outcomes.createLink);
        bundles.set(body.id as string, { signingKey: body.signingKey as string, keys: body.keys as unknown[], mailbox: 'MMMMMMMMMMMMMMMMMMMMMA' });
        return check({ id: body.id, mailbox: 'MMMMMMMMMMMMMMMMMMMMMA' });
      }
      const id = path.split('/')[2]!;
      if (method === 'GET' && bundles.has(id)) return check(bundles.get(id));
      if (method === 'DELETE' && bundles.has(id)) {
        needsSignature();
        bundles.delete(id);
        return check({});
      }
      throw new ApiError(404, 'not_found');
    },
  };
  return { api, calls, bundles, unsigned, state };
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
    background: false,
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
    // The first, plain launch asked whether the account lives elsewhere.
    expect(calls).toEqual(['POST /accounts/lookup', 'POST /register', 'GET /account', 'GET /invitations']);
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
    // First: the account is on another device. Starting over here is a choice, then typed.
    expect(app.screen.value).toEqual({ kind: 'elsewhere' });
    await app.startOver();
    expect(app.screen.value).toEqual({ kind: 'confirm-replace', busy: false });
    app.cancelReplace();
    expect(app.screen.value).toEqual({ kind: 'elsewhere' });
    await app.startOver();
    await app.confirmReplace();
    expect(app.screen.value.kind).toBe('home');
    expect(calls.filter((c) => c.includes('register'))).toEqual(['POST /register', 'POST /register?replace']);
  });

  it('says on a plain launch that the account is on another device, and can start over from there', async () => {
    const { api, calls } = fakeServer({ elsewhere: true, register: [new ApiError(409, 'account_exists')] });
    const db = await openDatabase(`app-test-${n++}`);
    const first = fakeBridge();
    const { app } = await setup({ api, bridge: first.bridge, launch: launchWith(''), db });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'elsewhere' });
    app.close();
    expect(first.posted.at(-1)?.type).toBe('web_app_close');

    // Starting over: setup reopens the app, and the existing account goes straight to the typed confirmation.
    await app.startOver();
    await tick();
    expect(app.screen.value.kind).toBe('reopening');
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    const { app: reopened } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
    await reopened.start();
    expect(reopened.screen.value).toEqual({ kind: 'confirm-replace', busy: false });
    await reopened.confirmReplace();
    expect(reopened.screen.value.kind).toBe('home');
    expect(calls.filter((c) => c.includes('register'))).toEqual(['POST /register', 'POST /register?replace']);
  });

  it("doesn't ask the server about launches that name a link", async () => {
    const { api, calls } = fakeServer({ elsewhere: true });
    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith('1' + 'A'.repeat(54)) });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'welcome', mismatch: false });
    expect(calls).toEqual([]);
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
    expect(sender.app.screen.value).toMatchObject({ kind: 'link', status: 'verified' });

    // The owner opening their own link is told so.
    const { app: ownerAgain } = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith(payloadOf(url)), db: owner.db });
    await ownerAgain.start();
    expect(ownerAgain.screen.value).toMatchObject({ kind: 'link', status: 'own' });
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
    expect(sender.app.screen.value).toMatchObject({ kind: 'link', status: 'mismatch' });
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
    expect(sender.app.screen.value).toMatchObject({ kind: 'link', status: 'not-found' });
  });

  it('says when an invitation is needed', async () => {
    const server = fakeServer({ createLink: [new ApiError(403, 'invitation_required')] });
    const owner = await registered(server.api);
    await owner.app.createLink();
    expect(home(owner.app.screen.value)).toMatchObject({ notice: 'invitation', links: [] });
    server.state.inviteOnly = true;
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
    expect(second.app.screen.value).toMatchObject({ kind: 'link', status: 'verified' });
    expect(await getMeta(db, 'pending-link')).toBeUndefined();
  });
});

describe('invitations', () => {
  it('lets an admin invite someone, who then joins through the link', async () => {
    const server = fakeServer();
    server.state.inviteOnly = true;
    server.state.admin = true;
    const admin = await registered(server.api);
    expect(home(admin.app.screen.value).invitations).toMatchObject({ admin: true, unlimited: true });

    await admin.app.invitations();
    expect(admin.app.screen.value.kind).toBe('invitations');
    await admin.app.createInvitation(3);
    const screen = admin.app.screen.value;
    if (screen.kind !== 'invitations') throw new Error(screen.kind);
    expect(screen.notice).toBe('created');
    expect(screen.created).toBe('aaaa-aaaa');
    expect(screen.info.codes[0]).toMatchObject({ code: 'aaaa-aaaa', uses: 3, left: 3, url: 'https://t.me/test_bot/dev?startapp=invaaaaaaaa' });
    expect(server.unsigned).toEqual([]);

    // Someone opens the invitation link (the fake has one account, so stop being admin).
    server.state.admin = false;
    const invitee = await registered(server.api, { launch: launchWith('invaaaaaaaa') });
    expect(home(invitee.app.screen.value)).toMatchObject({ notice: 'joined', account: { canCreateLinks: true } });
    expect(server.state.codes.get('aaaa-aaaa')!.left).toBe(2);
  });

  it('joins with a typed code, and explains a bad one', async () => {
    const server = fakeServer();
    server.state.inviteOnly = true;
    server.state.codes.set('abcd-efgh', { uses: 1, left: 1 });
    const user = await registered(server.api);
    expect(home(user.app.screen.value).invitations).toMatchObject({ member: false, inviteOnly: true });

    await user.app.redeem('nope');
    expect(home(user.app.screen.value).notice).toBe('invalid-invitation');
    await user.app.redeem(' ABCD EFGH ');
    expect(home(user.app.screen.value).notice).toBe('joined');
    await user.app.redeem('abcd-efgh');
    expect(home(user.app.screen.value).notice).toBe('invalid-invitation'); // used up
  });

  it("says when it's your own invitation code", async () => {
    const server = fakeServer();
    server.state.inviteOnly = true;
    const user = await registered(server.api);
    await user.app.redeem('mmmm-nnnn');
    expect(home(user.app.screen.value).notice).toBe('own-invitation');
  });

  it('remembers an invitation opened before setup', async () => {
    const server = fakeServer();
    server.state.inviteOnly = true;
    server.state.codes.set('abcd-efgh', { uses: 1, left: 1 });
    const db = await openDatabase(`app-test-${n++}`);
    const first = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith('invabcdefgh'), db });
    await first.app.start();
    await first.app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    const second = await setup({ api: server.api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
    await second.app.start();
    expect(home(second.app.screen.value).notice).toBe('joined');
  });

  it('lets admins set allowances and grant extra invitations by link', async () => {
    const server = fakeServer();
    server.state.inviteOnly = true;
    server.state.admin = true;
    const bridge = fakeBridge();
    const admin = await registered(server.api, { bridge: bridge.bridge });
    await admin.app.createLink();
    const memberLink = home(admin.app.screen.value).links[0]!;

    await admin.app.admin();
    expect(admin.app.screen.value.kind).toBe('admin');
    expect(bridge.posted.at(-1)).toEqual({ type: 'web_app_setup_back_button', data: { is_visible: true } });
    await admin.app.setLevel(1, 5);
    expect(admin.app.screen.value).toMatchObject({ kind: 'admin', notice: 'saved', levels: [{ level: 1, invitations: 5 }] });

    await admin.app.grant('not a link', 2);
    expect(admin.app.screen.value).toMatchObject({ notice: 'bad-link' });
    await admin.app.grant(memberLink.url, 2);
    expect(admin.app.screen.value).toMatchObject({ notice: 'granted' });
    expect(server.state.granted).toEqual([{ link: memberLink.id, invitations: 2 }]);
    expect(server.unsigned).toEqual([]);

    await admin.app.home();
    expect(bridge.posted.at(-1)).toEqual({ type: 'web_app_setup_back_button', data: { is_visible: false } });
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
