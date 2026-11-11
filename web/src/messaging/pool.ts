// The mailbox pool (design §9): mailboxes created ahead of time, so taking
// one for a new conversation never comes right after creating it.

import { checkObject } from '../net/api';
import type { Session } from '../net/session';
import { getMeta, putMeta, type Database } from '../storage/db';
import { withLock } from '../sync/locks';

const META_POOL = 'mailbox-pool';
const LOCK = 'mailbox-pool';
export const POOL_LOW = 3;
export const POOL_REFILL = 5;
const ID = /^[A-Za-z0-9_-]{22}$/;

async function pool(db: Database): Promise<string[]> {
  const value = await getMeta<unknown>(db, META_POOL);
  return Array.isArray(value) ? value.filter((v): v is string => typeof v === 'string' && ID.test(v)) : [];
}

export async function poolSize(db: Database): Promise<number> {
  return (await pool(db)).length;
}

/** Adds mailboxes to the pool. */
export function refillPool(db: Database, session: Session, count = POOL_REFILL): Promise<number> {
  return withLock(LOCK, async () => {
    const created = await session.call('POST', '/mailboxes', { count }, (v) => {
      const m = checkObject(v).mailboxes;
      if (!Array.isArray(m) || m.length !== count || !m.every((x) => typeof x === 'string' && ID.test(x))) throw new Error('bad mailboxes');
      return m as string[];
    });
    const next = [...(await pool(db)), ...created];
    await putMeta(db, META_POOL, next);
    return next.length;
  });
}

/** Drops mailboxes from the pool (the server no longer has them as ours). */
export function dropFromPool(db: Database, ids: Set<string>): Promise<void> {
  return withLock(LOCK, async () => {
    await putMeta(db, META_POOL, (await pool(db)).filter((id) => !ids.has(id)));
  });
}

/** The pool's mailboxes. */
export function pooled(db: Database): Promise<string[]> {
  return pool(db);
}

/** Takes a mailbox from the pool, refilling first only if it is empty. */
export async function takeMailbox(db: Database, session: Session): Promise<string> {
  if ((await poolSize(db)) === 0) await refillPool(db, session);
  return withLock(LOCK, async () => {
    const [first, ...rest] = await pool(db);
    if (!first) throw new Error('no mailbox');
    await putMeta(db, META_POOL, rest);
    return first;
  });
}
