import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { createLink, listLinks, replaceLink } from '../account/links';
import type { TokenIssuer } from '../account/wallet';
import { i2osp, modPow, os2ip, type IssuerKey } from '../crypto/blindrsa';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { generateVaultKey } from '../crypto/vault';
import { ApiError, type Api } from '../net/api';
import type { Session } from '../net/session';
import { checkBundle, type LinkRef } from '../protocol/link';
import { countTokens, LOW_WATER } from '../account/wallet';
import { getMeta, getSealed, openDatabase, putKey, putMeta, type Database } from '../storage/db';
import { POOL_LOW, poolSize } from './pool';
import { createMessaging } from './engine';
import { getConversation, listConversations, listMessages } from './store';
import { KEY_RETENTION_MS, type KeyEntry } from './keys';

let n = 0;
const rand = () => toBase64Url(crypto.getRandomValues(new Uint8Array(16)));

/** A fake backend with the real one's delivery rules, and a real issuer key. */
async function fakeBackend() {
  const pair = await crypto.subtle.generateKey(
    { name: 'RSA-PSS', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-384' },
    true,
    ['sign', 'verify'],
  );
  const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
  const big = (s: string) => os2ip(fromBase64Url(s));
  const key: IssuerKey = { key: pair.publicKey, n: big(jwk.n!), e: big(jwk.e!), k: 256 };
  const issuer: TokenIssuer = { key, keyId: new Uint8Array(await crypto.subtle.digest('SHA-256', await crypto.subtle.exportKey('spki', pair.publicKey))) };
  const d = big(jwk.d!);

  const mailboxes = new Map<string, string>(); // mailbox -> owner account
  const inboxes = new Map<string, { id: string; mailbox: string; envelope: string }[]>();
  const bundles = new Map<string, Record<string, unknown>>();
  const spent = new Set<string>();
  const seenEnvelopes: string[] = [];
  const sendHeaders: (Record<string, string> | undefined)[] = [];

  const api: Api = {
    post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
    async request(method, path, init, check) {
      if (method === 'POST' && path === '/send') {
        sendHeaders.push(init.headers);
        const { mailbox, envelope, token } = JSON.parse(init.body!) as Record<string, string>;
        if (!token || spent.has(token)) throw new ApiError(401, 'invalid_token');
        spent.add(token);
        seenEnvelopes.push(envelope!);
        const owner = mailboxes.get(mailbox!);
        if (owner) inboxes.get(owner)!.push({ id: rand(), mailbox: mailbox!, envelope: envelope! });
        return check({});
      }
      if (method === 'GET' && path.startsWith('/links/')) {
        const b = bundles.get(path.split('/')[2]!);
        if (!b) throw new ApiError(404, 'not_found');
        return check(b);
      }
      throw new ApiError(404, 'not_found');
    },
  };

  /** A logged-in session for one account. */
  function session(account: string): Session {
    inboxes.set(account, []);
    return {
      use: () => {},
      async call(method, path, body, check) {
        const b = (body ?? {}) as Record<string, unknown>;
        if (path === '/mailboxes') {
          const ids = Array.from({ length: b.count as number }, rand);
          ids.forEach((id) => mailboxes.set(id, account));
          return check({ mailboxes: ids });
        }
        if (path === '/tokens') {
          return check({
            signatures: (b.blinded as string[]).map((x) => toBase64Url(i2osp(modPow(os2ip(fromBase64Url(x)), d, key.n), 256))),
          });
        }
        if (path === '/links') {
          if (b.replaces) {
            // The old link and its intro mailbox go.
            const old = bundles.get(b.replaces as string);
            if (!old) throw new ApiError(404, 'not_found');
            bundles.delete(b.replaces as string);
            mailboxes.delete(old.mailbox as string);
          }
          const intro = rand();
          mailboxes.set(intro, account);
          bundles.set(b.id as string, { signingKey: b.signingKey, keys: b.keys, mailbox: intro });
          return check({ id: b.id, mailbox: intro });
        }
        if (method === 'PUT' && path.startsWith('/links/') && path.endsWith('/keys')) {
          const bundle = bundles.get(path.split('/')[2]!);
          if (!bundle) throw new ApiError(404, 'not_found');
          bundle.keys = b.keys;
          return check({});
        }
        if (method === 'POST' && path.startsWith('/mailboxes/') && path.endsWith('/discard')) {
          const id = path.split('/')[2]!;
          if (mailboxes.get(id) !== account) throw new ApiError(404, 'not_found');
          mailboxes.delete(id);
          return check({});
        }
        if (method === 'GET' && path === '/inbox') return check({ items: [...inboxes.get(account)!], more: false });
        if (path === '/ack') {
          const done = new Set(b.ids as string[]);
          inboxes.set(account, inboxes.get(account)!.filter((i) => !done.has(i.id)));
          return check({});
        }
        throw new ApiError(404, 'not_found');
      },
    };
  }

  return { api, session, issuer, seenEnvelopes, sendHeaders, inboxes };
}

async function person(backend: Awaited<ReturnType<typeof fakeBackend>>, account: string, now?: () => number) {
  const db: Database = await openDatabase(`engine-test-${n++}`);
  await putKey(db, 'vault-key', await generateVaultKey());
  const session = backend.session(account);
  return { db, session, messaging: createMessaging({ db, api: backend.api, session, issuer: backend.issuer, ...(now ? { now } : {}) }) };
}

async function linkOf(backend: Awaited<ReturnType<typeof fakeBackend>>, owner: Awaited<ReturnType<typeof person>>) {
  const record = await createLink(owner.db, owner.session);
  const ref: LinkRef = { id: record.id, fingerprint: record.fingerprint };
  const bundle = await backend.api.request('GET', `/links/${record.id}`, {}, checkBundle);
  return { ref, bundle };
}

describe('messaging between two people', () => {
  it('starts through a link, then talks on per-conversation keys and mailboxes', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);

    const conv = await sender.messaging.startConversation(ref, bundle, 'سلام! hello');
    expect(await owner.messaging.sync()).toBe(1);

    const [ownerConv] = await listConversations(owner.db);
    expect(ownerConv).toMatchObject({ id: conv, role: 'owner', unread: 1, myKey: null, link: { id: ref.id } });
    expect(ownerConv!.label).toMatch(/^\S+ [0-9A-Z]{4}$/u);
    expect((await listMessages(owner.db, conv)).map((m) => [m.out, m.text])).toEqual([[false, 'سلام! hello']]);

    // The owner's first reply brings the owner's own key and mailbox.
    await owner.messaging.send(conv, 'hi back');
    expect(await sender.messaging.sync()).toBe(1);
    const [senderConv] = await listConversations(sender.db);
    const [ownerAfter] = await listConversations(owner.db);
    expect(senderConv!.peerKey).toBe(ownerAfter!.myKey);
    expect(senderConv!.sendTo).toBe(ownerAfter!.myMailbox);
    expect(senderConv!.sendTo).not.toBe(bundle.mailbox);

    await sender.messaging.send(conv, 'how are you?');
    expect(await owner.messaging.sync()).toBe(1);
    expect((await listMessages(owner.db, conv)).map((m) => m.text)).toEqual(['سلام! hello', 'hi back', 'how are you?']);
    expect((await listMessages(sender.db, conv)).map((m) => [m.out, m.status])).toEqual([
      [true, 'sent'],
      [false, 'received'],
      [true, 'sent'],
    ]);

    // The server only ever saw ciphertext, and sends carried no login.
    const plaintexts = ['hello', 'hi back', 'how are you'].map((s) => toBase64Url(new TextEncoder().encode(s)));
    for (const env of backend.seenEnvelopes) {
      const raw = new TextDecoder('latin1').decode(fromBase64Url(env));
      for (const p of ['hello', 'hi back', 'how are you']) expect(raw).not.toContain(p);
      for (const p of plaintexts) expect(env).not.toContain(p);
    }
    expect(backend.sendHeaders.every((h) => !h || !Object.keys(h).some((k) => k.toLowerCase() === 'authorization'))).toBe(true);
  });

  it('ignores replays, and keeps one label per person per link', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const other = await person(backend, 'other');
    const { ref, bundle } = await linkOf(backend, owner);

    const first = await sender.messaging.startConversation(ref, bundle, 'one');
    await owner.messaging.sync();
    // The same envelope delivered again is dropped.
    const again = backend.seenEnvelopes[0]!;
    backend.inboxes.get('owner')!.push({ id: rand(), mailbox: bundle.mailbox, envelope: again });
    expect(await owner.messaging.sync()).toBe(0);

    // The same sender starting a second conversation keeps their label; someone else doesn't share it.
    const second = await sender.messaging.startConversation(ref, bundle, 'two');
    await other.messaging.startConversation(ref, bundle, 'three');
    expect(await owner.messaging.sync()).toBe(2);
    const byId = new Map((await listConversations(owner.db)).map((c) => [c.id, c]));
    expect(byId.size).toBe(3);
    expect(byId.get(first)!.label).toBe(byId.get(second)!.label);
    const otherConv = [...byId.values()].find((c) => c.id !== first && c.id !== second)!;
    expect(otherConv.label).not.toBe(byId.get(first)!.label);
  });

  it('drops messages it cannot open or that claim another conversation', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    await sender.messaging.startConversation(ref, bundle, 'hello');
    // Garbage in the inbox is confirmed and dropped, the real message kept.
    backend.inboxes.get('owner')!.push({ id: rand(), mailbox: bundle.mailbox, envelope: toBase64Url(new Uint8Array(663)) });
    expect(await owner.messaging.sync()).toBe(1);
    expect(backend.inboxes.get('owner')).toHaveLength(0);
  });

  it('pays every send with a fresh token', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'a');
    await sender.messaging.send(conv, 'b');
    await sender.messaging.send(conv, 'c');
    expect(backend.seenEnvelopes).toHaveLength(3);
    expect(await owner.messaging.sync()).toBe(3);
  });

  it('keeps pools topped up, and indexes keys of links made before indexing', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    // Forget the link key's index entry, as for a link made by an older version.
    await putMeta(owner.db, 'recipient-keys', {});

    await owner.messaging.maintain();
    expect(await poolSize(owner.db)).toBeGreaterThanOrEqual(POOL_LOW);
    expect(await countTokens(owner.db)).toBeGreaterThanOrEqual(LOW_WATER);

    await sender.messaging.startConversation(ref, bundle, 'still reaches you');
    expect(await owner.messaging.sync()).toBe(1);
  });
});

