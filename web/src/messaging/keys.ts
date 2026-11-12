// Which of our keys can open an envelope (design §6, §10). Incoming
// envelopes name a recipient key ID; this index maps key IDs to our private
// keys: link encryption keys (first messages) and conversation keys. Private
// keys stay sealed in the vault and are unsealed into non-extractable working
// copies when needed.

import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { AGREEMENT_IMPORT, exportPublicKey, generateCopyableAgreementKey } from '../crypto/keys';
import { seal, unseal } from '../crypto/vault';
import { keyId, type RecipientKey } from '../protocol/envelope';
import type { ConversationKey } from '../protocol/message';
import { deleteSealed, getKey, getMeta, getSealed, putMeta, putSealed, type Database } from '../storage/db';
import { withLock } from '../sync/locks';

const META_INDEX = 'recipient-keys';
const KEY_VAULT = 'vault-key';

export interface KeyEntry {
  /** Vault name of the private key. */
  name: string;
  /** Public key, base64url. */
  pub: string;
  /** For a link encryption key: the link's ID and fingerprint. */
  link?: { id: string; fingerprint: string };
  /** For a conversation key: the conversation's ID. */
  conv?: string;
  /**
   * Unix ms after which the private key is deleted (design D11): set once
   * nothing can still be written to it, plus the 30 days envelopes can wait
   * in the inbox, plus margin.
   */
  deleteAfter?: number;
}

/** How long a retired key is kept: undelivered envelopes expire after 30 days. */
export const KEY_RETENTION_MS = 35 * 24 * 3600 * 1000;

type Index = Record<string, KeyEntry>;

async function index(db: Database): Promise<Index> {
  return ((await getMeta<Index>(db, META_INDEX)) ?? {}) as Index;
}

/** Adds a key to the index. */
export function indexKey(db: Database, pub: Uint8Array, entry: Omit<KeyEntry, 'pub'>): Promise<void> {
  return withLock(META_INDEX, async () => {
    const id = toBase64Url(await keyId(pub));
    await putMeta(db, META_INDEX, { ...(await index(db)), [id]: { ...entry, pub: toBase64Url(pub) } });
  });
}

/** Removes keys from the index (their sealed copies are deleted separately). */
export function unindexKeys(db: Database, drop: (entry: KeyEntry) => boolean): Promise<void> {
  return withLock(META_INDEX, async () => {
    const kept = Object.entries(await index(db)).filter(([, e]) => !drop(e));
    await putMeta(db, META_INDEX, Object.fromEntries(kept));
  });
}

/** Schedules matching keys for deletion at `at` (Unix ms), unless already scheduled. */
export function scheduleKeyDeletion(db: Database, match: (entry: KeyEntry) => boolean, at: number): Promise<void> {
  return withLock(META_INDEX, async () => {
    const entries = Object.entries(await index(db));
    if (!entries.some(([, e]) => match(e) && e.deleteAfter === undefined)) return;
    await putMeta(
      db,
      META_INDEX,
      Object.fromEntries(entries.map(([id, e]) => [id, match(e) && e.deleteAfter === undefined ? { ...e, deleteAfter: at } : e])),
    );
  });
}

/** Deletes the private keys whose time has come, and forgets them. Returns how many. */
export function deleteExpiredKeys(db: Database, nowMs: number): Promise<number> {
  return withLock(META_INDEX, async () => {
    const entries = Object.entries(await index(db));
    const expired = entries.filter(([, e]) => e.deleteAfter !== undefined && e.deleteAfter <= nowMs);
    if (expired.length === 0) return 0;
    for (const [, e] of expired) await deleteSealed(db, e.name);
    await putMeta(db, META_INDEX, Object.fromEntries(entries.filter(([, e]) => !(e.deleteAfter !== undefined && e.deleteAfter <= nowMs))));
    return expired.length;
  });
}

/** Whether we still have the private key for this public key (base64url). */
export async function hasKey(db: Database, pub: string): Promise<boolean> {
  return (await index(db))[toBase64Url(await keyId(fromBase64Url(pub)))] !== undefined;
}

/** Whether any key of a link is indexed. */
export async function hasLinkKey(db: Database, linkId: string): Promise<boolean> {
  return Object.values(await index(db)).some((e) => e.link?.id === linkId);
}

export async function lookupKey(db: Database, id: Uint8Array): Promise<KeyEntry | undefined> {
  return (await index(db))[toBase64Url(id)];
}

/** Our key for a key ID, unsealed, if we have one. clear() drops the unsealed copies (after deletions). */
export function keyFinder(db: Database): ((id: Uint8Array) => Promise<RecipientKey | undefined>) & { clear(): void } {
  const cache = new Map<string, RecipientKey>();
  const find = async (id: Uint8Array) => {
    const k = toBase64Url(id);
    const cached = cache.get(k);
    if (cached) return cached;
    const entry = (await index(db))[k];
    if (!entry) return undefined;
    const key = await unsealKey(db, entry.name);
    const found = { privateKey: key, publicKey: fromBase64Url(entry.pub) };
    cache.set(k, found);
    return found;
  };
  return Object.assign(find, { clear: () => cache.clear() });
}

async function unsealKey(db: Database, name: string): Promise<CryptoKey> {
  const vaultKey = await getKey(db, KEY_VAULT);
  const sealed = await getSealed(db, name);
  if (!vaultKey || !sealed) throw new Error(`missing key ${name}`);
  return unseal(vaultKey, sealed, name, AGREEMENT_IMPORT, ['deriveBits']);
}

/** A new conversation key of ours: sealed in the vault and indexed. */
export async function createConversationKey(db: Database, conv: string): Promise<ConversationKey> {
  const vaultKey = await getKey(db, KEY_VAULT);
  if (!vaultKey) throw new Error('no vault key');
  const pair = await generateCopyableAgreementKey();
  const publicKey = await exportPublicKey(pair.publicKey);
  const name = `conv:${conv}:key:${toBase64Url(await keyId(publicKey))}`;
  await putSealed(db, name, await seal(vaultKey, pair.privateKey, name));
  await indexKey(db, publicKey, { name, conv });
  return { privateKey: await unsealKey(db, name), publicKey };
}

/** Our conversation key with this public key (to write with it again). */
export async function conversationKey(db: Database, pub: string): Promise<ConversationKey> {
  const entry = (await index(db))[toBase64Url(await keyId(fromBase64Url(pub)))];
  if (!entry) throw new Error('unknown conversation key');
  return { privateKey: await unsealKey(db, entry.name), publicKey: fromBase64Url(pub) };
}
