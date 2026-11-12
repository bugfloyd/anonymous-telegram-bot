// This account's link (design §8). An account has one link at a time, which
// it can replace: the new link has a new address (ID) and new encryption
// keys, but the same signing key, whose fingerprint is in the link. Senders'
// tags are made from that fingerprint, so after a replacement the same
// people get the same labels, and blocked people stay blocked. Keys are
// generated here and sealed in the vault (they are copied to new devices
// later).

import {
  exportPublicKey,
  fingerprint,
  generateCopyableAgreementKey,
  generateCopyableSigningKey,
  importSigningPublicKey,
  SIGNING_IMPORT,
} from '../crypto/keys';
import { LABEL_LINK_KEY, sign } from '../crypto/signing';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { seal, unseal } from '../crypto/vault';
import { ApiError, checkObject } from '../net/api';
import type { Session } from '../net/session';
import { hourFloor } from '../protocol/device-list';
import { KEY_VALIDITY_SECONDS, keyParts, newLinkId, type LinkRef } from '../protocol/link';
import { deleteSealed, getKey, getMeta, getSealed, putMeta, putSealed, type Database } from '../storage/db';
import { withLock } from '../sync/locks';
import { indexKey, KEY_RETENTION_MS, scheduleKeyDeletion, unindexKeys } from '../messaging/keys';

const META_LINKS = 'links';
/** The account's link signing key (sealed), and its public key (meta). */
const SIGNING = 'link-signing';
const META_SIGNING_PUBLIC = 'link-signing-public';
const KEY_VAULT = 'vault-key';
const LOCK = 'links';
const ID_ATTEMPTS = 3;
const ID = /^[A-Za-z0-9_-]{11}$/;
const FP = /^[A-Za-z0-9_-]{43}$/;

export interface LinkRecord extends LinkRef {
  /** The encryption keys sealed for this link: validity window and public key (base64url). */
  keys: { from: number; until: number; pub?: string }[];
  /** Unix seconds when the link next gets a new encryption key (weekly, jittered). */
  rotateAt?: number;
}

/** Link encryption keys are replaced every 6 to 9 days (design D11). */
const ROTATE_MIN = 6 * 24 * 3600;
const ROTATE_SPREAD = 3 * 24 * 3600;
const nextRotation = (now: number) => now + ROTATE_MIN + Math.floor(Math.random() * ROTATE_SPREAD);

/** What the server says about this account's links. */
export interface AccountInfo {
  canCreateLinks: boolean;
  admin: boolean;
  links: number;
  maxLinks: number;
  /** Whether Telegram notices are set up and not turned off. */
  notices: boolean;
}

export function checkAccountInfo(value: unknown): AccountInfo {
  const v = checkObject(value);
  const { canCreateLinks, admin, links, maxLinks, notices } = v;
  if (typeof canCreateLinks !== 'boolean' || typeof admin !== 'boolean') throw new Error('bad account');
  if (!Number.isSafeInteger(links) || !Number.isSafeInteger(maxLinks)) throw new Error('bad account');
  return { canCreateLinks, admin, links: links as number, maxLinks: maxLinks as number, notices: notices === true };
}

/** Vault names for a link's keys (signingName: links made when each had its own signing key). */
const signingName = (id: string) => `link:${id}:signing`;
export const keyName = (id: string, from: number) => `link:${id}:key:${from}`;

export async function listLinks(db: Database): Promise<LinkRecord[]> {
  const value = await getMeta<unknown>(db, META_LINKS);
  if (!Array.isArray(value)) return [];
  return value.filter(
    (r): r is LinkRecord =>
      !!r &&
      typeof r === 'object' &&
      ID.test((r as LinkRecord).id) &&
      FP.test((r as LinkRecord).fingerprint) &&
      Array.isArray((r as LinkRecord).keys),
  );
}