describe('seen receipts', () => {
  it('tell the sender what was read, once', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'one');
    await sender.messaging.send(conv, 'two');
    await owner.messaging.sync();

    await owner.messaging.markSeen(conv);
    expect((await listMessages(owner.db, conv)).map((m) => m.status)).toEqual(['read', 'read']);
    const sends = backend.seenEnvelopes.length;
    await owner.messaging.markSeen(conv); // nothing new to report
    expect(backend.seenEnvelopes.length).toBe(sends);

    await sender.messaging.sync();
    expect((await listMessages(sender.db, conv)).map((m) => [m.text, m.status])).toEqual([
      ['one', 'seen'],
      ['two', 'seen'],
    ]);
  });
});

describe('blocks', () => {
  it('silence a person: this conversation, and new ones through the same link', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const other = await person(backend, 'other');
    const { ref, bundle } = await linkOf(backend, owner);

    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    await owner.messaging.send(conv, 'hi');
    await sender.messaging.sync();

    await owner.messaging.block(conv);
    // Their sends still look accepted, but nothing arrives.
    await sender.messaging.send(conv, 'are you there?');
    await sender.messaging.startConversation(ref, bundle, 'it is me again');
    expect(await owner.messaging.sync()).toBe(0);
    // Someone else still gets through.
    await other.messaging.startConversation(ref, bundle, 'hello from someone else');
    expect(await owner.messaging.sync()).toBe(1);
    const statuses = (await listConversations(owner.db)).map((c) => c.status).sort();
    expect(statuses).toEqual(['active', 'blocked']);
  });

  it('work before the owner ever replied', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    await owner.messaging.block(conv);
    // Still writing to the link's intro mailbox, which the owner can't discard.
    await sender.messaging.send(conv, 'more');
    expect(await owner.messaging.sync()).toBe(0);
    expect((await listMessages(owner.db, conv)).map((m) => m.text)).toEqual(['hello']);
  });

  it('let a sender block a conversation too', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    await sender.messaging.block(conv);
    await owner.messaging.send(conv, 'please reply');
    expect(await sender.messaging.sync()).toBe(0);
  });
});

