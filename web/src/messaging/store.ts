// Conversations and history on this device (design D6: history lives on the
// device only). Message text is encrypted under the device's storage key, a
// non-extractable AES-GCM key, and bound to the message ID.

import { concat, randomBytes, utf8 } from '../crypto/encoding';
import type { Media, Sticker } from '../protocol/message';
import { getKey, putKey, type ConversationRecord, type Database, type MessageRecord } from '../storage/db';
import { withLock } from '../sync/locks';

const STORAGE_KEY = 'storage-key';
const IV = 12;

/** An attachment as kept with its message: no upload fields until it's uploaded (ours) or always (theirs). */
export type LocalMedia = Omit<Media, 'id' | 'k' | 'd'> & Partial<Pick<Media, 'id' | 'k' | 'd'>>;

/** One side's reaction to a message, and when it was set; an empty emoji is one taken back. */
export interface ReactionMark {
  e: string;
  at: number;
}

export interface MessageBody {
  text: string;
  replyTo?: string;
  /** An attachment: what it is and how to fetch it (the file itself is in the media store). */
  media?: LocalMedia;
  /** A sticker (then the text is empty). */
  sticker?: Sticker;
  /** Reactions, one per side: ours and theirs. */
  reactions?: { mine?: ReactionMark; theirs?: ReactionMark };
}

/** A message with its text, for showing. */
export interface MessageView extends Omit<MessageRecord, 'sealed'>, MessageBody {}

async function storageKey(db: Database): Promise<CryptoKey> {
  return withLock(STORAGE_KEY, async () => {
    const existing = await getKey(db, STORAGE_KEY);
    if (existing) return existing;
    const key = await crypto.subtle.generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt']);
    await putKey(db, STORAGE_KEY, key);
    return key;
  });
}

async function sealBody(db: Database, id: string, body: MessageBody): Promise<Uint8Array<ArrayBuffer>> {
  const iv = randomBytes(IV);
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: utf8(id) }, await storageKey(db), utf8(JSON.stringify(body)));
  return concat(iv, new Uint8Array(ct));
}

async function openBody(db: Database, id: string, sealed: Uint8Array<ArrayBuffer>): Promise<MessageBody> {
  const pt = await crypto.subtle.decrypt(
    { name: 'AES-GCM', iv: sealed.slice(0, IV), additionalData: utf8(id) },
    await storageKey(db),
    sealed.slice(IV),
  );
  return JSON.parse(new TextDecoder().decode(pt)) as MessageBody;
}

export function getConversation(db: Database, id: string): Promise<ConversationRecord | undefined> {
  return db.get('conversations', id);
}

export async function putConversation(db: Database, conversation: ConversationRecord): Promise<void> {
  await db.put('conversations', conversation);
}

/** Conversations, newest activity first. */
export async function listConversations(db: Database): Promise<ConversationRecord[]> {
  return (await db.getAll('conversations')).sort((a, b) => b.lastAt - a.lastAt);
}

export async function hasMessage(db: Database, id: string): Promise<boolean> {
  return (await db.getKey('messages', id)) !== undefined;
}

export async function addMessage(db: Database, record: Omit<MessageRecord, 'sealed'>, body: MessageBody): Promise<void> {
  await db.put('messages', { ...record, sealed: await sealBody(db, record.id, body) });
}

/** Sets a message's status; with from, only if it has that status now. */
export async function setMessageStatus(db: Database, id: string, status: MessageRecord['status'], from?: MessageRecord['status']): Promise<void> {
  const record = await db.get('messages', id);
  if (record && (from === undefined || record.status === from)) await db.put('messages', { ...record, status });
}

/** Deletes one message from this device, and its attachment. */
export async function deleteMessage(db: Database, id: string): Promise<void> {
  await db.delete('messages', id);
  await db.delete('media', id);
}

/** Keeps a message's attachment on this device, sealed under the storage key. */
export async function putMedia(db: Database, id: string, file: Uint8Array<ArrayBuffer>): Promise<void> {
  const iv = randomBytes(IV);
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv, additionalData: utf8(`media:${id}`) }, await storageKey(db), file);
  await db.put('media', concat(iv, new Uint8Array(ct)), id);
}

/** A message's attachment, if it's on this device. */
export async function getMedia(db: Database, id: string): Promise<Uint8Array<ArrayBuffer> | undefined> {
  const sealed = await db.get('media', id);
  if (!sealed) return undefined;
  const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: sealed.slice(0, IV), additionalData: utf8(`media:${id}`) }, await storageKey(db), sealed.slice(IV));
  return new Uint8Array(pt);
}

/** Sets where a message's attachment is. */
export async function setFile(db: Database, id: string, file: NonNullable<MessageRecord['file']>): Promise<void> {
  const record = await db.get('messages', id);
  if (record) await db.put('messages', { ...record, file });
}

