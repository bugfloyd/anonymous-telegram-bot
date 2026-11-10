import 'fake-indexeddb/auto';
import { describe, expect, it } from 'vitest';
import { generateCopyableSigningKey, generateSigningKey } from '../crypto/keys';
import { LABEL_SESSION, sign, verify } from '../crypto/signing';
import { generateVaultKey, seal, unseal } from '../crypto/vault';
import { getKey, getKeyPair, getMeta, getSealed, openDatabase, putKey, putMeta, putSealed } from './db';

let n = 0;
const fresh = () => openDatabase(`test-${n++}`);

describe('storage', () => {
  it('keeps a non-extractable key pair usable after reopening', async () => {
    const name = `test-${n++}`;
    const pair = await generateSigningKey();
    const db = await openDatabase(name);
    await putKey(db, 'device-signing', { privateKey: pair.privateKey, publicKey: pair.publicKey });
    db.close();

    const reopened = await openDatabase(name);
    const stored = await getKeyPair(reopened, 'device-signing');
    expect(stored).toBeDefined();
    expect(stored!.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', stored!.privateKey)).rejects.toThrow();
    const sig = await sign(stored!.privateKey, LABEL_SESSION, 'x');
    expect(await verify(pair.publicKey, sig, LABEL_SESSION, 'x')).toBe(true);
  });

  it('tells single keys and key pairs apart', async () => {
    const db = await fresh();
    await putKey(db, 'vault', await generateVaultKey());
    const pair = await generateSigningKey();
    await putKey(db, 'pair', { privateKey: pair.privateKey, publicKey: pair.publicKey });
    expect(await getKey(db, 'vault')).toBeInstanceOf(CryptoKey);
    expect(await getKeyPair(db, 'vault')).toBeUndefined();
    expect(await getKey(db, 'pair')).toBeUndefined();
    expect(await getKeyPair(db, 'pair')).toBeDefined();
    expect(await getKey(db, 'missing')).toBeUndefined();
  });

  it('stores sealed keys that unseal after reopening', async () => {
    const db = await fresh();
    const vaultKey = await generateVaultKey();
    const account = await generateCopyableSigningKey();
    await putKey(db, 'vault', vaultKey);
    await putSealed(db, 'account', await seal(vaultKey, account.privateKey, 'account'));

    const storedVault = await getKey(db, 'vault');
    const sealed = await getSealed(db, 'account');
    const working = await unseal(storedVault!, sealed!, 'account', { name: 'ECDSA', namedCurve: 'P-256' }, ['sign']);
    const sig = await sign(working, LABEL_SESSION, 'y');
    expect(await verify(account.publicKey, sig, LABEL_SESSION, 'y')).toBe(true);
  });

  it('stores small state', async () => {
    const db = await fresh();
    expect(await getMeta(db, 'account')).toBeUndefined();
    await putMeta(db, 'account', { id: 'abc', registeredAt: 1 });
    expect(await getMeta<{ id: string }>(db, 'account')).toEqual({ id: 'abc', registeredAt: 1 });
  });
});
