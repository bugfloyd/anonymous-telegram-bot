import { describe, expect, it } from 'vitest';
import { encryptFile, newFileKey } from '../crypto/media';
import { fromBase64Url, toBase64Url } from '../crypto/encoding';
import { ApiError, type Api } from '../net/api';
import type { Media } from '../protocol/message';
import { fetchForSaving, Gone, readTicket, saveUrl, SAVE_PREFIX } from './save';

const key = newFileKey();
const media: Media = {
  t: 'file',
  id: 'AAAAAAAAAAAAAAAAAAAAAA',
  k: toBase64Url(key),
  d: 'BBBBBBBBBBBBBBBBBBBBBA',
  s: 5,
  n: 'notes ✓.txt',
  m: 'text/plain',
};

describe('save tickets', () => {
  it('carry what the save page needs, after the # only', () => {
    const url = saveUrl('https://example.github.io/', media);
    expect(url.startsWith(`https://example.github.io/${SAVE_PREFIX}`)).toBe(true);
    const ticket = readTicket(new URL(url).hash);
    expect(ticket).toEqual({ id: media.id, k: media.k, s: 5, n: 'notes ✓.txt', m: 'text/plain' });
  });

  it('refuse broken ones', () => {
    expect(readTicket('#save=nonsense')).toBeNull();
    expect(readTicket('#other')).toBeNull();
    const bad = saveUrl('https://x/', { ...media, k: 'AAAA' });
    expect(readTicket(new URL(bad).hash)).toBeNull();
  });

  it('carries no delete secret, and still reads older links that did', () => {
    const url = saveUrl('https://x/', media);
    expect(new TextDecoder().decode(fromBase64Url(new URL(url).hash.slice(SAVE_PREFIX.length)))).not.toContain(media.d);
    const old = `#save=${toBase64Url(new TextEncoder().encode(JSON.stringify({ id: media.id, k: media.k, d: media.d, s: 5 })))}`;
    expect(readTicket(old)).toEqual({ id: media.id, k: media.k, s: 5 });
  });

  it('fetches and decrypts the upload, and leaves it on the server; a gone upload says so', async () => {
    const upload = await encryptFile(new TextEncoder().encode('hello'), key);
    const calls: string[] = [];
    const api: Api = {
      post: (path, body, check) => api.request('POST', path, { body: JSON.stringify(body) }, check),
      async request(method, path, _init, check) {
        calls.push(`${method} ${path}`);
        if (path === `/media/${media.id}`) return check({ url: 'https://bucket.example/m/x' });
        throw new ApiError(404, 'not_found');
      },
    };
    const transfer = { put: async () => {}, get: async () => upload };
    const ticket = readTicket(new URL(saveUrl('https://x/', media)).hash)!;
    const file = await fetchForSaving(api, transfer, ticket);
    expect(file.name).toBe('notes ✓.txt');
    expect(file.type).toBe('text/plain');
    expect(new TextDecoder().decode(await file.arrayBuffer())).toBe('hello');
    expect(calls).toEqual([`GET /media/${media.id}`]);
    await expect(fetchForSaving(api, transfer, { ...ticket, id: 'CCCCCCCCCCCCCCCCCCCCCA' })).rejects.toBeInstanceOf(Gone);
  });
});
