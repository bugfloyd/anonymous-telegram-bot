// The account's signed device list, byte-compatible with the backend
// (backend/internal/devicelist). Devices sign it with the account key; the
// server only checks it is well formed and stores it.
//
// A signed message labelled LABEL_DEVICE_LIST with the parts: suite (1 byte),
// sequence number (8), hash of the previous list (32), issued (8), expires (8),
// then for each device: ID (16), signing key (65), added (8). Big-endian;
// times are Unix seconds on a whole hour, so the list doesn't say exactly when
// anything happened.

import { fromBase64Url } from '../crypto/encoding';
import { SUITE } from '../crypto/keys';
import { LABEL_DEVICE_LIST, message } from '../crypto/signing';

export const MAX_VALIDITY_SECONDS = 30 * 24 * 3600;
const HOUR = 3600;

export interface DeviceEntry {
  /** 22-character device ID. */
  deviceId: string;
  /** The device signing key, a 65-byte P-256 point. */
  key: Uint8Array;
  /** Unix seconds, on a whole hour. */
  added: number;
}

export interface DeviceList {
  seq: number;
  /** SHA-256 of the previous list; 32 zero bytes for the first. */
  prev: Uint8Array;
  issued: number;
  expires: number;
  entries: DeviceEntry[];
}

/** The start of the hour containing a Unix time. */
export function hourFloor(unixSeconds: number): number {
  return Math.floor(unixSeconds / HOUR) * HOUR;
}

/** A new account's first list: just this device, valid for 30 days. */
export function firstList(deviceId: string, key: Uint8Array, nowSeconds: number): DeviceList {
  const issued = hourFloor(nowSeconds);
  return {
    seq: 1,
    prev: new Uint8Array(32),
    issued,
    expires: issued + MAX_VALIDITY_SECONDS,
    entries: [{ deviceId, key, added: issued }],
  };
}

/** The list's signed bytes. */
export function encodeDeviceList(list: DeviceList): Uint8Array<ArrayBuffer> {
  if (list.prev.length !== 32) throw new Error('prev must be 32 bytes');
  const parts: Uint8Array[] = [new Uint8Array([SUITE]), u64(list.seq), list.prev, u64(list.issued), u64(list.expires)];
  for (const entry of list.entries) {
    const id = fromBase64Url(entry.deviceId);
    if (id.length !== 16) throw new Error('bad device ID');
    if (entry.key.length !== 65) throw new Error('bad device key');
    parts.push(id, entry.key, u64(entry.added));
  }
  return message(LABEL_DEVICE_LIST, ...parts);
}

function u64(n: number): Uint8Array {
  if (!Number.isSafeInteger(n) || n < 0) throw new Error('not a u64');
  const out = new Uint8Array(8);
  new DataView(out.buffer).setBigUint64(0, BigInt(n));
  return out;
}