/** The size of the attachments on this device, in bytes: in all, and by conversation. */
export async function mediaUsage(db: Database): Promise<{ total: number; byConversation: Record<string, number> }> {
  const byConversation: Record<string, number> = {};
  let total = 0;
  for (const record of await db.getAll('messages')) {
    if (record.file !== 'local') continue;
    const size = (await openBody(db, record.id, record.sealed)).media?.s ?? 0;
    byConversation[record.conv] = (byConversation[record.conv] ?? 0) + size;
    total += size;
  }
  return { total, byConversation };
}

/**
 * Removes attachments from this device and keeps their messages: those of
 * one conversation, or those older than a time, or all. Only uploaded ones
 * go, since they can be fetched again while the server keeps them; one of
 * ours still sending, or failed, stays. Returns the bytes freed.
 */
export async function clearMedia(db: Database, which: { conv?: string; before?: number } = {}): Promise<number> {
  let freed = 0;
  for (const record of await db.getAll('messages')) {
    if (record.file !== 'local') continue;
    if (which.conv !== undefined && record.conv !== which.conv) continue;
    if (which.before !== undefined && record.at >= which.before) continue;
    if (record.out && (record.status === 'sending' || record.status === 'failed')) continue;
    const { media } = await openBody(db, record.id, record.sealed);
    if (!media?.id || !media.k) continue;
    // The same lock as fetching, so a fetch can't put the file back halfway.
    freed += await withLock(`media:${record.id}`, async () => {
      const tx = db.transaction(['messages', 'media'], 'readwrite');
      const now = await tx.objectStore('messages').get(record.id);
      if (now?.file !== 'local') {
        await tx.done;
        return 0;
      }
      await tx.objectStore('media').delete(record.id);
      await tx.objectStore('messages').put({ ...now, file: 'cleared' });
      await tx.done;
      return media.s;
    });
  }
  return freed;
}

/**
 * Sets one side's reaction to a message (an empty emoji takes it back). One
 * set at an earlier time than the one there (a replay, or an old one
 * arriving late) changes nothing. Returns whether what's shown changed.
 */
export async function setReaction(db: Database, id: string, side: 'mine' | 'theirs', e: string, at: number): Promise<boolean> {
  return withLock(`message:${id}`, async () => {
    const message = await getMessage(db, id);
    if (!message) return false;
    const current = message.reactions?.[side];
    if (current && current.at >= at) return false;
    const body: MessageBody = {
      text: message.text,
      ...(message.replyTo ? { replyTo: message.replyTo } : {}),
      ...(message.media ? { media: message.media } : {}),
      ...(message.sticker ? { sticker: message.sticker } : {}),
      reactions: { ...message.reactions, [side]: { e, at } },
    };
    const sealed = await sealBody(db, id, body);
    const tx = db.transaction('messages', 'readwrite');
    const record = await tx.store.get(id);
    if (record) await tx.store.put({ ...record, sealed });
    await tx.done;
    return !!record && (current?.e ?? '') !== e;
  });
}

/** Marks one of our messages in a conversation as seen, if it was sent; returns whether it did. */
export async function markOwnSeen(db: Database, conv: string, id: string): Promise<boolean> {
  const record = await db.get('messages', id);
  if (!record || record.conv !== conv || !record.out || (record.status !== 'sent' && record.status !== 'sending')) return false;
  await db.put('messages', { ...record, status: 'seen' });
  return true;
}

/** One message with its text, if it's on this device. */
export async function getMessage(db: Database, id: string): Promise<MessageView | undefined> {
  const record = await db.get('messages', id);
  if (!record) return undefined;
  const { sealed, ...rest } = record;
  return { ...rest, ...(await openBody(db, id, sealed)) };
}

/** A conversation's messages, oldest first, with their text. */
export async function listMessages(db: Database, conv: string): Promise<MessageView[]> {
  const records = await db.getAllFromIndex('messages', 'byConversation', IDBKeyRange.bound([conv, 0], [conv, Number.MAX_SAFE_INTEGER]));
  return Promise.all(
    records.map(async ({ sealed, ...rest }) => ({ ...rest, ...(await openBody(db, rest.id, sealed)) })),
  );
}

/** Deletes a conversation's messages from this device. */
export async function deleteMessages(db: Database, conv: string): Promise<void> {
  const tx = db.transaction(['messages', 'media'], 'readwrite');
  const messages = tx.objectStore('messages');
  const range = IDBKeyRange.bound([conv, 0], [conv, Number.MAX_SAFE_INTEGER]);
  for (const id of await messages.index('byConversation').getAllKeys(range)) {
    await messages.delete(id);
    await tx.objectStore('media').delete(id);
  }
  await tx.done;
}
