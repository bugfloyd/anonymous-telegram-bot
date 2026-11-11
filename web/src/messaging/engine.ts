// Messaging (design §9, §10, §12): starting conversations through links,
// writing, and receiving.
//
// Who writes with which key (docs/protocol.md): a first message comes from a
// fresh conversation key of the sender and is written for the link's current
// encryption key; it carries the sender's reply mailbox and sender tag. The
// owner's first reply comes from a fresh conversation key of the owner and
// carries the owner's mailbox. From then on each side writes for the other's
// latest key, to the other's mailbox.
//
// Sends carry no login: they go to POST /send with an anonymous token.

import { fromBase64Url, toBase64Url, utf8 } from '../crypto/encoding';
import { keyName, listLinks } from '../account/links';
import { countTokens, LOW_WATER, refillTokens, returnToken, takeToken, type StoredToken, type TokenIssuer } from '../account/wallet';
import { ApiError, checkObject, NetworkError, type Api } from '../net/api';
import type { Session } from '../net/session';
import { checkBundle, verifyBundle, type Bundle, type LinkRef } from '../protocol/link';
import { newId, openMessage, sealMessage, type Draft, type Message } from '../protocol/message';
import { getSealed, type ConversationRecord, type Database } from '../storage/db';
import { withLock } from '../sync/locks';
import { conversationKey, createConversationKey, hasLinkKey, indexKey, keyFinder, lookupKey } from './keys';
import { POOL_LOW, poolSize, refillPool, takeMailbox } from './pool';
import { accountSecret, label, senderTag } from './secret';
import { addMessage, getConversation, hasMessage, putConversation, setMessageStatus } from './store';

const DAY = 24 * 3600 * 1000;
/** Messages sent longer ago than this, or later than now plus a day, are dropped (design §10). */
const MAX_AGE = 31 * DAY;
const MAX_INBOX_PAGES = 10;

export class NoTokens extends Error {
  constructor() {
    super('no send tokens');
    this.name = 'NoTokens';
  }
}

export interface MessagingDeps {
  db: Database;
  /** For calls without login: sending, reading link bundles. */
  api: Api;
  /** For logged-in calls: inbox, mailboxes, tokens. */
  session: Session;
  issuer: TokenIssuer;
  /** Unix milliseconds. */
  now?: () => number;
}

interface InboxItem {
  id: string;
  mailbox: string;
  envelope: string;
}

function checkInbox(value: unknown): { items: InboxItem[]; more: boolean } {
  const { items, more } = checkObject(value);
  if (!Array.isArray(items) || typeof more !== 'boolean') throw new Error('bad inbox');
  return {
    more,
    items: items.map((i: unknown) => {
      const { id, mailbox, envelope } = checkObject(i);
      if (typeof id !== 'string' || typeof mailbox !== 'string' || typeof envelope !== 'string') throw new Error('bad item');
      return { id, mailbox, envelope };
    }),
  };
}

