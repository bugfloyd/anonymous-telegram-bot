import { describe, expect, it } from 'vitest';
import { fromBase64Url, fromHex, toBase64Url, toHex } from './encoding';
import {
  exportPublicKey,
  fingerprint,
  generateAgreementKey,
  generateCopyableSigningKey,
  generateSigningKey,
  importSigningPublicKey,
} from './keys';
import { LABEL_REGISTER, LABEL_SESSION, message, sign, verify } from './signing';
import { generateVaultKey, seal, unseal } from './vault';

// The same vector as backend/internal/devicesig: the backend must accept what
// the client signs, byte for byte.
const BACKEND_MESSAGE =
  '0018696e636f676e69746f636861742f76322f73657373696f6e00000016414141414141414141414141414141414141414141410000000a31373030303030303030000000096e6f6e63652d313233';

describe('signed messages', () => {
  it('encode exactly like the backend', () => {
    expect(toHex(message(LABEL_SESSION, 'AAAAAAAAAAAAAAAAAAAAAA', '1700000000', 'nonce-123'))).toBe(BACKEND_MESSAGE);
  });

  it('keep parts apart', () => {
    expect(toHex(message('l', 'ab', 'c'))).not.toBe(toHex(message('l', 'a', 'bc')));
    expect(toHex(message('l', new Uint8Array([1, 2])))).toBe('00016c000000020102');
  });

  it('sign as P1363 and verify only for the same purpose and parts', async () => {
    const key = await generateSigningKey();
    const sig = await sign(key.privateKey, LABEL_SESSION, 'device', '1700000000', 'nonce');
    expect(sig.length).toBe(64);
    expect(await verify(key.publicKey, sig, LABEL_SESSION, 'device', '1700000000', 'nonce')).toBe(true);
    expect(await verify(key.publicKey, sig, LABEL_REGISTER, 'device', '1700000000', 'nonce')).toBe(false);
    expect(await verify(key.publicKey, sig, LABEL_SESSION, 'device', '1700000001', 'nonce')).toBe(false);
  });
});

describe('keys', () => {
  it('generates non-extractable device keys', async () => {
    const signing = await generateSigningKey();
    const agreement = await generateAgreementKey();
    await expect(crypto.subtle.exportKey('pkcs8', signing.privateKey)).rejects.toThrow();
    await expect(crypto.subtle.exportKey('pkcs8', agreement.privateKey)).rejects.toThrow();
  });

  it('exports 65-byte public points that round-trip', async () => {
    const key = await generateSigningKey();
    const raw = await exportPublicKey(key.publicKey);
    expect(raw.length).toBe(65);
    expect(raw[0]).toBe(0x04);
    const imported = await importSigningPublicKey(raw);
    const sig = await sign(key.privateKey, LABEL_REGISTER, 'x');
    expect(await verify(imported, sig, LABEL_REGISTER, 'x')).toBe(true);
  });

  it('fingerprints as 43 base64url characters, stable per key', async () => {
    const a = await generateSigningKey();
    const b = await generateSigningKey();
    const fa = await fingerprint(a.publicKey);
    expect(fa).toMatch(/^[A-Za-z0-9_-]{43}$/);
    expect(await fingerprint(a.publicKey)).toBe(fa);
    expect(await fingerprint(b.publicKey)).not.toBe(fa);
  });

  it('fingerprints exactly like the backend', async () => {
    // backend/internal/devicesig: TestFingerprintMatchesClient.
    const key = await importSigningPublicKey(
      fromHex(
        '043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1',
      ),
    );
    expect(await fingerprint(key)).toBe('IVVfe8yQuoQPEcsZWUQBwjazebJkrBLkm1Q7ec-It2M');
  });
});

describe('vault', () => {
  it('seals a copyable key and unseals a non-extractable working copy', async () => {
    const vaultKey = await generateVaultKey();
    const account = await generateCopyableSigningKey();
    const sealed = await seal(vaultKey, account.privateKey, 'account');
    const working = await unseal(vaultKey, sealed, 'account', { name: 'ECDSA', namedCurve: 'P-256' }, ['sign']);
    expect(working.extractable).toBe(false);
    const sig = await sign(working, LABEL_REGISTER, 'x');
    expect(await verify(account.publicKey, sig, LABEL_REGISTER, 'x')).toBe(true);
  });

  it('refuses a sealed key under another name, or tampered', async () => {
    const vaultKey = await generateVaultKey();
    const account = await generateCopyableSigningKey();
    const sealed = await seal(vaultKey, account.privateKey, 'account');
    const params = { name: 'ECDSA', namedCurve: 'P-256' } as const;
    await expect(unseal(vaultKey, sealed, 'link:abc', params, ['sign'])).rejects.toThrow();
    const tampered = sealed.slice();
    tampered[20] = (tampered[20] ?? 0) ^ 1;
    await expect(unseal(vaultKey, tampered, 'account', params, ['sign'])).rejects.toThrow();
    await expect(unseal(await generateVaultKey(), sealed, 'account', params, ['sign'])).rejects.toThrow();
  });

  it('cannot export the vault key', async () => {
    await expect(crypto.subtle.exportKey('raw', await generateVaultKey())).rejects.toThrow();
  });
});

describe('encoding', () => {
  it('round-trips base64url and hex', () => {
    const bytes = new Uint8Array([0, 1, 250, 251, 252, 253, 254, 255]);
    expect(toBase64Url(bytes)).toBe('AAH6-_z9_v8');
    expect(Array.from(fromBase64Url('AAH6-_z9_v8'))).toEqual(Array.from(bytes));
    expect(Array.from(fromBase64Url('AAH6-_z9_v8='))).toEqual(Array.from(bytes));
    expect(Array.from(fromHex(toHex(bytes)))).toEqual(Array.from(bytes));
  });

  it('rejects malformed input', () => {
    expect(() => fromBase64Url('a+b/')).toThrow();
    expect(() => fromHex('abc')).toThrow();
    expect(() => fromHex('zz')).toThrow();
  });
});