/** The account's link signing key, created with its first link and kept for every link after it. */
async function linkSigningKey(db: Database, vaultKey: CryptoKey): Promise<{ privateKey: CryptoKey; publicKey: Uint8Array<ArrayBuffer>; fp: string }> {
  const sealed = await getSealed(db, SIGNING);
  const pub = await getMeta<unknown>(db, META_SIGNING_PUBLIC);
  if (sealed && typeof pub === 'string') {
    const publicKey = fromBase64Url(pub);
    return {
      privateKey: await unseal(vaultKey, sealed, SIGNING, SIGNING_IMPORT, ['sign']),
      publicKey,
      fp: await fingerprint(await importSigningPublicKey(publicKey)),
    };
  }
  const pair = await generateCopyableSigningKey();
  const publicKey = await exportPublicKey(pair.publicKey);
  await putSealed(db, SIGNING, await seal(vaultKey, pair.privateKey, SIGNING));
  await putMeta(db, META_SIGNING_PUBLIC, toBase64Url(publicKey));
  return { privateKey: pair.privateKey, publicKey, fp: await fingerprint(pair.publicKey) };
}

/** Creates this account's link. Throws ApiError invitation_required, or link_limit if it has one. */
export function createLink(db: Database, session: Session, now: () => number = () => Math.floor(Date.now() / 1000)): Promise<LinkRecord> {
  return withLock(LOCK, () => makeLink(db, session, now, null));
}

/**
 * Replaces this account's link with a new one (or creates one if there is
 * none): the old link stops working at once. Conversations carry on, since
 * they no longer go through the link.
 */
export function replaceLink(db: Database, session: Session, now: () => number = () => Math.floor(Date.now() / 1000)): Promise<LinkRecord> {
  return withLock(LOCK, async () => {
    const links = await listLinks(db);
    const current = links[links.length - 1];
    // Links from when an account could have several: only one stays.
    for (const extra of links.slice(0, -1)) await revoke(db, session, extra.id);
    if (!current) return makeLink(db, session, now, null);
    try {
      return await makeLink(db, session, now, current);
    } catch (err) {
      if (!(err instanceof ApiError && err.code === 'not_found')) throw err;
      // Gone already (revoked elsewhere): just make a new one.
      await revoke(db, session, current.id);
      return makeLink(db, session, now, null);
    }
  });
}

/** New encryption keys, sealed here first, then the bundle on the server (in place of replaces, if set). */
async function makeLink(db: Database, session: Session, now: () => number, replaces: LinkRecord | null): Promise<LinkRecord> {
  const vaultKey = await getKey(db, KEY_VAULT);
  if (!vaultKey) throw new Error('no vault key');
  const signing = await linkSigningKey(db, vaultKey);
  const encryption = await generateCopyableAgreementKey();
  const encryptionKey = await exportPublicKey(encryption.publicKey);
  // Back-date by a random few hours, so the keys of two links made around
  // the same time don't carry the same times.
  const from = hourFloor(now()) - 3600 * Math.floor(Math.random() * 12);
  const until = from + KEY_VALIDITY_SECONDS;
  const signature = await sign(signing.privateKey, LABEL_LINK_KEY, ...keyParts(encryptionKey, from, until));

  for (let attempt = 1; ; attempt++) {
    const id = newLinkId();
    // Sealed before the server knows the link, so a link never exists
    // without its keys on this device.
    await putSealed(db, keyName(id, from), await seal(vaultKey, encryption.privateKey, keyName(id, from)));
    try {
      await session.call(
        'POST',
        '/links',
        {
          id,
          signingKey: toBase64Url(signing.publicKey),
          keys: [{ key: toBase64Url(encryptionKey), from, until, signature: toBase64Url(signature) }],
          ...(replaces ? { replaces: replaces.id } : {}),
        },
        checkObject,
        { sign: true },
      );
    } catch (err) {
      await deleteSealed(db, keyName(id, from));
      if (err instanceof ApiError && err.code === 'conflict' && attempt < ID_ATTEMPTS) continue;
      throw err;
    }
    const record: LinkRecord = {
      id,
      fingerprint: signing.fp,
      keys: [{ from, until, pub: toBase64Url(encryptionKey) }],
      rotateAt: nextRotation(now()),
    };
    await putMeta(db, META_LINKS, [...(await listLinks(db)).filter((l) => l.id !== replaces?.id), record]);
    if (replaces) {
      // Its keys stay until first messages sent before the replacement can
      // no longer be waiting in the inbox.
      await scheduleKeyDeletion(db, (e) => e.link?.id === replaces.id, now() * 1000 + KEY_RETENTION_MS);
    }
    // First messages to the link are wrapped to this key.
    await indexKey(db, encryptionKey, { name: keyName(id, from), link: { id, fingerprint: signing.fp } });
    return record;
  }
}