export function createMessaging(deps: MessagingDeps) {
  const { db, api, session, issuer } = deps;
  const now = deps.now ?? Date.now;
  const findKey = keyFinder(db);

  /** Writes the first message to someone's link, starting a conversation. Returns its ID. */
  async function startConversation(ref: LinkRef, bundle: Bundle, text: string): Promise<string> {
    const { raw: linkKey } = await verifyBundle(ref, bundle, Math.floor(now() / 1000));
    const id = newId();
    const myKey = await createConversationKey(db, id);
    const myMailbox = await takeMailbox(db, session);
    const secret = await accountSecret(db);
    const conversation: ConversationRecord = {
      id,
      role: 'sender',
      link: { id: ref.id, fingerprint: ref.fingerprint },
      label: await label(secret, 'link', utf8(ref.fingerprint)),
      peerKey: toBase64Url(linkKey),
      myKey: toBase64Url(myKey.publicKey),
      sendTo: bundle.mailbox,
      myMailbox,
      lastAt: now(),
      unread: 0,
      status: 'active',
      lastReceived: null,
    };
    await putConversation(db, conversation);
    await write(conversation, text);
    return id;
  }

  /** Writes a message in a conversation. */
  async function send(conv: string, text: string, replyTo?: string): Promise<void> {
    const conversation = await getConversation(db, conv);
    if (!conversation) throw new Error('unknown conversation');
    await write(conversation, text, replyTo);
  }

  async function write(conversation: ConversationRecord, text: string, replyTo?: string): Promise<void> {
    let c = conversation;
    // The owner's first reply: our own key and mailbox for this conversation.
    if (c.myKey === null || c.myMailbox === null) {
      const key = await createConversationKey(db, c.id);
      c = { ...c, myKey: toBase64Url(key.publicKey), myMailbox: await takeMailbox(db, session) };
      await putConversation(db, c);
    }
    const myKey = await conversationKey(db, c.myKey!);
    const draft: Draft = {
      kind: 'message',
      conv: c.id,
      id: newId(),
      at: now(),
      text,
      reply: c.myMailbox!,
      ...(replyTo ? { replyTo } : {}),
      ...(c.lastReceived ? { last: c.lastReceived } : {}),
    };
    if (c.role === 'sender') draft.tag = toBase64Url(await senderTag(await accountSecret(db), c.link.fingerprint));
    const envelope = await sealMessage(draft, myKey, fromBase64Url(c.peerKey));
    await addMessage(db, { id: draft.id, conv: c.id, at: draft.at, out: true, status: 'sending' }, { text, ...(replyTo ? { replyTo } : {}) });
    await putConversation(db, { ...c, lastAt: draft.at });
    try {
      await deliver(c.sendTo, envelope);
      await setMessageStatus(db, draft.id, 'sent');
    } catch (err) {
      await setMessageStatus(db, draft.id, 'failed');
      throw err;
    }
  }

  /** Sends an envelope to a mailbox with a token, without logging in. */
  async function deliver(mailbox: string, envelope: Uint8Array): Promise<void> {
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = await nextToken();
      try {
        await api.post('/send', { mailbox, envelope: toBase64Url(envelope), token: token.token }, checkObject);
        return;
      } catch (err) {
        // A token the server refuses (spent, or too old) is dropped; try another.
        if (err instanceof ApiError && err.code === 'invalid_token' && attempt === 0) continue;
        // Not spent: a capped send, or one that may never have arrived.
        if ((err instanceof ApiError && err.code === 'rate_limited') || err instanceof NetworkError) {
          await returnToken(db, token.token, token.epoch);
        }
        throw err;
      }
    }
  }

  async function nextToken(): Promise<StoredToken> {
    let token = await takeToken(db, Math.floor(now() / 1000));
    if (!token) {
      // Out of tokens: fetching right before a send is a timing hint, but
      // the wallet is normally refilled well ahead (maintain()).
      try {
        await refillTokens(db, session, issuer, undefined, Math.floor(now() / 1000));
      } catch (err) {
        // The day's tokens are used up.
        if (err instanceof ApiError && err.code === 'rate_limited') throw new NoTokens();
        throw err;
      }
      token = await takeToken(db, Math.floor(now() / 1000));
    }
    if (!token) throw new NoTokens();
    return token;
  }

  /** Fetches, opens and stores new messages, then confirms them. Returns how many arrived. */
  function sync(): Promise<number> {
    return withLock('messaging-sync', async () => {
      let received = 0;
      for (let page = 0; page < MAX_INBOX_PAGES; page++) {
        const { items, more } = await session.call('GET', '/inbox', undefined, checkInbox);
        if (items.length === 0) break;
        for (const item of items) if (await receive(item)) received++;
        // Confirmed whether or not they opened: what we can't open now we never will.
        await session.call('POST', '/ack', { ids: items.map((i) => i.id) }, checkObject);
        if (!more) break;
      }
      return received;
    });
  }

  /** Handles one inbox item; returns whether it was a new message. */
  async function receive(item: InboxItem): Promise<boolean> {
    let message: Message;
    let keyId: Uint8Array;
    try {
      ({ message, keyId } = await openMessage(fromBase64Url(item.envelope), findKey));
    } catch {
      return false;
    }
    const entry = await lookupKey(db, keyId);
    const t = now();
    if (!entry || message.at < t - MAX_AGE || message.at > t + DAY) return false;
    if (await hasMessage(db, message.id)) return false; // a replay

    let conv = await getConversation(db, message.conv);
    if (entry.link) {
      // Written for our link's key: a new conversation, or more from a sender
      // we haven't answered yet (whose key can't change meanwhile).
      if (conv && (conv.role !== 'owner' || conv.link.id !== entry.link.id || conv.peerKey !== message.from)) return false;
      if (!conv) {
        if (!message.reply) return false;
        const secret = await accountSecret(db);
        conv = {
          id: message.conv,
          role: 'owner',
          link: entry.link,
          // Senders who don't send a tag (modified clients) are named by their key.
          label: await label(secret, 'anon', fromBase64Url(message.tag ?? message.from)),
          peerKey: message.from,
          myKey: null,
          sendTo: message.reply,
          myMailbox: null,
          lastAt: 0,
          unread: 0,
          status: 'active',
          lastReceived: null,
        };
      }
    } else if (entry.conv) {
      // Written for our conversation key, which only the other side knows.
      if (!conv || entry.conv !== message.conv) return false;
    } else {
      return false;
    }

    // The sender's key may have rotated, and their mailbox changed.
    conv = { ...conv, peerKey: message.from, sendTo: message.reply ?? conv.sendTo, lastReceived: message.id };
    // Shown no later than it arrived, so a fast clock can't push a message above newer ones.
    const at = Math.min(message.at, t);
    let isNew = false;
    if (message.kind === 'message' && message.text) {
      await addMessage(
        db,
        { id: message.id, conv: conv.id, at, out: false, status: 'received' },
        { text: message.text, ...(message.replyTo ? { replyTo: message.replyTo } : {}) },
      );
      conv.unread += 1;
      conv.lastAt = Math.max(conv.lastAt, at);
      isNew = true;
    } else if (message.kind === 'end') {
      conv.status = 'ended';
    }
    await putConversation(db, conv);
    return isNew;
  }

  async function markRead(conv: string): Promise<void> {
    const c = await getConversation(db, conv);
    if (c && c.unread > 0) await putConversation(db, { ...c, unread: 0 });
  }

  /**
   * Upkeep, run at a random moment after the app opens: tops up the mailbox
   * pool and the token wallet, so neither is fetched right before a send.
   */
  async function maintain(): Promise<void> {
    if ((await poolSize(db)) < POOL_LOW) await refillPool(db, session);
    if ((await countTokens(db, Math.floor(now() / 1000))) < LOW_WATER) {
      await refillTokens(db, session, issuer, undefined, Math.floor(now() / 1000));
    }
    await backfillLinkKeys();
  }

  /**
   * Links made before keys were indexed: match each one's sealed key to the
   * public key in its bundle, so first messages to it can be opened.
   */
  async function backfillLinkKeys(): Promise<void> {
    for (const link of await listLinks(db)) {
      if (await hasLinkKey(db, link.id)) continue;
      let bundle: Bundle;
      try {
        bundle = await api.request('GET', `/links/${link.id}`, {}, checkBundle);
      } catch {
        continue;
      }
      for (const k of bundle.keys) {
        const own = link.keys.find((mine) => mine.from === k.from);
        if (own && (await getSealed(db, keyName(link.id, k.from)))) {
          await indexKey(db, k.key, { name: keyName(link.id, k.from), link: { id: link.id, fingerprint: link.fingerprint } });
        }
      }
    }
  }

  return { startConversation, send, sync, markRead, maintain };
}

export type Messaging = ReturnType<typeof createMessaging>;
