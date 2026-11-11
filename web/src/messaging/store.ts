// Conversations and history on this device (design D6: history lives on the
// device only). Message text is encrypted under the device's storage key, a
// non-extractable AES-GCM key, and bound to the message ID.

import { concat, randomBytes, utf8 } from '../crypto/encoding';
import { getKey, putKey, type ConversationRecord, type Database, type MessageRecord } from '../storage/db';
import { withLock } from '../sync/locks';

const STORAGE_KEY = 'storage-key';
const IV = 12;

export interface MessageBody {
  text: string;
  replyTo?: string;
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

export async function setMessageStatus(db: Database, id: string, status: MessageRecord['status']): Promise<void> {
  const record = await db.get('messages', id);
  if (record) await db.put('messages', { ...record, status });
}

/** A conversation's messages, oldest first, with their text. */
export async function listMessages(db: Database, conv: string): Promise<MessageView[]> {
  const records = await db.getAllFromIndex('messages', 'byConversation', IDBKeyRange.bound([conv, 0], [conv, Number.MAX_SAFE_INTEGER]));
  return Promise.all(
    records.map(async ({ sealed, ...rest }) => ({ ...rest, ...(await openBody(db, rest.id, sealed)) })),
  );
}
