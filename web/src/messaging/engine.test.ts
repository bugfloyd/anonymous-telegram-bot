import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { createLink } from '../account/links';
import type { TokenIssuer } from '../account/wallet';
import { i2osp, modPow, os2ip, type IssuerKey } from '../crypto/blindrsa';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { generateVaultKey } from '../crypto/vault';
import { ApiError, type Api } from '../net/api';
import type { Session } from '../net/session';
import { checkBundle, type LinkRef } from '../protocol/link';
import { countTokens, LOW_WATER } from '../account/wallet';
import { openDatabase, putKey, putMeta, type Database } from '../storage/db';
import { POOL_LOW, poolSize } from './pool';
import { createMessaging } from './engine';
import { listConversations, listMessages } from './store';

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
          const intro = rand();
          mailboxes.set(intro, account);
          bundles.set(b.id as string, { signingKey: b.signingKey, keys: b.keys, mailbox: intro });
          return check({ id: b.id, mailbox: intro });
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

async function person(backend: Awaited<ReturnType<typeof fakeBackend>>, account: string) {
  const db: Database = await openDatabase(`engine-test-${n++}`);
  await putKey(db, 'vault-key', await generateVaultKey());
  const session = backend.session(account);
  return { db, session, messaging: createMessaging({ db, api: backend.api, session, issuer: backend.issuer }) };
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
