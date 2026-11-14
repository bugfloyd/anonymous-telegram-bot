// Saving an attachment outside Telegram (docs/protocol.md, Media). A Mini
// App can't hand a file to the device: Telegram's download needs a public
// HTTPS URL, which would mean a decrypted copy on a server. So the app opens
// this site in the device's own browser, with what's needed to fetch and
// open the upload after the "#", which browsers never send to any server:
//
//   <site>/#save=<base64url(JSON {id, k, s, n?, m?})>
//
// The page fetches the upload, decrypts it on the device and offers it as a
// normal download. It doesn't delete the upload: saving or not, it stays
// until the recipient deletes the message or the conversation, or it
// expires, so the app can always fetch it again until then. The link
// carries no delete secret, so one left in a browser's history can only
// fetch what its holder could already open.

import { fromBase64Url, toBase64Url, utf8 } from '../crypto/encoding';
import { decryptFile } from '../crypto/media';
import { checkObject, type Api } from '../net/api';
import type { Transfer } from '../net/transfer';
import { fileName, type Media } from '../protocol/message';

export const SAVE_PREFIX = '#save=';

/** What the save page needs: how to fetch and open the upload, and what to call it. */
export type SaveTicket = Pick<Media, 'id' | 'k' | 's' | 'n' | 'm'>;

/** The address that opens the save page for an attachment. */
export function saveUrl(base: string, media: Media): string {
  const ticket: SaveTicket = { id: media.id, k: media.k, s: media.s, n: fileName(media), ...(media.m ? { m: media.m } : {}) };
  return `${base}${SAVE_PREFIX}${toBase64Url(utf8(JSON.stringify(ticket)))}`;
}

/** Reads a save ticket from the address's fragment, or null if there isn't a valid one. */
export function readTicket(hash: string): SaveTicket | null {
  if (!hash.startsWith(SAVE_PREFIX)) return null;
  try {
    const t = JSON.parse(new TextDecoder().decode(fromBase64Url(hash.slice(SAVE_PREFIX.length)))) as Record<string, unknown>;
    const id = typeof t.id === 'string' && /^[A-Za-z0-9_-]{22}$/.test(t.id) ? t.id : null;
    const ok =
      id &&
      typeof t.k === 'string' &&
      fromBase64Url(t.k).length === 32 &&
      Number.isSafeInteger(t.s) &&
      (t.s as number) > 0 &&
      (t.n === undefined || typeof t.n === 'string') &&
      (t.m === undefined || (typeof t.m === 'string' && /^[\w.+-]+\/[\w.+-]+$/.test(t.m)));
    // Older links also carry a delete secret, which is left out.
    return ok ? { id, k: t.k as string, s: t.s as number, ...(t.n !== undefined ? { n: t.n as string } : {}), ...(t.m !== undefined ? { m: t.m as string } : {}) } : null;
  } catch {
    return null;
  }
}

/** The upload is gone: saved already, deleted, or expired. */
export class Gone extends Error {
  constructor() {
    super('upload gone');
    this.name = 'Gone';
  }
}

/** Fetches and decrypts the attachment a ticket names. */
export async function fetchForSaving(api: Api, transfer: Transfer, ticket: SaveTicket, onProgress?: (f: number) => void): Promise<File> {
  let url: string;
  try {
    url = await api.request('GET', `/media/${ticket.id}`, {}, (v) => {
      const { url } = checkObject(v);
      if (typeof url !== 'string' || !url.startsWith('https://')) throw new Error('bad download');
      return url;
    });
  } catch (err) {
    if ((err as { code?: string }).code === 'not_found') throw new Gone();
    throw err;
  }
  const bytes = await decryptFile(await transfer.get(url, onProgress), fromBase64Url(ticket.k), ticket.s);
  return new File([bytes], ticket.n ?? 'file', ticket.m ? { type: ticket.m } : {});
}
