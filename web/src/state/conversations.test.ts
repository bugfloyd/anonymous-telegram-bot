import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { readAccount } from '../account/account';
import type { TokenIssuer } from '../account/wallet';
import { i2osp, modPow, os2ip, type IssuerKey } from '../crypto/blindrsa';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { ApiError, type Api } from '../net/api';
import { parseLaunch, type Launch } from '../platform/launch';
import type { Bridge } from '../platform/telegram';
import { listConversations } from '../messaging/store';
import { openDatabase } from '../storage/db';
import { createApp, type Screen } from './app';

let n = 0;
const rand = () => toBase64Url(crypto.getRandomValues(new Uint8Array(16)));
const config = {
  apiUrl: 'https://api.example',
  botUsername: 'test_bot',
  appName: 'dev',
  issuer: { name: 'test', spki: new Uint8Array(0), keyId: new Uint8Array(32) },
};

/** A backend with several accounts, the delivery rules, and a real token issuer. */
async function backend() {
  const pair = await crypto.subtle.generateKey(
    { name: 'RSA-PSS', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-384' },
    true,
    ['sign', 'verify'],
  );
  const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const big = (s: string) => os2ip(fromBase64Url(s));
  const key: IssuerKey = { key: pair.publicKey, n: big(jwk.n!), e: big(jwk.e!), k: 256 };
  const issuer: TokenIssuer = { key, keyId: new Uint8Array(32) };
  const d = big(jwk.d!);

  const owners = new Map<string, string>(); // mailbox -> account
  const inboxes = new Map<string, { id: string; mailbox: string; envelope: string }[]>();
  const bundles = new Map<string, Record<string, unknown>>();
  const state = { tokensLeft: Infinity };
  // What the app sent to /notices and /send, to check what it tells the server.
  const noticeSetups: string[] = [];
  const sends: Record<string, unknown>[] = [];
  const noticesOn = new Set<string>();

  const api: Api = {
    post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
    async request(method, path, init, check) {
      const body = init.body ? (JSON.parse(init.body) as Record<string, unknown>) : {};
      const auth = init.headers?.Authorization;
      const account = auth ? auth.split('.')[1]! : '';
      const token = (a: string) => ({ account: a, token: `v1.${a}.m`, expires: Math.floor(Date.now() / 1000) + 900 });
      if (path === '/register') {
        const a = rand();
        inboxes.set(a, []);
        return check(token(a));
      }
      if (path === '/session') return check(token(body.account as string));
      if (path === '/account') return check({ canCreateLinks: true, admin: false, links: 0, maxLinks: 5, notices: noticesOn.has(account) });
      if (path === '/notices') {
        noticeSetups.push(body.initData as string);
        noticesOn.add(account);
        return check({});
      }
      if (path === '/invitations') {
        return check({ inviteOnly: false, member: false, admin: false, level: 0, unlimited: false, remaining: 0, maxUses: 0, codes: [] });
      }
      if (method === 'POST' && path === '/links') {
        if (body.replaces) {
          const old = bundles.get(body.replaces as string);
          if (!old) throw new ApiError(404, 'not_found');
          bundles.delete(body.replaces as string);
          owners.delete(old.mailbox as string);
        }
        const intro = rand();
        owners.set(intro, account);
        bundles.set(body.id as string, { signingKey: body.signingKey, keys: body.keys, mailbox: intro });
        return check({ id: body.id, mailbox: intro });
      }
      if (method === 'GET' && path.startsWith('/links/')) {
        const b = bundles.get(path.split('/')[2]!);
        if (!b) throw new ApiError(404, 'not_found');
        return check(b);
      }
      if (path === '/mailboxes') {
        const ids = Array.from({ length: body.count as number }, rand);
        ids.forEach((id) => owners.set(id, account));
        return check({ mailboxes: ids });
      }
      if (path === '/tokens') {
        const blinded = body.blinded as string[];
        if (blinded.length > state.tokensLeft) throw new ApiError(429, 'rate_limited');
        state.tokensLeft -= blinded.length;
        return check({ signatures: blinded.map((b) => toBase64Url(i2osp(modPow(os2ip(fromBase64Url(b)), d, key.n), 256))) });
      }
      if (path === '/send') {
        if (auth) throw new Error('sends must not carry a login');
        sends.push(body);
        const owner = owners.get(body.mailbox as string);
        if (owner) inboxes.get(owner)!.push({ id: rand(), mailbox: body.mailbox as string, envelope: body.envelope as string });
        return check({});
      }
      if (path === '/inbox') return check({ items: [...inboxes.get(account)!], more: false });
      if (path === '/ack') {
        const done = new Set(body.ids as string[]);
        inboxes.set(account, inboxes.get(account)!.filter((i) => !done.has(i.id)));
        return check({});
      }
      throw new ApiError(404, 'not_found');
    },
  };
  return { api, issuer, state, noticeSetups, sends };
}

const bridge = (): Bridge => ({ transport: 'webview-proxy', post: () => {}, on: () => () => {} });

function launchWith(startParam: string, allowsWrite = true): Launch {
  const initData = `auth_date=1&start_param=${startParam}&user=${encodeURIComponent(`{"id":1,"allows_write_to_pm":${allowsWrite}}`)}`;
  return parseLaunch('tgWebAppData=' + encodeURIComponent(initData));
}

/** Sets up an account (two launches) and returns the app of the registration launch. */
async function person(b: Awaited<ReturnType<typeof backend>>, startParam = ''): Promise<ReturnType<typeof createApp> & { db: Awaited<ReturnType<typeof openDatabase>>; reopen: (startParam: string) => Promise<ReturnType<typeof createApp>> }> {
  const db = await openDatabase(`conv-test-${n++}`);
  const deps = { config, api: b.api, db, bridge: bridge(), broadcast: () => {}, closeDelayMs: 0, background: false, issuer: b.issuer };
  const first = createApp({ ...deps, launch: launchWith(startParam) });
  await first.start();
  await first.setUp();
  const code = 'reg' + (await readAccount(db))!.fingerprint;
  const app = createApp({ ...deps, launch: launchWith(code) });
  await app.start();
  const reopen = async (param: string) => {
    const next = createApp({ ...deps, launch: launchWith(param) });
    await next.start();
    return next;
  };
  return Object.assign(app, { db, reopen });
}

function screenOf<K extends Screen['kind']>(screen: Screen, kind: K): Extract<Screen, { kind: K }> {
  if (screen.kind !== kind) throw new Error(`expected ${kind}, got ${screen.kind}`);
  return screen as Extract<Screen, { kind: K }>;
}

describe('conversations in the app', () => {
  it('shows a message as sending before it is delivered', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    // Hold the send until we've looked at the screen.
    let release: () => void = () => {};
    const held = new Promise<void>((r) => (release = r));
    const post = b.api.post.bind(b.api);
    b.api.post = async (path, body, check) => {
      if (path === '/send') await held;
      return post(path, body, check);
    };
    const writing = sender.writeToLink('right away');
    await new Promise((r) => setTimeout(r, 300));
    expect(screenOf(sender.screen.value, 'conversation').messages.map((m) => [m.text, m.status])).toEqual([['right away', 'sending']]);
    release();
    await writing;
    expect(screenOf(sender.screen.value, 'conversation').messages.map((m) => m.status)).toEqual(['sent']);
  });

  it('lets someone write to a link, and the owner read and reply', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const url = screenOf(owner.screen.value, 'home').links[0]!.url;
    const payload = new URL(url).searchParams.get('startapp')!;

    // A new user opens the link, sets up, and lands on it, ready to write.
    const sender = await person(b, payload);
    expect(screenOf(sender.screen.value, 'link')).toMatchObject({ status: 'verified', canWrite: true });
    await sender.writeToLink('hello there');
    const conv = screenOf(sender.screen.value, 'conversation');
    expect(conv.messages.map((m) => [m.out, m.text, m.status])).toEqual([[true, 'hello there', 'sent']]);
    expect(conv.role).toBe('sender');

    // The owner's inbox fills the chat list.
    await owner.showTab('chats');
    await owner.sync();
    const home = screenOf(owner.screen.value, 'home');
    expect(home.conversations).toHaveLength(1);
    expect(home.conversations[0]).toMatchObject({ unread: 1, role: 'owner', last: { text: 'hello there', out: false } });

    await owner.openConversation(home.conversations[0]!.id);
    await owner.sendMessage('hi! who is this?');
    expect(screenOf(owner.screen.value, 'conversation').messages.map((m) => m.text)).toEqual(['hello there', 'hi! who is this?']);
    await owner.home();
    expect(screenOf(owner.screen.value, 'home').conversations[0]!.unread).toBe(0);

    // The sender's open conversation picks up the reply.
    await sender.sync();
    expect(screenOf(sender.screen.value, 'conversation').messages.map((m) => [m.out, m.text])).toEqual([
      [true, 'hello there'],
      [false, 'hi! who is this?'],
    ]);

    // Opening the link again continues the conversation instead of starting another.
    const reopened = await sender.reopen(payload);
    expect(screenOf(reopened.screen.value, 'conversation').id).toBe(conv.id);
    expect(await listConversations(sender.db)).toHaveLength(1);
  });

  it('sets up notices from plain launches only, and keeps seen receipts quiet', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    // The owner's registration launch set up notices, once.
    expect(b.noticeSetups).toHaveLength(1);
    expect(new URLSearchParams(b.noticeSetups[0]).get('start_param')).toMatch(/^reg/);
    await owner.home();
    expect(b.noticeSetups).toHaveLength(1);

    // Someone who arrives through a link never sends that launch's data.
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    const before = b.noticeSetups.length;
    await sender.reopen(payload);
    for (const initData of b.noticeSetups) expect(new URLSearchParams(initData).get('start_param') ?? '').not.toContain(payload);
    expect(b.noticeSetups.length).toBe(before);

    // Messages notify; seen receipts don't.
    await sender.writeToLink('hello');
    await owner.showTab('chats');
    await owner.sync();
    await owner.openConversation(screenOf(owner.screen.value, 'home').conversations[0]!.id);
    // The receipt goes in the background.
    for (let i = 0; i < 100 && b.sends.length < 2; i++) await new Promise((r) => setTimeout(r, 50));
    expect(b.sends.map((s) => s.quiet === true)).toEqual([false, true]);
  });

  it('asks before setting up notices when the bot may not write yet', async () => {
    const b = await backend();
    const db = await openDatabase(`conv-test-${n++}`);
    const handlers: Record<string, (data: unknown) => void> = {};
    const posted: string[] = [];
    const tg: Bridge = {
      transport: 'webview-proxy',
      post: (type: string) => void posted.push(type),
      on: (type: string, handler: (data: never) => void) => {
        handlers[type] = handler as (data: unknown) => void;
        return () => {};
      },
    } as Bridge;
    const deps = { config, api: b.api, db, bridge: tg, broadcast: () => {}, closeDelayMs: 0, background: false, issuer: b.issuer };
    await createApp({ ...deps, launch: launchWith('', false) }).setUp();
    const code = 'reg' + (await readAccount(db))!.fingerprint;
    const app = createApp({ ...deps, launch: launchWith(code, false) });
    await app.start();
    // Registering asks Telegram; until it's allowed, nothing is sent and home offers it.
    expect(posted).toContain('web_app_request_write_access');
    expect(b.noticeSetups).toHaveLength(0);
    expect(screenOf(app.screen.value, 'home').askNotices).toBe(true);
    handlers.write_access_requested!({ status: 'allowed' });
    for (let i = 0; i < 20 && b.noticeSetups.length === 0; i++) await new Promise((r) => setTimeout(r, 10));
    expect(b.noticeSetups).toHaveLength(1);
    expect(screenOf(app.screen.value, 'home').askNotices).toBe(false);
  });

  it('deletes a conversation from the list, and brings it back when they write again', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    await sender.writeToLink('hello');
    await owner.showTab('chats');
    await owner.sync();
    const id = screenOf(owner.screen.value, 'home').conversations[0]!.id;

    await owner.deleteConversation(id, false);
    const home = screenOf(owner.screen.value, 'home');
    expect(home.notice).toBe('deleted');
    expect(home.conversations).toEqual([]);

    // The sender still has theirs, and writing again brings it back with only the new message.
    await sender.sendMessage('anyone?');
    await owner.sync();
    const back = screenOf(owner.screen.value, 'home').conversations;
    expect(back.map((c) => [c.id, c.last?.text, c.unread])).toEqual([[id, 'anyone?', 1]]);
  });

  it('shows the delivery state of the last message in the list', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    await sender.writeToLink('hello');
    await sender.home();
    await sender.showTab('chats');
    expect(screenOf(sender.screen.value, 'home').conversations[0]!.last).toMatchObject({ out: true, status: 'sent' });
  });

  it('swaps the link for a new one; people who were answered carry on through it', async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const oldUrl = screenOf(owner.screen.value, 'home').links[0]!.url;
    const oldPayload = new URL(oldUrl).searchParams.get('startapp')!;
    const sender = await person(b, oldPayload);
    await sender.writeToLink('hello');
    await owner.showTab('chats');
    await owner.sync();
    await owner.openConversation(screenOf(owner.screen.value, 'home').conversations[0]!.id);
    await owner.sendMessage('hi back');
    await sender.sync();

    await owner.home();
    await owner.showTab('links');
    await owner.replaceLink();
    const home = screenOf(owner.screen.value, 'home');
    expect(home.notice).toBe('replaced');
    expect(home.links).toHaveLength(1);
    const newPayload = new URL(home.links[0]!.url).searchParams.get('startapp')!;
    expect(newPayload).not.toBe(oldPayload);

    // The owner opening their old link sees it's theirs.
    expect(screenOf((await owner.reopen(oldPayload)).screen.value, 'link').status).toBe('own');
    // Someone opening the old link: gone.
    const stranger = await person(b, oldPayload);
    expect(screenOf(stranger.screen.value, 'link').status).toBe('not-found');
    // The sender, already answered, opens the new link: the same conversation.
    const conv = (await listConversations(sender.db))[0]!.id;
    expect(screenOf((await sender.reopen(newPayload)).screen.value, 'conversation').id).toBe(conv);
  });

  it("updates the writer's open chat when the other side reads it", async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    await sender.writeToLink('hello');
    expect(screenOf(sender.screen.value, 'conversation').messages.map((m) => m.status)).toEqual(['sent']);
    await owner.showTab('chats');
    await owner.sync();
    await owner.openConversation(screenOf(owner.screen.value, 'home').conversations[0]!.id);
    // The receipt goes in the background; nothing else arrives with it.
    for (let i = 0; i < 100 && b.sends.length < 2; i++) await new Promise((r) => setTimeout(r, 50));
    await sender.sync();
    expect(screenOf(sender.screen.value, 'conversation').messages.map((m) => m.status)).toEqual(['seen']);
  });

  it("says when the day's tokens are used up", async () => {
    const b = await backend();
    const owner = await person(b);
    await owner.createLink();
    const payload = new URL(screenOf(owner.screen.value, 'home').links[0]!.url).searchParams.get('startapp')!;
    const sender = await person(b, payload);
    b.state.tokensLeft = 0;
    await sender.writeToLink('hello');
    // The conversation opens at once; the message shows as not sent, and why.
    const conv = screenOf(sender.screen.value, 'conversation');
    expect(conv.error).toBe('no-tokens');
    expect(conv.messages.map((m) => [m.text, m.status])).toEqual([['hello', 'failed']]);
  });
});
