// Turns a picked file into an attachment, before it's encrypted (phase 5).
// "As photo" redraws the image at up to 1280 px as JPEG, as Telegram does:
// smaller, and without the file's metadata (location, camera). "As video"
// keeps the recording (no compression; at most 25 MB) minus its location
// and time metadata (media/mp4.ts), with a frame as preview, its size and
// length. "As GIF" sends the GIF as it is, animated. "As music" sends an
// audio file as it is, with its length, for the in-chat player. "As file"
// keeps the original bytes untouched. Voice messages are recorded instead
// (media/record.ts).

import { toBase64Url } from '../crypto/encoding';
import type { Outgoing } from '../messaging/engine';
import { MAX_FILE_BYTES } from '../protocol/message';
import { stripVideoMetadata } from './mp4';

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

async function drawJpeg(
  image: ImageBitmap | HTMLCanvasElement | HTMLVideoElement,
  side: number,
  quality: number,
): Promise<{ bytes: Uint8Array<ArrayBuffer>; w: number; h: number; canvas: HTMLCanvasElement }> {
  const [width, height] = image instanceof HTMLVideoElement ? [image.videoWidth, image.videoHeight] : [image.width, image.height];
  const scale = Math.min(1, side / Math.max(width, height));
  const w = Math.max(1, Math.round(width * scale));
  const h = Math.max(1, Math.round(height * scale));
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
  return { bytes: new Uint8Array(await blob.arrayBuffer()), w, h, canvas };
}

/** A photo: redrawn smaller as JPEG, with a tiny preview. */
export async function preparePhoto(file: Blob): Promise<Outgoing> {
  const image = await createImageBitmap(file, { imageOrientation: 'from-image' });
  try {
    const full = await drawJpeg(image, PHOTO_SIDE, 0.85);
    if (full.bytes.length > MAX_FILE_BYTES) throw new TooBig();
    // From the shrunk copy: drawing the full-size photo again is the slow part.
    return { file: full.bytes, media: { t: 'photo', s: full.bytes.length, m: 'image/jpeg', w: full.w, h: full.h, ...(await preview(full.canvas)) } };
  } finally {
    image.close();
  }
}

/** A tiny blurred-preview JPEG, base64url, if it's small enough to carry. */
async function preview(image: ImageBitmap | HTMLCanvasElement | HTMLVideoElement): Promise<{ p?: string }> {
  const tiny = toBase64Url((await drawJpeg(image, PREVIEW_SIDE, 0.5)).bytes);
  return tiny.length <= MAX_PREVIEW ? { p: tiny } : {};
}

/** A GIF, as it is (it plays in every browser), with its first frame as preview. */
export async function prepareGif(file: File): Promise<Outgoing> {
  if (file.size > MAX_FILE_BYTES) throw new TooBig();
  const image = await createImageBitmap(file);
  try {
    const bytes = new Uint8Array(await file.arrayBuffer());
    return { file: bytes, media: { t: 'gif', s: bytes.length, m: 'image/gif', w: image.width, h: image.height, ...(await preview(image)) } };
  } finally {
    image.close();
  }
}

/**
 * A video, as recorded, without its location and time metadata. If this
 * browser can't read it, it can't make a preview either: it goes as a file,
 * as Telegram does with videos it can't play.
 */
export async function prepareVideo(file: File): Promise<Outgoing> {
  if (file.size > MAX_FILE_BYTES) throw new TooBig();
  const info = await probeVideo(file);
  if (!info) return prepareFile(file);
  const bytes = new Uint8Array(await file.arrayBuffer());
  stripVideoMetadata(bytes);
  const type = file.type === 'video/x-m4v' ? 'video/mp4' : file.type;
  return { file: bytes, media: { t: 'video', s: bytes.length, m: type, ...info } };
}

