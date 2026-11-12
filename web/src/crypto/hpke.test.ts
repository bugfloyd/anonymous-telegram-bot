import { describe, expect, it } from 'vitest';
import { fromHex, toBase64Url, toHex } from './encoding';
import { AEAD_AES_256_GCM, decap, encap, keySchedule, open, seal, type AeadId } from './hpke';
import data from './hpke-vectors.json';

interface Vector {
  aead_id: number;
  info: string;
  skRm: string;
  skEm: string;
  pkRm: string;
  pkEm: string;
  enc: string;
  shared_secret: string;
  key_schedule_context: string;
  secret: string;
  key: string;
  base_nonce: string;
  aad: string;
  pt: string;
  nonce: string;
  ct: string;
}

/** Imports a P-256 key pair from the vector's raw scalar and point. */
async function keyPair(sk: string, pk: string): Promise<CryptoKeyPair> {
  const point = fromHex(pk);
  const jwk = {
    kty: 'EC',
    crv: 'P-256',
    x: toBase64Url(point.slice(1, 33)),
    y: toBase64Url(point.slice(33, 65)),
  };
  const params = { name: 'ECDH', namedCurve: 'P-256' };
  return {
    privateKey: await crypto.subtle.importKey('jwk', { ...jwk, d: toBase64Url(fromHex(sk)) }, params, false, ['deriveBits']),
    publicKey: await crypto.subtle.importKey('jwk', jwk, params, true, []),
  };
}

const vectors = (data as { vectors: Vector[] }).vectors;

describe.each(vectors.map((v) => [v.aead_id === 1 ? 'AES-128-GCM' : 'AES-256-GCM', v] as const))('RFC 9180 vector, %s', (_, v) => {
  const aead = v.aead_id as AeadId;

  it('derives every intermediate value', async () => {
    const ephemeral = await keyPair(v.skEm, v.pkEm);
    const { sharedSecret, enc } = await encap(fromHex(v.pkRm), ephemeral);
    expect(toHex(enc)).toBe(v.enc);
    expect(toHex(sharedSecret)).toBe(v.shared_secret);

    const recipient = await keyPair(v.skRm, v.pkRm);
    expect(toHex(await decap(fromHex(v.enc), recipient.privateKey, fromHex(v.pkRm)))).toBe(v.shared_secret);

    const ks = await keySchedule(sharedSecret, fromHex(v.info), aead);
    expect(toHex(ks.context)).toBe(v.key_schedule_context);
    expect(toHex(ks.secret)).toBe(v.secret);
    expect(toHex(ks.key)).toBe(v.key);
    expect(toHex(ks.baseNonce)).toBe(v.base_nonce);
    expect(v.nonce).toBe(v.base_nonce); // the first encryption uses the base nonce
  });

  it('seals and opens exactly as the RFC says', async () => {
    const ephemeral = await keyPair(v.skEm, v.pkEm);
    const { enc, ct } = await seal(fromHex(v.pkRm), fromHex(v.info), fromHex(v.aad), fromHex(v.pt), { aead, ephemeral });
    expect(toHex(enc)).toBe(v.enc);
    expect(toHex(ct)).toBe(v.ct);

    const recipient = await keyPair(v.skRm, v.pkRm);
    const pt = await open(fromHex(v.enc), recipient.privateKey, fromHex(v.pkRm), fromHex(v.info), fromHex(v.aad), fromHex(v.ct), { aead });
    expect(toHex(pt)).toBe(v.pt);
  });
});

describe('seal and open', () => {
  it('round-trip with fresh keys, and reject anything changed', async () => {
    const recipient = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, false, ['deriveBits']);
    const pk = new Uint8Array(await crypto.subtle.exportKey('raw', recipient.publicKey));
    const info = new Uint8Array([1, 2, 3]);
    const aad = new Uint8Array([9]);
    const pt = crypto.getRandomValues(new Uint8Array(32));
    const { enc, ct } = await seal(pk, info, aad, pt);
    expect(enc).toHaveLength(65);
    expect(ct).toHaveLength(48);
    expect(await open(enc, recipient.privateKey, pk, info, aad, ct)).toEqual(pt);

    await expect(open(enc, recipient.privateKey, pk, new Uint8Array([1, 2, 4]), aad, ct)).rejects.toThrow();
    await expect(open(enc, recipient.privateKey, pk, info, new Uint8Array([8]), ct)).rejects.toThrow();
    const flipped = ct.slice();
    flipped[0]! ^= 1;
    await expect(open(enc, recipient.privateKey, pk, info, aad, flipped)).rejects.toThrow();

    const other = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, false, ['deriveBits']);
    await expect(open(enc, other.privateKey, pk, info, aad, ct)).rejects.toThrow();
    // Two seals to the same key never share an ephemeral key.
    const again = await seal(pk, info, aad, pt, { aead: AEAD_AES_256_GCM });
    expect(toHex(again.enc)).not.toBe(toHex(enc));
  });
});
