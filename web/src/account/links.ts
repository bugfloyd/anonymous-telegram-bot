// This account's links (design §8). Each link has its own signing key (its
// fingerprint is in the link) and encryption key, generated here, sealed in
// the vault (they are copied to new devices later) and never reused, so two
// links of one person can't be matched.

import { exportPublicKey, fingerprint, generateCopyableAgreementKey, generateCopyableSigningKey } from '../crypto/keys';
import { LABEL_LINK_KEY, sign } from '../crypto/signing';
import { toBase64Url } from '../crypto/encoding';
import { seal } from '../crypto/vault';
import { ApiError, checkObject } from '../net/api';
import type { Session } from '../net/session';
import { hourFloor } from '../protocol/device-list';
import { KEY_VALIDITY_SECONDS, keyParts, newLinkId, type LinkRef } from '../protocol/link';
import { deleteSealed, getKey, getMeta, putMeta, putSealed, type Database } from '../storage/db';
import { withLock } from '../sync/locks';

const META_LINKS = 'links';
const KEY_VAULT = 'vault-key';
const LOCK = 'links';
const ID_ATTEMPTS = 3;
const ID = /^[A-Za-z0-9_-]{11}$/;
const FP = /^[A-Za-z0-9_-]{43}$/;

export interface LinkRecord extends LinkRef {
  /** Validity windows of the encryption keys sealed for this link. */
  keys: { from: number; until: number }[];
}

/** What the server says about this account's links. */
export interface AccountInfo {
  canCreateLinks: boolean;
  admin: boolean;
  links: number;
  maxLinks: number;
}

export function checkAccountInfo(value: unknown): AccountInfo {
  const v = checkObject(value);
  const { canCreateLinks, admin, links, maxLinks } = v;
  if (typeof canCreateLinks !== 'boolean' || typeof admin !== 'boolean') throw new Error('bad account');
  if (!Number.isSafeInteger(links) || !Number.isSafeInteger(maxLinks)) throw new Error('bad account');
  return { canCreateLinks, admin, links: links as number, maxLinks: maxLinks as number };
}

/** Vault names for a link's keys. */
const signingName = (id: string) => `link:${id}:signing`;
const keyName = (id: string, from: number) => `link:${id}:key:${from}`;

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

/**
 * Creates a link: new keys, sealed here first, then the bundle on the server.
 * Throws ApiError invitation_required or link_limit when the server refuses.
 */
export function createLink(db: Database, session: Session, now: () => number = () => Math.floor(Date.now() / 1000)): Promise<LinkRecord> {
  return withLock(LOCK, async () => {
    const vaultKey = await getKey(db, KEY_VAULT);
    if (!vaultKey) throw new Error('no vault key');
    const signing = await generateCopyableSigningKey();
    const encryption = await generateCopyableAgreementKey();
    const signingKey = await exportPublicKey(signing.publicKey);
    const encryptionKey = await exportPublicKey(encryption.publicKey);
    // Back-date by a random few hours, so the keys of two links made around
    // the same time don't carry the same times.
    const from = hourFloor(now()) - 3600 * Math.floor(Math.random() * 12);
    const until = from + KEY_VALIDITY_SECONDS;
    const signature = await sign(signing.privateKey, LABEL_LINK_KEY, ...keyParts(encryptionKey, from, until));
    const fp = await fingerprint(signing.publicKey);

    for (let attempt = 1; ; attempt++) {
      const id = newLinkId();
      // Sealed before the server knows the link, so a link never exists
      // without its keys on this device.
      await putSealed(db, signingName(id), await seal(vaultKey, signing.privateKey, signingName(id)));
      await putSealed(db, keyName(id, from), await seal(vaultKey, encryption.privateKey, keyName(id, from)));
      try {
        await session.call(
          'POST',
          '/links',
          {
            id,
            signingKey: toBase64Url(signingKey),
            keys: [{ key: toBase64Url(encryptionKey), from, until, signature: toBase64Url(signature) }],
          },
          checkObject,
          { sign: true },
        );
      } catch (err) {
        await deleteSealed(db, signingName(id));
        await deleteSealed(db, keyName(id, from));
        if (err instanceof ApiError && err.code === 'conflict' && attempt < ID_ATTEMPTS) continue;
        throw err;
      }
      const record: LinkRecord = { id, fingerprint: fp, keys: [{ from, until }] };
      await putMeta(db, META_LINKS, [...(await listLinks(db)), record]);
      return record;
    }
  });
}

/** Revokes a link and deletes its keys. A link the server no longer has counts as revoked. */
export function revokeLink(db: Database, session: Session, id: string): Promise<void> {
  return withLock(LOCK, async () => {
    try {
      await session.call('DELETE', `/links/${id}`, undefined, checkObject, { sign: true });
    } catch (err) {
      if (!(err instanceof ApiError && err.code === 'not_found')) throw err;
    }
    const links = await listLinks(db);
    const record = links.find((l) => l.id === id);
    await putMeta(
      db,
      META_LINKS,
      links.filter((l) => l.id !== id),
    );
    await deleteSealed(db, signingName(id));
    for (const k of record?.keys ?? []) await deleteSealed(db, keyName(id, k.from));
  });
}