/** A video's size, length and a preview frame; null if this browser can't open it. */
async function probeVideo(file: Blob): Promise<{ w: number; h: number; l?: number; p?: string } | null> {
  const url = URL.createObjectURL(file);
  const video = document.createElement('video');
  video.muted = true;
  video.playsInline = true;
  video.preload = 'auto';
  try {
    video.src = url;
    if (!(await happens(video, 'loadedmetadata', 10_000))) return null;
    const w = video.videoWidth;
    const h = video.videoHeight;
    if (!w || !h) return null;
    const length = Number.isFinite(video.duration) ? { l: Math.round(video.duration) } : {};
    // A frame a moment in (the very first is often black). Some browsers
    // (iPhone) only decode frames while playing: muted, that's allowed.
    let frame = {};
    try {
      video.currentTime = Number.isFinite(video.duration) ? Math.min(0.5, video.duration / 2) : 0;
      await happens(video, 'seeked', 3000);
      if (video.readyState < 2) {
        void video.play().catch(() => {});
        await happens(video, 'timeupdate', 3000);
        video.pause();
      }
      if (video.readyState >= 2) frame = await preview(video);
    } catch {
      // No preview, then.
    }
    return { w, h, ...length, ...frame };
  } finally {
    video.removeAttribute('src');
    video.load();
    URL.revokeObjectURL(url);
  }
}

/**
 * A song or other audio file, as it is, with its length. If this browser
 * can't read it, it goes as a file.
 */
export async function prepareAudio(file: File): Promise<Outgoing> {
  if (file.size > MAX_FILE_BYTES) throw new TooBig();
  const length = await probeAudio(file);
  const out = await prepareFile(file);
  if (length === null) return out;
  return { file: out.file, media: { ...out.media, t: 'audio', ...(length !== undefined ? { l: length } : {}) } };
}

/** An audio file's length in whole seconds (undefined if unknown); null if this browser can't open it. */
async function probeAudio(file: Blob): Promise<number | undefined | null> {
  const url = URL.createObjectURL(file);
  const audio = document.createElement('audio');
  audio.preload = 'metadata';
  try {
    audio.src = url;
    if (!(await happens(audio, 'loadedmetadata', 10_000))) return null;
    return Number.isFinite(audio.duration) ? Math.round(audio.duration) : undefined;
  } finally {
    audio.removeAttribute('src');
    audio.load();
    URL.revokeObjectURL(url);
  }
}

/** Waits for an element's event: true if it happened, false on an error or after `ms`. */
function happens(target: HTMLMediaElement, event: string, ms: number): Promise<boolean> {
  return new Promise((resolve) => {
    const done = (result: boolean) => {
      clearTimeout(timer);
      target.removeEventListener(event, ok);
      target.removeEventListener('error', fail);
      resolve(result);
    };
    const ok = () => done(true);
    const fail = () => done(false);
    const timer = setTimeout(fail, ms);
    target.addEventListener(event, ok, { once: true });
    target.addEventListener('error', fail, { once: true });
  });
}

/** Any file, untouched. */
export async function prepareFile(file: File): Promise<Outgoing> {
  if (file.size > MAX_FILE_BYTES) throw new TooBig();
  if (file.size === 0) throw new Error('empty file');
  const bytes = new Uint8Array(await file.arrayBuffer());
  const type = /^[\w.+-]+\/[\w.+-]+$/.test(file.type) && file.type.length <= 100 ? file.type : null;
  return { file: bytes, media: { t: 'file', s: bytes.length, n: (file.name || 'file').slice(0, 255), ...(type ? { m: type } : {}) } };
}

/** What a picked file can be sent as, besides a file: a photo, a video, a GIF or music. */
export function showableAs(file: File): 'photo' | 'video' | 'gif' | 'audio' | null {
  if (/^image\/(jpeg|png|webp|heic|heif|avif|bmp)$/.test(file.type)) return 'photo';
  if (file.type === 'image/gif') return 'gif';
  if (/^video\/(mp4|quicktime|webm|x-m4v|3gpp)$/.test(file.type)) return 'video';
  if (/^audio\/(mpeg|mp3|mp4|x-m4a|aac|x-aac|ogg|opus|flac|x-flac|wav|x-wav|wave|webm)$/.test(file.type)) return 'audio';
  return null;
}