describe('deleting a conversation', () => {
  it('removes it from this device only, until they write again', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const sender = await person(backend, 'sender');
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    await owner.messaging.send(conv, 'hi');
    await sender.messaging.sync();

    await owner.messaging.remove(conv);
    expect(await listMessages(owner.db, conv)).toEqual([]);
    expect((await listConversations(owner.db)).map((c) => [c.id, c.hidden, c.unread])).toEqual([[conv, true, 0]]);
    // The other side keeps theirs, and can still write: it comes back with only the new message.
    expect((await listMessages(sender.db, conv)).map((m) => m.text)).toEqual(['hello', 'hi']);
    await sender.messaging.send(conv, 'still there?');
    expect(await owner.messaging.sync()).toBe(1);
    expect((await listMessages(owner.db, conv)).map((m) => m.text)).toEqual(['still there?']);
    expect((await listConversations(owner.db))[0]!.hidden).toBe(false);
    // Replying works as before.
    await owner.messaging.send(conv, 'yes');
    expect(await sender.messaging.sync()).toBe(1);
  });
});

describe('a replaced link', () => {
  it('keeps its blocks and labels, and the old one stops working', async () => {
    const backend = await fakeBackend();
    const owner = await person(backend, 'owner');
    const blocked = await person(backend, 'blocked');
    const friend = await person(backend, 'friend');
    const old = await linkOf(backend, owner);
    const spam = await blocked.messaging.startConversation(old.ref, old.bundle, 'spam');
    await friend.messaging.startConversation(old.ref, old.bundle, 'hi, it is me');
    expect(await owner.messaging.sync()).toBe(2);
    await owner.messaging.block(spam);
    const labelOf = async (text: string) => {
      for (const c of await listConversations(owner.db)) {
        if ((await listMessages(owner.db, c.id)).some((m) => m.text === text)) return c.label;
      }
      return null;
    };
    const friendLabel = await labelOf('hi, it is me');

    const record = await replaceLink(owner.db, owner.session);
    expect(record.fingerprint).toBe(old.ref.fingerprint);
    expect(record.id).not.toBe(old.ref.id);
    expect((await listLinks(owner.db)).map((l) => l.id)).toEqual([record.id]);
    await expect(backend.api.request('GET', `/links/${old.ref.id}`, {}, checkBundle)).rejects.toThrow();
    const next = { ref: { id: record.id, fingerprint: record.fingerprint }, bundle: await backend.api.request('GET', `/links/${record.id}`, {}, checkBundle) };

    // The blocked person writes through the new link: dropped.
    await blocked.messaging.startConversation(next.ref, next.bundle, 'remember me?');
    expect(await owner.messaging.sync()).toBe(0);
    // The friend starts again through the new link: the same label as before.
    await friend.messaging.startConversation(next.ref, next.bundle, 'new link, same me');
    expect(await owner.messaging.sync()).toBe(1);
    expect(await labelOf('new link, same me')).toBe(friendLabel);
  });
});

