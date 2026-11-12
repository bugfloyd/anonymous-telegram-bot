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
import { keyName, listLinks, rotateLinkKey } from '../account/links';
import { countTokens, LOW_WATER, refillTokens, returnToken, takeToken, type StoredToken, type TokenIssuer } from '../account/wallet';
import { ApiError, checkObject, NetworkError, type Api } from '../net/api';
import type { Session } from '../net/session';
import { checkBundle, verifyBundle, type Bundle, type LinkRef } from '../protocol/link';
import { newId, openMessage, sealMessage, type Draft, type Message } from '../protocol/message';
import { getMeta, getSealed, putMeta, type ConversationRecord, type Database } from '../storage/db';
import { withLock } from '../sync/locks';
import {
  conversationKey,
  createConversationKey,
  deleteExpiredKeys,
  hasKey,
  hasLinkKey,
  indexKey,
  KEY_RETENTION_MS,
  keyFinder,
  lookupKey,
  scheduleKeyDeletion,
} from './keys';
import { POOL_LOW, poolSize, refillPool, takeMailbox } from './pool';
import { accountSecret, label, senderTag } from './secret';
import { addMessage, deleteMessages, getConversation, hasMessage, listMessages, markOwnSeen, putConversation, setMessageStatus } from './store';

const DAY = 24 * 3600 * 1000;
/** Messages sent longer ago than this, or later than now plus a day, are dropped (design §10). */
const MAX_AGE = 31 * DAY;
const MAX_INBOX_PAGES = 10;
const MAX_SEEN = 100;
const META_BLOCKED = 'blocked-tags';
/** Our conversation keys are replaced every 6 to 9 days (design D11), each conversation on its own schedule. */
const ROTATE_MIN = 6 * DAY;
const ROTATE_SPREAD = 3 * DAY;
/** How long a sender not answered yet trusts the link key it has before fetching the current one. */
const LINK_KEY_RECHECK = 3600 * 1000;

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
  const nextRotation = () => now() + ROTATE_MIN + Math.floor(Math.random() * ROTATE_SPREAD);

  /** Writes the first message to someone's link, starting a conversation. Returns its ID. */
  /**
   * Writes the first message to someone's link, starting a conversation.
   * onStored runs as soon as the conversation and message are stored, before
   * the (slower) delivery. Returns the conversation's ID once delivered.
   */
  async function startConversation(
    ref: LinkRef,
    bundle: Bundle,
    text: string,
    onStored?: (conv: string) => void | Promise<void>,
  ): Promise<string> {
    const { raw: linkKey } = await verifyBundle(ref, bundle, Math.floor(now() / 1000));
    const id = newId();
    const myKey = await createConversationKey(db, id);
    const secret = await accountSecret(db);
    const conversation: ConversationRecord = {
      id,
      role: 'sender',
      link: { id: ref.id, fingerprint: ref.fingerprint },
      label: await label(secret, 'link', utf8(ref.fingerprint)),
      peerKey: toBase64Url(linkKey),
      myKey: toBase64Url(myKey.publicKey),
      sendTo: bundle.mailbox,
      // Taken from the pool once the message shows.
      myMailbox: null,
      lastAt: now(),
      unread: 0,
      status: 'active',
      lastReceived: null,
      rotateAt: nextRotation(),
      linkCheckedAt: now(),
    };
    await putConversation(db, conversation);
    await write(conversation, text, undefined, () => onStored?.(id));
    return id;
  }

  /**
   * Writes a message in a conversation. onStored runs as soon as the message
   * is stored (shown as sending), before delivery.
   */
  async function send(conv: string, text: string, replyTo?: string, onStored?: () => void | Promise<void>): Promise<void> {
    const conversation = await getConversation(db, conv);
    if (!conversation) throw new Error('unknown conversation');
    await write(conversation, text, replyTo, onStored);
  }

  async function write(conversation: ConversationRecord, text: string, replyTo?: string, onStored?: () => void | Promise<void>): Promise<void> {
    // Stored and shown first; everything slow happens after.
    const id = newId();
    const at = now();
    await addMessage(db, { id, conv: conversation.id, at, out: true, status: 'sending' }, { text, ...(replyTo ? { replyTo } : {}) });
    await putConversation(db, { ...conversation, lastAt: at, hidden: false });
    await onStored?.();
    try {
      await transmit(conversation.id, { kind: 'message', id, at, text, ...(replyTo ? { replyTo } : {}) });
      // Unless their receipt came in first.
      await setMessageStatus(db, id, 'sent', 'sending');
    } catch (err) {
      await setMessageStatus(db, id, 'failed');
      throw err;
    }
  }

  /**
   * Seals and sends something in a conversation: a message, or a seen
   * receipt. One at a time per conversation, so the owner's first reply (or
   * first receipt) creates one key and takes one mailbox.
   */
  function transmit(conv: string, fields: Omit<Draft, 'conv' | 'reply' | 'last' | 'tag'>): Promise<void> {
    return withLock(`send:${conv}`, async () => {
      let c = await getConversation(db, conv);
      if (!c) throw new Error('unknown conversation');
      if (c.myKey === null || (c.lastReceived !== null && now() >= (c.rotateAt ?? 0))) {
        // A first key, or time for a new one (design D11). The new key goes
        // out with this message: nothing is sent just to rotate. Not before
        // they've written to us, since until then they know us by this key
        // alone. The old key stays until they've used the new one (receive).
        const key = await createConversationKey(db, c.id);
        c = { ...c, myKey: toBase64Url(key.publicKey), rotateAt: nextRotation() };
        await putConversation(db, c);
      }
      if (c.role === 'sender' && c.lastReceived === null && now() - (c.linkCheckedAt ?? 0) >= LINK_KEY_RECHECK) {
        c = await refreshLinkKey(c);
      }
      if (c.myMailbox === null) {
        c = { ...c, myMailbox: await takeMailbox(db, session) };
        await putConversation(db, c);
      }
      const myKey = await conversationKey(db, c.myKey!);
      const draft: Draft = { ...fields, conv: c.id, reply: c.myMailbox!, ...(c.lastReceived ? { last: c.lastReceived } : {}) };
      if (c.role === 'sender') draft.tag = toBase64Url(await senderTag(await accountSecret(db), c.link.fingerprint));
      // Only messages trigger a Telegram notice for the other side, not
      // receipts or key updates.
      await deliver(c.sendTo, await sealMessage(draft, myKey, fromBase64Url(c.peerKey)), fields.kind !== 'message');
    });
  }

  /**
   * Before they've answered, we write for their link's key. Links get a new
   * key weekly and old ones are deleted later, so fetch the current one if
   * ours is more than an hour old. If that fails, the one we have still
   * works for a while.
   */
  async function refreshLinkKey(c: ConversationRecord): Promise<ConversationRecord> {
    try {
      const bundle = await api.request('GET', `/links/${c.link.id}`, {}, checkBundle);
      const { raw } = await verifyBundle({ id: c.link.id, fingerprint: c.link.fingerprint }, bundle, Math.floor(now() / 1000));
      c = { ...c, peerKey: toBase64Url(raw), linkCheckedAt: now() };
      await putConversation(db, c);
    } catch {
      // Unreachable, replaced or revoked: keep what we have.
    }
    return c;
  }

  /**
   * Opening a conversation: tells the other side, at once, which of their
   * messages we have now read (design D12), and clears the unread count.
   */
  async function markSeen(conv: string): Promise<void> {
    const c = await getConversation(db, conv);
    if (!c) return;
    if (c.unread > 0) await putConversation(db, { ...c, unread: 0 });
    if (c.status !== 'active') return;
    const unseen = (await listMessages(db, conv)).filter((m) => !m.out && m.status === 'received').map((m) => m.id);
    for (let i = 0; i < unseen.length; i += MAX_SEEN) {
      const batch = unseen.slice(i, i + MAX_SEEN);
      // Left as received if this fails, so the next opening tries again.
      await transmit(conv, { kind: 'seen', id: newId(), at: now(), seen: batch });
      for (const id of batch) await setMessageStatus(db, id, 'read');
    }
  }

  /**
   * Blocks a conversation, silently (design §11): our mailbox for it is
   * discarded, so their sends go nowhere, and for someone who wrote to our
   * link their sender tag is remembered, so new conversations they start
   * through that link are dropped too. Nobody is told. The keys stay, so it
   * can be undone (unblock).
   */
  async function block(conv: string): Promise<void> {
    const c = await getConversation(db, conv);
    if (!c) return;
    if (c.myMailbox) {
      try {
        await session.call('POST', `/mailboxes/${c.myMailbox}/discard`, undefined, checkObject);
      } catch (err) {
        if (!(err instanceof ApiError && err.code === 'not_found')) throw err;
      }
    }
    if (c.role === 'owner' && c.tag) await addBlockedTag(c.link.fingerprint, c.tag);
    await putConversation(db, { ...c, status: 'blocked', unread: 0 });
  }

  /**
   * Undoes a block. Their sender tag is forgotten, so they can write through
   * our link again. Their app still writes to the mailbox we discarded, so
   * if we had one, we send them a quiet key update from a new mailbox (and,
   * if our key is gone, a new key): the conversation then carries on.
   * Whatever they sent while blocked is lost.
   */
  async function unblock(conv: string): Promise<void> {
    const c = await getConversation(db, conv);
    if (!c || c.status !== 'blocked') return;
    if (c.myMailbox !== null) {
      // Blocks used to delete the conversation's keys: then a new one.
      const keyKept = c.myKey !== null && (await hasKey(db, c.myKey));
      await putConversation(db, { ...c, myMailbox: null, myKey: keyKept ? c.myKey : null });
      // If this fails, it stays blocked, and unblocking can be tried again.
      await transmit(c.id, { kind: 'key_update', id: newId(), at: now() });
    }
    if (c.role === 'owner' && c.tag) await removeBlockedTag(c.link.fingerprint, c.tag);
    const updated = (await getConversation(db, conv))!;
    await putConversation(db, { ...updated, status: 'active' });
  }

  /**
   * Deletes a conversation from this device only: its messages go, and it
   * leaves the list. The other side keeps theirs and isn't told. Its keys
   * and mailbox stay, so if they write again it comes back, as in Telegram;
   * block() is for never hearing from them again.
   */
  async function remove(conv: string): Promise<void> {
    await withLock(`send:${conv}`, async () => {
      const c = await getConversation(db, conv);
      if (!c) return;
      await deleteMessages(db, conv);
      await putConversation(db, { ...c, unread: 0, hidden: true });
    });
  }

  async function blockedTags(): Promise<Set<string>> {
    const value = await getMeta<unknown>(db, META_BLOCKED);
    return new Set(Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string') : []);
  }

  function addBlockedTag(linkFingerprint: string, tag: string): Promise<void> {
    return withLock(META_BLOCKED, async () => {
      await putMeta(db, META_BLOCKED, [...(await blockedTags()), `${linkFingerprint}:${tag}`]);
    });
  }

  function removeBlockedTag(linkFingerprint: string, tag: string): Promise<void> {
    return withLock(META_BLOCKED, async () => {
      await putMeta(db, META_BLOCKED, [...(await blockedTags())].filter((t) => t !== `${linkFingerprint}:${tag}`));
    });
  }

  /** Sends an envelope to a mailbox with a token, without logging in. */
  async function deliver(mailbox: string, envelope: Uint8Array, quiet: boolean): Promise<void> {
    for (let attempt = 0; attempt < 2; attempt++) {
      const token = await nextToken();
      try {
        const body = { mailbox, envelope: toBase64Url(envelope), token: token.token, ...(quiet ? { quiet: true } : {}) };
        await api.post('/send', body, checkObject);
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

  /**
   * Fetches, opens and stores new messages and receipts, then confirms them.
   * Returns how many changed something shown: new messages, seen receipts
   * that marked ours, ends.
   */
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

  /** Handles one inbox item; returns whether it changed anything shown (a new message, a receipt, an end). */
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
    // Nothing more from a blocked conversation.
    if (conv?.status === 'blocked') return false;
    if (entry.link) {
      // Written for our link's key: a new conversation, or more from a sender
      // we haven't answered yet (whose key can't change meanwhile).
      if (conv && (conv.role !== 'owner' || conv.link.id !== entry.link.id || conv.peerKey !== message.from)) return false;
      if (!conv) {
        if (!message.reply) return false;
        // A new conversation from a person we blocked through this link.
        // By the link's fingerprint, which a replaced link keeps.
        if (message.tag && (await blockedTags()).has(`${entry.link.fingerprint}:${message.tag}`)) return false;
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
          ...(message.tag ? { tag: message.tag } : {}),
        };
      }
    } else if (entry.conv) {
      // Written for our conversation key, which only the other side knows.
      if (!conv || entry.conv !== message.conv) return false;
      if (entry.pub === conv.myKey) {
        // They have our current key, so they no longer write to older ones:
        // those go once anything written to them has expired from the inbox.
        const current = conv.myKey;
        await scheduleKeyDeletion(db, (e) => e.conv === conv!.id && e.pub !== current, now() + KEY_RETENTION_MS);
      }
    } else {
      return false;
    }

    // The sender's key may have rotated, and their mailbox changed.
    conv = { ...conv, peerKey: message.from, sendTo: message.reply ?? conv.sendTo, lastReceived: message.id };
    // Shown no later than it arrived, so a fast clock can't push a message above newer ones.
    const at = Math.min(message.at, t);
    let changed = false;
    if (message.kind === 'message' && message.text) {
      await addMessage(
        db,
        { id: message.id, conv: conv.id, at, out: false, status: 'received' },
        { text: message.text, ...(message.replyTo ? { replyTo: message.replyTo } : {}) },
      );
      conv.unread += 1;
      conv.lastAt = Math.max(conv.lastAt, at);
      // Something new brings a deleted conversation back.
      conv.hidden = false;
      changed = true;
    } else if (message.kind === 'seen') {
      // They opened these: our messages in this conversation only.
      for (const id of message.seen ?? []) if (await markOwnSeen(db, conv.id, id)) changed = true;
    } else if (message.kind === 'end') {
      changed = conv.status !== 'ended';
      conv.status = 'ended';
    }
    await putConversation(db, conv);
    return changed;
  }

  async function markRead(conv: string): Promise<void> {
    const c = await getConversation(db, conv);
    if (c && c.unread > 0) await putConversation(db, { ...c, unread: 0 });
  }

  /**
   * Upkeep, run at a random moment after the app opens: tops up the mailbox
   * pool and the token wallet, so neither is fetched right before a send;
   * deletes old keys whose time has come; gives the link a new key when due.
   */
  async function maintain(): Promise<void> {
    if ((await poolSize(db)) < POOL_LOW) await refillPool(db, session);
    if ((await countTokens(db, Math.floor(now() / 1000))) < LOW_WATER) {
      await refillTokens(db, session, issuer, undefined, Math.floor(now() / 1000));
    }
    // Deleting old keys needs no network, so it goes first.
    if ((await deleteExpiredKeys(db, now())) > 0) findKey.clear();
    await backfillLinkKeys();
    await rotateLinkKey(db, session, Math.floor(now() / 1000));
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

  return { startConversation, send, sync, markRead, markSeen, block, unblock, remove, maintain };
}

export type Messaging = ReturnType<typeof createMessaging>;
