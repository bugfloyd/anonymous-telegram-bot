// Turns a picked file into an attachment, before it's encrypted (phase 5).
// "As photo" redraws the image at up to 1280 px as JPEG, as Telegram does:
// smaller, and without the file's metadata (location, camera). "As file"
// keeps the original bytes untouched.

import { toBase64Url } from '../crypto/encoding';
import type { Outgoing } from '../messaging/engine';
import { MAX_FILE_BYTES } from '../protocol/message';

/** The file is over the 25 MB limit. */
export class TooBig extends Error {
  constructor() {
    super('file too big');
    this.name = 'TooBig';
  }
}

const PHOTO_SIDE = 1280;
const PREVIEW_SIDE = 40;
const MAX_PREVIEW = 3000;

async function drawJpeg(image: ImageBitmap, side: number, quality: number): Promise<{ bytes: Uint8Array<ArrayBuffer>; w: number; h: number }> {
  const scale = Math.min(1, side / Math.max(image.width, image.height));
  const w = Math.max(1, Math.round(image.width * scale));
  const h = Math.max(1, Math.round(image.height * scale));
  const canvas = document.createElement('canvas');
  canvas.width = w;
  canvas.height = h;
  const ctx = canvas.getContext('2d');
  if (!ctx) throw new Error('no canvas');
  // JPEG has no transparency: white behind it, as Telegram does.
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, w, h);
  ctx.drawImage(image, 0, 0, w, h);
  const blob = await new Promise<Blob>((resolve, reject) => canvas.toBlob((b) => (b ? resolve(b) : reject(new Error('encode'))), 'image/jpeg', quality));
  return { bytes: new Uint8Array(await blob.arrayBuffer()), w, h };
}

/** A photo: redrawn smaller as JPEG, with a tiny preview. */
export async function preparePhoto(file: Blob): Promise<Outgoing> {
  const image = await createImageBitmap(file, { imageOrientation: 'from-image' });
  try {
    const full = await drawJpeg(image, PHOTO_SIDE, 0.85);
    const tiny = toBase64Url((await drawJpeg(image, PREVIEW_SIDE, 0.5)).bytes);
    if (full.bytes.length > MAX_FILE_BYTES) throw new TooBig();
    return {
      file: full.bytes,
      media: { t: 'photo', s: full.bytes.length, m: 'image/jpeg', w: full.w, h: full.h, ...(tiny.length <= MAX_PREVIEW ? { p: tiny } : {}) },
    };
  } finally {
    image.close();
  }
}

/** Any file, untouched. */
export async function prepareFile(file: File): Promise<Outgoing> {
  if (file.size > MAX_FILE_BYTES) throw new TooBig();
  if (file.size === 0) throw new Error('empty file');
  const bytes = new Uint8Array(await file.arrayBuffer());
  const type = /^[\w.+-]+\/[\w.+-]+$/.test(file.type) && file.type.length <= 100 ? file.type : null;
  return { file: bytes, media: { t: 'file', s: bytes.length, n: (file.name || 'file').slice(0, 255), ...(type ? { m: type } : {}) } };
}

/** Whether a picked file can be sent as a photo (GIFs come later, animated). */
export function isPhoto(file: File): boolean {
  return /^image\/(jpeg|png|webp|heic|heif|avif|bmp)$/.test(file.type);
}
