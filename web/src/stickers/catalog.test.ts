import { describe, expect, it } from 'vitest';
import { toBase64Url } from '../crypto/encoding';
import { checkMessage } from '../protocol/message';
import { findSticker, PACKS } from './catalog';

describe('sticker catalog', () => {
  it('has an image for every sticker, and a sticker for every image', () => {
    const art = Object.keys(import.meta.glob('/public/stickers/*/*.webp')).map((path) => path.slice('/public/stickers/'.length, -'.webp'.length));
    const ids = PACKS.flatMap((p) => p.stickers.map((s) => s.id));
    expect(new Set(ids).size).toBe(ids.length);
    expect([...art].sort()).toEqual([...ids].sort());
  });

  it('only has IDs and emoji a message can carry', () => {
    for (const s of PACKS.flatMap((p) => p.stickers)) {
      expect(findSticker(s.id)).toBe(s);
      expect(() =>
        checkMessage({ v: 1, kind: 'message', conv: 'AAAAAAAAAAAAAAAAAAAAAA', id: 'AAAAAAAAAAAAAAAAAAAAAA', at: 1, from: toBase64Url(new Uint8Array(65)), to: 'AAAAAAAAAAA', sticker: s }),
      ).not.toThrow();
    }
  });
});
