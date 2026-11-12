import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { getMeta, openDatabase, putMeta } from './db';
import { databaseName, profileFor } from './profile';

describe('profiles', () => {
  it('give each Telegram account its own, stable, opaque database', async () => {
    const a = await profileFor('123456789');
    const b = await profileFor('987654321');
    expect(a).toMatch(/^[A-Za-z0-9_-]{16}$/);
    expect(a).not.toBe(b);
    expect(await profileFor('123456789')).toBe(a);
    expect(databaseName(a)).not.toContain('123456789');
    expect(databaseName(await profileFor(''))).toBe('incognitochat');
  });

  it('keep two accounts on one Telegram app apart', async () => {
    const first = await openDatabase(databaseName(await profileFor('111')));
    const second = await openDatabase(databaseName(await profileFor('222')));
    await putMeta(first, 'account', { state: 'registered' });
    expect(await getMeta(second, 'account')).toBeUndefined();
  });
});
