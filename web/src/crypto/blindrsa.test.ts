import { describe, expect, it } from 'vitest';
import { blind, emsaPssEncode, finalize, i2osp, modInverse, modPow, os2ip, verify, type IssuerKey } from './blindrsa';
import { fromHex, toBase64Url, toHex } from './encoding';
import data from './blindrsa-vectors.json';

const v = data.vector;
const big = (hex: string) => BigInt(hex);

async function vectorKey(): Promise<IssuerKey> {
  const n = big(v.n);
  const k = Math.ceil(n.toString(16).length / 2);
  const key = await crypto.subtle.importKey(
    'jwk',
    { kty: 'RSA', n: toBase64Url(i2osp(n, k)), e: toBase64Url(i2osp(big(v.e), 3)), alg: 'PS384', ext: true },
    { name: 'RSA-PSS', hash: 'SHA-384' },
    true,
    ['verify'],
  );
  return { key, n, e: big(v.e), k };
}

describe('RFC 9474 vector, RSABSSA-SHA384-PSS-Deterministic', () => {
  it('encodes, blinds, unblinds and verifies exactly as the RFC says', async () => {
    const pk = await vectorKey();
    const msg = fromHex(v.input_msg);
    const modBits = pk.n.toString(2).length;
    expect(toHex(await emsaPssEncode(msg, modBits - 1, fromHex(v.salt)))).toBe(v.encoded_msg);

    const { blinded, state } = await blind(pk, msg, { salt: fromHex(v.salt), inv: big(v.inv) });
    expect(toHex(blinded)).toBe(v.blinded_msg);

    // The issuer's step (normally the server's, with circl): blinded^d mod n.
    const blindSig = i2osp(modPow(os2ip(blinded), big(v.d), pk.n), pk.k);
    expect(toHex(blindSig)).toBe(v.blind_sig);

    const sig = await finalize(pk, msg, blindSig, state);
    expect(toHex(sig)).toBe(v.sig);
    expect(await verify(pk, msg, sig)).toBe(true);
  });
});

describe('blind signatures', () => {
  it('round-trip with a fresh 2048-bit key, and unlinkably', async () => {
    const pair = await crypto.subtle.generateKey(
      { name: 'RSA-PSS', modulusLength: 2048, publicExponent: new Uint8Array([1, 0, 1]), hash: 'SHA-384' },
      true,
      ['sign', 'verify'],
    );
    const jwk = await crypto.subtle.exportKey('jwk', pair.privateKey);
    const b = (s: string) => os2ip(Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0)));
    const n = b(jwk.n!);
    const d = b(jwk.d!);
    const pk: IssuerKey = { key: pair.publicKey, n, e: b(jwk.e!), k: 256 };
    const msg = crypto.getRandomValues(new Uint8Array(98));

    const first = await blind(pk, msg);
    const second = await blind(pk, msg);
    // The same message blinds differently each time: the issuer can't match them.
    expect(toHex(first.blinded)).not.toBe(toHex(second.blinded));

    const sign = (blinded: Uint8Array) => i2osp(modPow(os2ip(blinded), d, n), 256);
    const sig = await finalize(pk, msg, sign(first.blinded), first.state);
    expect(await verify(pk, msg, sig)).toBe(true);
    // A signature for another blinding fails to unblind into a valid one.
    await expect(finalize(pk, msg, sign(second.blinded), first.state)).rejects.toThrow('does not verify');
    // And a valid signature doesn't cover another message.
    expect(await verify(pk, crypto.getRandomValues(new Uint8Array(98)), sig)).toBe(false);
  });

  it('computes inverses and powers', () => {
    expect(modInverse(3n, 11n)).toBe(4n);
    expect(modPow(4n, 13n, 497n)).toBe(445n);
    expect(() => modInverse(2n, 4n)).toThrow();
    expect(toHex(i2osp(os2ip(fromHex('00ff01')), 3))).toBe('00ff01');
  });
});
