import { describe, expect, it } from 'vitest';
import { concat } from '../crypto/encoding';
import { stripVideoMetadata } from './mp4';

function box(type: string, ...children: Uint8Array[]): Uint8Array {
  const body = children.length ? concat(...children) : new Uint8Array(0);
  const out = new Uint8Array(8 + body.length);
  new DataView(out.buffer).setUint32(0, out.length);
  out.set([...type].map((c) => c.charCodeAt(0)), 4);
  out.set(body, 8);
  return out;
}

/** A header box: version, flags, then the two times filled with 0xAA, then 4 more bytes. */
function header(type: string, version: 0 | 1): Uint8Array {
  const times = new Uint8Array(version === 1 ? 16 : 8).fill(0xaa);
  return box(type, new Uint8Array([version, 0, 0, 0]), times, new Uint8Array([1, 2, 3, 4]));
}

const text = (s: string) => new TextEncoder().encode(s);
const types = (bytes: Uint8Array) => new TextDecoder('latin1').decode(bytes);

function movie(): Uint8Array {
  return concat(
    box('ftyp', text('qt  ')),
    box('mdat', text('frames')),
    box(
      'moov',
      header('mvhd', 0),
      box('trak', header('tkhd', 1), box('mdia', header('mdhd', 0), box('hdlr', text('vide'))), box('udta', text('name'))),
      box('udta', box('©xyz', text('+52.3676+004.9041/'))),
      box('meta', box('keys', text('com.apple.quicktime.location.ISO6709')), box('ilst', text('+52.3676+004.9041/'))),
    ),
  );
}

describe('stripVideoMetadata', () => {
  it('drops location and device metadata and recording times, keeping the length', () => {
    const bytes = movie();
    const before = bytes.slice();
    expect(stripVideoMetadata(bytes)).toBe(true);
    expect(bytes.length).toBe(before.length);
    const after = types(bytes);
    expect(after).not.toContain('udta');
    expect(after).not.toContain('meta');
    expect(after).toContain('free');
    expect(after).not.toContain('52.3676');
    expect(after).not.toContain('com.apple');
    expect(after).not.toContain('name');
    expect(after).toContain('frames');
    expect(after).toContain('vide');
    expect(bytes.includes(0xaa)).toBe(false);
    // Everything outside the metadata boxes and the times is as it was.
    expect(types(bytes.subarray(0, 30))).toBe(types(before.subarray(0, 30)));
  });

  it('reads 64-bit and to-the-end sizes', () => {
    const moov = box('moov', box('udta', text('where')));
    const large = new Uint8Array(16 + 6);
    const view = new DataView(large.buffer);
    view.setUint32(0, 1);
    large.set(text('mdat'), 4);
    view.setUint32(8, 0);
    view.setUint32(12, large.length);
    const last = box('free', text('end'));
    new DataView(last.buffer).setUint32(0, 0);
    const bytes = concat(box('ftyp', text('isom')), large, moov, last);
    expect(stripVideoMetadata(bytes)).toBe(true);
    expect(types(bytes)).not.toContain('udta');
  });

  it('drops XMP and reads fragmented files', () => {
    const xmp = box('uuid', new Uint8Array([0xbe, 0x7a, 0xcf, 0xcb, 0x97, 0xa9, 0x42, 0xe8, 0x9c, 0x71, 0x99, 0x94, 0x91, 0xe3, 0xaf, 0xac]), text('<exif:GPSLatitude>52,22N'));
    const other = box('uuid', new Uint8Array(16).fill(7), text('keep me'));
    const bytes = concat(box('ftyp', text('iso6')), box('moov', header('mvhd', 0)), box('moof', text('fragment')), box('mdat', text('frames')), xmp, other);
    expect(stripVideoMetadata(bytes)).toBe(true);
    const after = types(bytes);
    expect(after).not.toContain('GPS');
    expect(after).toContain('fragment');
    expect(after).toContain('keep me');
  });

  it('leaves anything else untouched', () => {
    for (const bytes of [text('not a video at all'), box('ftyp', text('isom')), concat(box('ftyp', text('isom')), box('moov', box('udta')).subarray(0, 12))]) {
      const before = bytes.slice();
      expect(stripVideoMetadata(bytes)).toBe(false);
      expect(bytes).toEqual(before);
    }
  });
});