describe('key rotation', () => {
  const DAY = 24 * 3600 * 1000;
  const convKeys = async (db: Database, conv: string) =>
    Object.values((await getMeta<Record<string, KeyEntry>>(db, 'recipient-keys')) ?? {}).filter((e) => e.conv === conv);
  const linkKeys = async (db: Database, link: string) =>
    Object.values((await getMeta<Record<string, KeyEntry>>(db, 'recipient-keys')) ?? {}).filter((e) => e.link?.id === link);

  it('replaces our conversation key weekly with the next message, and deletes the old one once they use the new', async () => {
    const backend = await fakeBackend();
    let clock = Date.now();
    const now = () => clock;
    const owner = await person(backend, 'owner', now);
    const sender = await person(backend, 'sender', now);
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    await owner.messaging.send(conv, 'hi');
    await sender.messaging.sync();
    const first = (await getConversation(owner.db, conv))!.myKey!;

    // Before it's due: the same key.
    clock += 5 * DAY;
    await owner.messaging.send(conv, 'still the same key');
    expect((await getConversation(owner.db, conv))!.myKey).toBe(first);

    // Due: the next message carries a new key; nothing extra is sent.
    clock += 5 * DAY;
    const sendsBefore = backend.seenEnvelopes.length;
    await owner.messaging.send(conv, 'a new key comes with this');
    expect(backend.seenEnvelopes.length).toBe(sendsBefore + 1);
    const second = (await getConversation(owner.db, conv))!.myKey!;
    expect(second).not.toBe(first);
    expect(await sender.messaging.sync()).toBe(2);
    expect((await getConversation(sender.db, conv))!.peerKey).toBe(second);

    // The old key stays while they haven't written to the new one...
    expect((await convKeys(owner.db, conv)).map((k) => [k.pub, k.deleteAfter])).toEqual([
      [first, undefined],
      [second, undefined],
    ]);
    clock += 40 * DAY;
    await owner.messaging.maintain();
    expect((await convKeys(owner.db, conv)).length).toBe(2);

    // ...and goes 35 days after they have.
    await sender.messaging.send(conv, 'got your new key');
    expect(await owner.messaging.sync()).toBe(1);
    expect((await convKeys(owner.db, conv)).find((k) => k.pub === first)!.deleteAfter).toBe(clock + KEY_RETENTION_MS);
    clock += 34 * DAY;
    await owner.messaging.maintain();
    expect((await convKeys(owner.db, conv)).length).toBe(2);
    clock += 2 * DAY;
    await owner.messaging.maintain();
    expect((await convKeys(owner.db, conv)).map((k) => k.pub)).toEqual([second]);
    expect(await getSealed(owner.db, (await convKeys(owner.db, conv))[0]!.name)).toBeDefined();
    // Still talking.
    await sender.messaging.send(conv, 'still here');
    expect(await owner.messaging.sync()).toBe(1);
  });

  it("doesn't replace a sender's key before the owner has answered", async () => {
    const backend = await fakeBackend();
    let clock = Date.now();
    const owner = await person(backend, 'owner', () => clock);
    const sender = await person(backend, 'sender', () => clock);
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await sender.messaging.startConversation(ref, bundle, 'hello');
    const key = (await getConversation(sender.db, conv))!.myKey;
    clock += 10 * DAY;
    await sender.messaging.send(conv, 'anyone?');
    expect((await getConversation(sender.db, conv))!.myKey).toBe(key);
    expect(await owner.messaging.sync()).toBe(2);
  });

  it("replaces the link's key weekly; unanswered senders follow it; the old one goes after 35 days", async () => {
    const backend = await fakeBackend();
    let clock = Date.now();
    const now = () => clock;
    const owner = await person(backend, 'owner', now);
    const early = await person(backend, 'early', now);
    const later = await person(backend, 'later', now);
    const { ref, bundle } = await linkOf(backend, owner);
    const conv = await early.messaging.startConversation(ref, bundle, 'hello');
    await owner.messaging.sync();
    const firstKey = bundle.keys[0]!.key;

    await owner.messaging.maintain();
    expect((await linkKeys(owner.db, ref.id)).length).toBe(1);
    clock += 10 * DAY;
    await owner.messaging.maintain();
    const rotated = await backend.api.request('GET', `/links/${ref.id}`, {}, checkBundle);
    expect(rotated.keys).toHaveLength(1);
    expect(toBase64Url(rotated.keys[0]!.key)).not.toBe(toBase64Url(firstKey));
    const keys = await linkKeys(owner.db, ref.id);
    expect(keys.map((k) => k.deleteAfter !== undefined)).toEqual([true, false]);

    // Not answered yet: the early sender fetches the new key before writing again.
    await early.messaging.send(conv, 'me again');
    expect((await getConversation(early.db, conv))!.peerKey).toBe(toBase64Url(rotated.keys[0]!.key));
    // Someone new writes to the new key.
    await later.messaging.startConversation(ref, rotated, 'hi from later');
    expect(await owner.messaging.sync()).toBe(2);

    // 36 days on: the first key is deleted (and the link has moved on again).
    const firstName = keys[0]!.name;
    clock += 36 * DAY;
    await owner.messaging.maintain();
    const after = await linkKeys(owner.db, ref.id);
    expect(after.map((k) => k.pub)).not.toContain(toBase64Url(firstKey));
    expect(await getSealed(owner.db, firstName)).toBeUndefined();
    expect(after.map((k) => k.deleteAfter !== undefined)).toEqual([true, false]);
  });
});
