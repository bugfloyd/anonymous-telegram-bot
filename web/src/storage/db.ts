// On-device storage (IndexedDB). Never localStorage: it can't hold CryptoKeys
// and some webviews clear it on their own.
//
// CryptoKeys live in their own store under explicit string keys, with no
// indexes: a WebKit bug breaks index queries on records that contain a
// CryptoKey. Each schema change is a new version with its own upgrade step.

import { openDB, type DBSchema, type IDBPDatabase } from 'idb';

export const DB_NAME = 'incognitochat';
export const DB_VERSION = 2;

export interface StoredKeyPair {
  privateKey: CryptoKey;
  publicKey: CryptoKey;
}

interface Schema extends DBSchema {
  /** CryptoKeys and key pairs: device keys, the vault key, working copies. */
  keys: { key: string; value: CryptoKey | StoredKeyPair };
  /** Sealed copyable keys (see crypto/vault.ts). */
  vault: { key: string; value: Uint8Array<ArrayBuffer> };
  /** Small app state: account and device IDs, settings. */
  meta: { key: string; value: unknown };
  /** Conversations (messaging/store.ts). */
  conversations: { key: string; value: ConversationRecord };
  /** Message history, text encrypted under the device's storage key (messaging/store.ts). */
  messages: { key: string; value: MessageRecord; indexes: { byConversation: [string, number] } };
}

/** A conversation, as kept on the device. */
export interface ConversationRecord {
  id: string;
  /** owner: someone wrote to our link; sender: we wrote to someone's link. */
  role: 'owner' | 'sender';
  /** The link the conversation came through. */
  link: { id: string; fingerprint: string };
  label: string;
  /** The other side's current public key (65 bytes, base64url): for a sender before any reply, the link's key. */
  peerKey: string;
  /** Our current conversation public key; null for an owner who hasn't replied yet. */
  myKey: string | null;
  /** The mailbox we write to. */
  sendTo: string;
  /** The mailbox they write to us at; null for an owner who hasn't replied yet (they write to the link). */
  myMailbox: string | null;
  /** Unix ms of the latest message, for ordering. */
  lastAt: number;
  unread: number;
  /** blocked: our mailbox is discarded and anything more from them is dropped. */
  status: 'active' | 'ended' | 'blocked';
  /** For an owner: the sender's tag (base64url), to block the person and not just this conversation. */
  tag?: string;
  /** The ID of the last message received from them. */
  lastReceived: string | null;
  /** Deleted on this device: out of the list, with no messages, until a new one comes or goes. */
  hidden?: boolean;
  /** Unix ms when our conversation key is next replaced (weekly, jittered per conversation). */
  rotateAt?: number;
  /** For a sender not answered yet: Unix ms when the link's current key was last fetched. */
  linkCheckedAt?: number;
}

/** A message, as kept on the device. Only its text is sealed; times and status are local metadata. */
export interface MessageRecord {
  id: string;
  conv: string;
  /** Unix ms shown: the sender's time, but never later than when it arrived. */
  at: number;
  out: boolean;
  /** Ours: sending, sent, failed, then seen once they open it. Theirs: received, then read once we told them. */
  status: 'sending' | 'sent' | 'failed' | 'seen' | 'received' | 'read';
  /** AES-GCM under the storage key: nonce ‖ ciphertext of {text, replyTo}. */
  sealed: Uint8Array<ArrayBuffer>;
}

export type Database = IDBPDatabase<Schema>;

const upgrades: ((db: IDBPDatabase<Schema>) => void)[] = [
  // Version 1.
  (db) => {
    db.createObjectStore('keys');
    db.createObjectStore('vault');
    db.createObjectStore('meta');
  },
  // Version 2: messaging.
  (db) => {
    db.createObjectStore('conversations', { keyPath: 'id' });
    const messages = db.createObjectStore('messages', { keyPath: 'id' });
    messages.createIndex('byConversation', ['conv', 'at']);
  },
];

/** Opens (and if needed creates or upgrades) the app's database. */
export function openDatabase(name = DB_NAME): Promise<Database> {
  return openDB<Schema>(name, DB_VERSION, {
    upgrade(db, oldVersion) {
      for (let v = oldVersion; v < DB_VERSION; v++) upgrades[v]?.(db);
    },
    // Another copy of the app is running an older version: let it close.
    blocking() {
      location.reload();
    },
  });
}

export async function getKey(db: Database, name: string): Promise<CryptoKey | undefined> {
  const value = await db.get('keys', name);
  return value instanceof CryptoKey ? value : undefined;
}

export async function getKeyPair(db: Database, name: string): Promise<StoredKeyPair | undefined> {
  const value = await db.get('keys', name);
  return value && !(value instanceof CryptoKey) ? value : undefined;
}

export async function putKey(db: Database, name: string, key: CryptoKey | StoredKeyPair): Promise<void> {
  await db.put('keys', key, name);
}

export async function getSealed(db: Database, name: string): Promise<Uint8Array<ArrayBuffer> | undefined> {
  return db.get('vault', name);
}

export async function putSealed(db: Database, name: string, sealed: Uint8Array<ArrayBuffer>): Promise<void> {
  await db.put('vault', sealed, name);
}

export async function deleteSealed(db: Database, name: string): Promise<void> {
  await db.delete('vault', name);
}

export async function getMeta<T>(db: Database, name: string): Promise<T | undefined> {
  return (await db.get('meta', name)) as T | undefined;
}

export async function putMeta(db: Database, name: string, value: unknown): Promise<void> {
  await db.put('meta', value, name);
}

export async function deleteMeta(db: Database, name: string): Promise<void> {
  await db.delete('meta', name);
}

/**
 * Deletes everything stored for the previous account on this device: keys,
 * sealed keys, conversations, messages and settings, except the settings
 * named in keep. A new account must not inherit any of it: the server
 * deleted that account's mailboxes and links.
 */
export async function wipe(db: Database, keep: string[] = []): Promise<void> {
  const tx = db.transaction(['keys', 'vault', 'meta', 'conversations', 'messages'], 'readwrite');
  await Promise.all([tx.objectStore('keys').clear(), tx.objectStore('vault').clear(), tx.objectStore('conversations').clear(), tx.objectStore('messages').clear()]);
  const meta = tx.objectStore('meta');
  for (const name of await meta.getAllKeys()) {
    if (!keep.includes(String(name))) await meta.delete(name);
  }
  await tx.done;
}

/** Asks the browser not to evict our storage. Best effort: often refused. */
export async function requestPersistence(): Promise<boolean> {
  try {
    return (await navigator.storage?.persist?.()) ?? false;
  } catch {
    return false;
  }
}
