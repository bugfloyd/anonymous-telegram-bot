import { describe, expect, it } from 'vitest';
import { fromHex, toHex } from '../crypto/encoding';
import { encodeDeviceList, firstList, hourFloor, MAX_VALIDITY_SECONDS } from './device-list';

// The same list as backend/internal/devicelist/devicelist_test.go.
const KEY = '043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb1';
const VECTOR =
  '001c696e636f676e69746f636861742f76322f6465766963652d6c697374000000010100000008000000000000000100000020000000000000000000000000000000000000000000000000000000000000000000000008000000006b49d20000000008000000006b715f00000000100000000000000000000000000000000000000041043e4cd596e64fe0d4dac5cce1479f58b981e24664c49c89e823be92673cd970d130b3038618a2156d3db3edccde5a6f271cb346dd868dc771d0ba2812a1197cb100000008000000006b49d200';

describe('device list', () => {
  it('encodes exactly as the backend does', () => {
    const list = firstList('AAAAAAAAAAAAAAAAAAAAAA', fromHex(KEY), 1_800_000_000 + 1234);
    expect(list.issued).toBe(1_800_000_000);
    expect(list.expires - list.issued).toBe(MAX_VALIDITY_SECONDS);
    expect(toHex(encodeDeviceList(list))).toBe(VECTOR);
  });

  it('rounds times down to the hour', () => {
    expect(hourFloor(7199)).toBe(3600);
    expect(hourFloor(7200)).toBe(7200);
  });

  it('refuses malformed entries', () => {
    const list = firstList('AAAAAAAAAAAAAAAAAAAAAA', fromHex(KEY), 1_800_000_000);
    expect(() => encodeDeviceList({ ...list, entries: [{ ...list.entries[0]!, deviceId: 'AAAA' }] })).toThrow();
    expect(() => encodeDeviceList({ ...list, entries: [{ ...list.entries[0]!, key: new Uint8Array(64) }] })).toThrow();
    expect(() => encodeDeviceList({ ...list, prev: new Uint8Array(31) })).toThrow();
  });
});
