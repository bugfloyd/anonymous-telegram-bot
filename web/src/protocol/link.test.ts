import { describe, expect, it } from 'vitest';
import { fromHex, toBase64Url, toHex } from '../crypto/encoding';
import { exportPublicKey, fingerprint, generateCopyableAgreementKey, generateSigningKey } from '../crypto/keys';
import { LABEL_LINK_KEY, message, sign } from '../crypto/signing';
import { BundleMismatch, encodePayload, keyParts, linkUrl, newLinkId, parsePayload, PAYLOAD_LENGTH, verifyBundle, type Bundle } from './link';

// The same vector as backend/internal/links/links_test.go.
const KEY = '043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1';
const KEY_MESSAGE =
  '0019696e636f676e69746f636861742f76322f6c696e6b2d6b6579000000010100000041043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb100000008000000006b49d20000000008000000006b6ebc00';
const NOW = 1_800_000_000;
const FP = 'IVVfe8yQuoQPEcsZWUQBwjazebJkrBLkm1Q7ec-It2M';

describe('link payloads', () => {
  it('round-trip at a fixed length', () => {
    const id = newLinkId();
    const payload = encodePayload({ id, fingerprint: FP });
    expect(payload).toHaveLength(PAYLOAD_LENGTH);
    expect(parsePayload(payload)).toEqual({ id, fingerprint: FP });
    expect(linkUrl('some_bot', 'app', { id, fingerprint: FP })).toBe(`https://t.me/some_bot/app?startapp=${payload}`);
  });

  it('reject anything else', () => {
    const good = encodePayload({ id: 'AAAAAAAAAAA', fingerprint: FP });
    expect(parsePayload(good)).not.toBeNull();
    for (const bad of [
      '',
      good.slice(1),
      good + 'A',
      '2' + good.slice(1),
      'reg' + FP,
      '1AAAAAAAAAAB' + FP, // non-canonical ID
      '1AAAAAAAAAA!' + FP.slice(1) + 'A',
    ]) {
      expect(parsePayload(bad)).toBeNull();
    }
  });
});

describe('link keys', () => {
  it('sign the same message as the backend', () => {
    expect(toHex(message(LABEL_LINK_KEY, ...keyParts(fromHex(KEY), NOW, NOW + 28 * 24 * 3600)))).toBe(KEY_MESSAGE);
  });
});

async function makeBundle(options: { from?: number; until?: number } = {}) {
  const signing = await generateSigningKey();
  const enc = await generateCopyableAgreementKey();
  const raw = await exportPublicKey(enc.publicKey);
  const from = options.from ?? NOW - 3600;
  const until = options.until ?? NOW + 3600;
  const signature = await sign(signing.privateKey, LABEL_LINK_KEY, ...keyParts(raw, from, until));
  const bundle: Bundle = { signingKey: await exportPublicKey(signing.publicKey), keys: [{ key: raw, from, until, signature }] };
  return { bundle, ref: { id: 'AAAAAAAAAAA', fingerprint: await fingerprint(signing.publicKey) }, signing, raw };
}

describe('verifyBundle', () => {
  it('accepts a bundle that matches the link and picks the newest current key', async () => {
    const { bundle, ref, signing } = await makeBundle();
    const newer = await generateCopyableAgreementKey();
    const newerRaw = await exportPublicKey(newer.publicKey);
    const signature = await sign(signing.privateKey, LABEL_LINK_KEY, ...keyParts(newerRaw, NOW - 60, NOW + 7200));
    bundle.keys.push({ key: newerRaw, from: NOW - 60, until: NOW + 7200, signature });
    const { raw } = await verifyBundle(ref, bundle, NOW);
    expect(toBase64Url(raw)).toBe(toBase64Url(newerRaw));
  });

  it('rejects a swapped signing key', async () => {
    const { bundle } = await makeBundle();
    const other = await makeBundle();
    await expect(verifyBundle(other.ref, bundle, NOW)).rejects.toBeInstanceOf(BundleMismatch);
  });

  it('rejects an encryption key the signing key never signed', async () => {
    const { bundle, ref } = await makeBundle();
    const other = await makeBundle();
    bundle.keys[0]!.key = other.raw;
    await expect(verifyBundle(ref, bundle, NOW)).rejects.toBeInstanceOf(BundleMismatch);
  });

  it('rejects a bundle with no current key', async () => {
    const { bundle, ref } = await makeBundle({ from: NOW - 7200, until: NOW - 3600 });
    await expect(verifyBundle(ref, bundle, NOW)).rejects.toThrow('no current key');
  });
});
