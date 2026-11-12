import { describe, expect, it } from 'vitest';
import { MAX_FILE_BYTES } from '../protocol/message';
import { decryptFile, encryptFile, newFileKey, uploadSize } from './media';

describe('attachment encryption', () => {
  it('pads to powers of two up to 1 MiB, then whole MiB', () => {
    expect(uploadSize(1)).toBe(16 * 1024);
    expect(uploadSize(16 * 1024 - 28)).toBe(16 * 1024);
    expect(uploadSize(16 * 1024 - 27)).toBe(32 * 1024);
    expect(uploadSize(300_000)).toBe(512 * 1024);
    expect(uploadSize(1024 * 1024 - 28)).toBe(1024 * 1024);
    expect(uploadSize(1024 * 1024)).toBe(2 * 1024 * 1024);
    expect(uploadSize(MAX_FILE_BYTES)).toBe(26 * 1024 * 1024); // the server's limit
  });

  it('round-trips, and refuses a wrong key, size or a changed byte', async () => {
    const file = crypto.getRandomValues(new Uint8Array(50_000));
    const key = newFileKey();
    const upload = await encryptFile(file, key);
    expect(upload.length).toBe(uploadSize(file.length));
    expect(await decryptFile(upload, key, file.length)).toEqual(file);
    await expect(decryptFile(upload, newFileKey(), file.length)).rejects.toThrow();
    await expect(decryptFile(upload, key, 10)).rejects.toThrow();
    const changed = upload.slice();
    changed[100]! ^= 1;
    await expect(decryptFile(changed, key, file.length)).rejects.toThrow();
  });
});
