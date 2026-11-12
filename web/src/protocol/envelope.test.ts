import { describe, expect, it } from 'vitest';
import { utf8 } from '../crypto/encoding';
import { generateCopyableAgreementKey, exportPublicKey } from '../crypto/keys';
import {
  EnvelopeError,
  keyId,
  MAX_ENVELOPE,
  MAX_INNER,
  openEnvelope,
  pad,
  PADDED_SIZES,
  parseEnvelope,
  sealEnvelope,
  unpad,
  WRAP_LENGTH,
  type RecipientKey,
} from './envelope';

async function recipient(): Promise<RecipientKey & { publicKey: Uint8Array<ArrayBuffer>; id: Uint8Array }> {
  const pair = await generateCopyableAgreementKey();
  const publicKey = await exportPublicKey(pair.publicKey);
  return { privateKey: pair.privateKey, publicKey, id: await keyId(publicKey) };
}

const finder = (...keys: { id: Uint8Array; privateKey: CryptoKey; publicKey: Uint8Array }[]) => async (id: Uint8Array) =>
  keys.find((k) => k.id.every((b, i) => b === id[i]));

describe('padding', () => {
  it('pads to fixed sizes and back', () => {
    for (const n of [0, 1, 508, 509, 2044, 2045, MAX_INNER]) {
      const inner = new Uint8Array(n).fill(7);
      const padded = pad(inner);
      expect(PADDED_SIZES).toContain(padded.length);
      expect(unpad(padded)).toEqual(inner);
    }
    expect(pad(new Uint8Array(508))).toHaveLength(512);
    expect(pad(new Uint8Array(509))).toHaveLength(2048);
    expect(() => pad(new Uint8Array(MAX_INNER + 1))).toThrow(EnvelopeError);
  });

  it('rejects padding that was tampered with', () => {
    const padded = pad(new Uint8Array([1, 2, 3]));
    const junk = padded.slice();
    junk[100] = 1;
    expect(() => unpad(junk)).toThrow(EnvelopeError);
    const long = padded.slice();
    new DataView(long.buffer).setUint32(0, 600);
    expect(() => unpad(long)).toThrow(EnvelopeError);
    expect(() => unpad(new Uint8Array(513))).toThrow(EnvelopeError);
  });
});

describe('envelope', () => {
  it('opens for the recipient, only', async () => {
    const alice = await recipient();
    const bob = await recipient();
    const envelope = await sealEnvelope([alice.publicKey], utf8('hello'));
    expect(envelope.length).toBe(2 + WRAP_LENGTH + 12 + 512 + 16);
    const opened = await openEnvelope(envelope, finder(alice));
    expect(new TextDecoder().decode(opened.inner)).toBe('hello');
    expect(opened.keyId).toEqual(alice.id);
    await expect(openEnvelope(envelope, finder(bob))).rejects.toThrow('no key');
  });

  it('opens for every recipient of a multi-wrap envelope', async () => {
    const keys = [await recipient(), await recipient(), await recipient()];
    const envelope = await sealEnvelope(
      keys.map((k) => k.publicKey),
      utf8('to all my devices'),
    );
    for (const k of keys) {
      expect(new TextDecoder().decode((await openEnvelope(envelope, finder(k))).inner)).toBe('to all my devices');
    }
  });

  it('catches a wrap moved onto another body, and a changed body', async () => {
    const alice = await recipient();
    const a = parseEnvelope(await sealEnvelope([alice.publicKey], utf8('first')));
    const b = await sealEnvelope([alice.publicKey], utf8('second'));
    // a's wrap on b's body.
    const spliced = new Uint8Array(b);
    spliced.set([...a.wraps[0]!.keyId, ...a.wraps[0]!.enc, ...a.wraps[0]!.ct], 2);
    await expect(openEnvelope(spliced, finder(alice))).rejects.toThrow(EnvelopeError);

    const flipped = new Uint8Array(b);
    flipped[flipped.length - 1]! ^= 1;
    await expect(openEnvelope(flipped, finder(alice))).rejects.toThrow(EnvelopeError);
  });

  it('rejects malformed layouts before any crypto', () => {
    expect(() => parseEnvelope(new Uint8Array([2, 1]))).toThrow('unknown suite');
    expect(() => parseEnvelope(new Uint8Array([1, 0]))).toThrow('bad wrap count');
    expect(() => parseEnvelope(new Uint8Array(2 + WRAP_LENGTH + 100).fill(1, 0, 2))).toThrow('bad body size');
    expect(() => parseEnvelope(new Uint8Array(MAX_ENVELOPE + 1))).toThrow('bad size');
  });

  it('gives each key a stable 8-byte ID', async () => {
    const k = await recipient();
    expect(k.id).toHaveLength(8);
    expect(await keyId(k.publicKey)).toEqual(k.id);
  });
});