/**
 * Gives the link a new encryption key when its turn comes (design D11). The
 * bundle then lists only the new key, so new first messages go to it; the
 * old key is kept until anything written to it has expired from the inbox.
 * Senders not answered yet fetch the current key before writing again. Run
 * at upkeep; `now` in Unix seconds. Returns whether it rotated.
 */
export function rotateLinkKey(db: Database, session: Session, now: number): Promise<boolean> {
  return withLock(LOCK, async () => {
    const links = await listLinks(db);
    const link = links[links.length - 1];
    if (!link) return false;
    const save = (changed: LinkRecord) => putMeta(db, META_LINKS, links.map((l) => (l.id === changed.id ? changed : l)));
    if (link.rotateAt === undefined) {
      // A link from before rotation: start its schedule.
      await save({ ...link, rotateAt: nextRotation(now) });
      return false;
    }
    if (now < link.rotateAt) return false;
    const vaultKey = await getKey(db, KEY_VAULT);
    if (!vaultKey) return false;
    const signingKey = await signingKeyFor(db, vaultKey, link);
    if (!signingKey) return false;

    const encryption = await generateCopyableAgreementKey();
    const encryptionKey = await exportPublicKey(encryption.publicKey);
    // Back-dated like a new link's, but never to before the key it replaces,
    // since senders take the newest.
    const newest = Math.max(...link.keys.map((k) => k.from));
    const from = Math.min(hourFloor(now), Math.max(hourFloor(now) - 3600 * Math.floor(Math.random() * 12), newest + 3600));
    const until = from + KEY_VALIDITY_SECONDS;
    const signature = await sign(signingKey, LABEL_LINK_KEY, ...keyParts(encryptionKey, from, until));
    const name = keyName(link.id, from);
    await putSealed(db, name, await seal(vaultKey, encryption.privateKey, name));
    try {
      await session.call(
        'PUT',
        `/links/${link.id}/keys`,
        { keys: [{ key: toBase64Url(encryptionKey), from, until, signature: toBase64Url(signature) }] },
        checkObject,
        { sign: true },
      );
    } catch (err) {
      await deleteSealed(db, name);
      // Gone (replaced or revoked elsewhere): nothing to rotate.
      if (err instanceof ApiError && err.code === 'not_found') return false;
      throw err;
    }
    await indexKey(db, encryptionKey, { name, link: { id: link.id, fingerprint: link.fingerprint } });
    await scheduleKeyDeletion(db, (e) => e.link?.id === link.id && e.name !== name, now * 1000 + KEY_RETENTION_MS);
    await save({
      ...link,
      // Keys deleted long ago drop off the record.
      keys: [...link.keys.filter((k) => k.until + KEY_RETENTION_MS / 1000 > now), { from, until, pub: toBase64Url(encryptionKey) }],
      rotateAt: nextRotation(now),
    });
    return true;
  });
}

/** The private key that signs this link's encryption keys. */
async function signingKeyFor(db: Database, vaultKey: CryptoKey, link: LinkRecord): Promise<CryptoKey | null> {
  const account = await linkSigningKey(db, vaultKey);
  if (account.fp === link.fingerprint) return account.privateKey;
  // A link made when each had its own signing key.
  const sealed = await getSealed(db, signingName(link.id));
  return sealed ? unseal(vaultKey, sealed, signingName(link.id), SIGNING_IMPORT, ['sign']) : null;
}

/**
 * Revokes a link and deletes its keys. A link the server no longer has counts
 * as revoked. The signing key stays, for the next link.
 */
export function revokeLink(db: Database, session: Session, id: string): Promise<void> {
  return withLock(LOCK, () => revoke(db, session, id));
}

async function revoke(db: Database, session: Session, id: string): Promise<void> {
  try {
    await session.call('DELETE', `/links/${id}`, undefined, checkObject, { sign: true });
  } catch (err) {
    if (!(err instanceof ApiError && err.code === 'not_found')) throw err;
  }
  const links = await listLinks(db);
  const record = links.find((l) => l.id === id);
  await putMeta(db, META_LINKS, links.filter((l) => l.id !== id));
  await unindexKeys(db, (e) => e.link?.id === id);
  // Links made when each had its own signing key.
  await deleteSealed(db, signingName(id));
  for (const k of record?.keys ?? []) await deleteSealed(db, keyName(id, k.from));
}
