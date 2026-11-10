// On-device storage (IndexedDB). Never localStorage: it can't hold CryptoKeys
// and some webviews clear it on their own.
//
// CryptoKeys live in their own store under explicit string keys, with no
// indexes: a WebKit bug breaks index queries on records that contain a
// CryptoKey. Each schema change is a new version with its own upgrade step.

import { openDB, type DBSchema, type IDBPDatabase } from 'idb';

export const DB_NAME = 'incognitochat';
export const DB_VERSION = 1;

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
}

export type Database = IDBPDatabase<Schema>;

const upgrades: ((db: IDBPDatabase<Schema>) => void)[] = [
  // Version 1.
  (db) => {
    db.createObjectStore('keys');
    db.createObjectStore('vault');
    db.createObjectStore('meta');
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

export async function getMeta<T>(db: Database, name: string): Promise<T | undefined> {
  return (await db.get('meta', name)) as T | undefined;
}

export async function putMeta(db: Database, name: string, value: unknown): Promise<void> {
  await db.put('meta', value, name);
}

/** Asks the browser not to evict our storage. Best effort: often refused. */
export async function requestPersistence(): Promise<boolean> {
  try {
    return (await navigator.storage?.persist?.()) ?? false;
  } catch {
    return false;
  }
}
