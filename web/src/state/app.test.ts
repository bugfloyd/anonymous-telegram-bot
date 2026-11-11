import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { readAccount } from '../account/account';
import { ApiError, NetworkError, type Api } from '../net/api';
import { parseLaunch, type Launch } from '../platform/launch';
import type { Bridge, OutgoingEvents } from '../platform/telegram';
import { getMeta, openDatabase } from '../storage/db';
import type { SyncEvent } from '../sync/locks';
import { confirmationMatches, createApp, type Deps } from './app';

let n = 0;
const config = { apiUrl: 'https://api.example', botUsername: 'test_bot', appName: 'dev' };
const tick = () => new Promise((r) => setTimeout(r, 5));

type Outcome = 'ok' | ApiError | NetworkError;

function fakeApi(outcomes: { register?: Outcome[]; session?: Outcome[] }) {
  const calls: string[] = [];
  const api: Api = {
    async post(path, body, check) {
      calls.push(path + ((body as { replace?: boolean }).replace ? '?replace' : ''));
      const queue = path === '/register' ? outcomes.register : outcomes.session;
      const outcome = queue?.shift() ?? 'ok';
      if (outcome !== 'ok') throw outcome;
      return check({ account: 'BBBBBBBBBBBBBBBBBBBBBB', token: 'v1.p.m', expires: 1 });
    },
  };
  return { api, calls };
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

describe('app flow', () => {
  it('says so when the build has no server', async () => {
    const { app } = await setup({ config: null });
    await app.start();
    expect(app.screen.value.kind).toBe('unconfigured');
  });

  it('asks to open Telegram when set up outside it', async () => {
    const { app } = await setup({ api: fakeApi({}).api });
    await app.start();
    expect(app.screen.value.kind).toBe('outside-telegram');
  });

  it('sets up across two launches: reopen with the code, then register', async () => {
    const { api, calls } = fakeApi({});
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
    expect(reopened.screen.value).toEqual({ kind: 'ready', offline: false });
    expect(calls).toEqual(['/register']);
    expect(broadcasts).toContainEqual({ type: 'account-changed' });
    expect(second.posted.map((p) => p.type)).toContain('web_app_request_write_access');
    expect(await readAccount(db)).toMatchObject({ state: 'registered' });
  });

  it("doesn't ask for write access the user already gave", async () => {
    const { api } = fakeApi({});
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
    const { api, calls } = fakeApi({});
    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith('reg' + 'x'.repeat(43)) });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'welcome', mismatch: true });
    expect(calls).toEqual([]);
  });

  it('needs the typed confirmation to replace an existing account', async () => {
    const { api, calls } = fakeApi({ register: [new ApiError(409, 'account_exists')] });
    const db = await openDatabase(`app-test-${n++}`);
    await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db })).app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;

    const { app } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(code), db });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'confirm-replace', busy: false });
    await app.confirmReplace();
    expect(app.screen.value.kind).toBe('ready');
    expect(calls).toEqual(['/register', '/register?replace']);
  });

  it('reopens for fresh launch data when the old one expired', async () => {
    const { api } = fakeApi({ register: [new ApiError(401, 'init_data_rejected')] });
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
    const { api } = fakeApi({ session: ['ok', new NetworkError('down'), new ApiError(410, 'account_replaced')] });
    const db = await openDatabase(`app-test-${n++}`);
    await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db })).app.setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    await (await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(code), db })).app.start();

    const { app, broadcasts } = await setup({ api, bridge: fakeBridge().bridge, launch: launchWith(''), db });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'ready', offline: false });
    await app.start();
    expect(app.screen.value).toEqual({ kind: 'ready', offline: true });
    await app.start();
    expect(app.screen.value.kind).toBe('replaced');
    expect(broadcasts).toContainEqual({ type: 'account-changed' });
    // It stays replaced without asking the server again.
    await app.start();
    expect(app.screen.value.kind).toBe('replaced');
  });

  it('remembers a link opened before setup', async () => {
    const { app, db } = await setup({ api: fakeApi({}).api, bridge: fakeBridge().bridge, launch: launchWith('aBcDeFgHiJkLmNoPqRsTuV') });
    await app.start();
    expect(app.screen.value.kind).toBe('welcome');
    expect(await getMeta(db, 'pending-link')).toBe('aBcDeFgHiJkLmNoPqRsTuV');
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
