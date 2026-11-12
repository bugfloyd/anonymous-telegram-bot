// Removes where and when a video was recorded, before it's sent "as video"
// (photos are redrawn instead, which drops their metadata). Phones keep the
// location in MP4/QuickTime metadata boxes: moov/udta ("©xyz", Android and
// others) and moov/meta ("com.apple.quicktime.location.ISO6709", iPhone),
// next to the make, model and software; editing apps add XMP in a "uuid"
// box. The recording time is also in the movie, track and media headers
// (mvhd, tkhd, mdhd).
//
// Metadata boxes become "free" boxes full of zeros and the header times are
// zeroed, all in place: the file keeps its exact length, so every offset in
// it stays right, and players skip "free" boxes. "As file" sends the
// original bytes, as Telegram does.

/** How an MP4 (ftyp) or an older QuickTime file starts. */
const FIRST = new Set(['ftyp', 'wide', 'free', 'skip', 'mdat', 'moov', 'pnot']);
/** The "uuid" box type Adobe's XMP metadata uses. */
const XMP = [0xbe, 0x7a, 0xcf, 0xcb, 0x97, 0xa9, 0x42, 0xe8, 0x9c, 0x71, 0x99, 0x94, 0x91, 0xe3, 0xaf, 0xac];
/** Boxes to look inside: the movie, its tracks, and their media. */
const CONTAINERS = new Set(['moov', 'trak', 'mdia']);
/** Metadata boxes, dropped wherever they are on the way. */
const METADATA = new Set(['udta', 'meta']);
/** Headers with a creation and modification time. */
const HEADERS = new Set(['mvhd', 'tkhd', 'mdhd']);

const FREE = [0x66, 0x72, 0x65, 0x65]; // "free"

class Unreadable extends Error {}

/**
 * Strips location, device and time metadata from an MP4 or QuickTime file,
 * in place. Returns false (and changes nothing) if the bytes aren't one.
 */
export function stripVideoMetadata(bytes: Uint8Array): boolean {
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  const edits: (() => void)[] = [];
  try {
    const top = boxes(bytes, view, 0, bytes.length);
    if (!top[0] || !FIRST.has(top[0].type) || !top.some((b) => b.type === 'moov')) return false;
    for (const box of top) visit(bytes, view, box, edits);
  } catch (err) {
    if (err instanceof Unreadable) return false;
    throw err;
  }
  for (const edit of edits) edit();
  return true;
}

interface Box {
  type: string;
  /** Where the box starts, where its contents start, and where it ends. */
  start: number;
  body: number;
  end: number;
}

function visit(bytes: Uint8Array, view: DataView, box: Box, edits: (() => void)[]): void {
  if (METADATA.has(box.type) || (box.type === 'uuid' && box.body + 16 <= box.end && XMP.every((b, i) => bytes[box.body + i] === b))) {
    edits.push(() => {
      bytes.set(FREE, box.start + 4);
      bytes.fill(0, box.body, box.end);
    });
  } else if (HEADERS.has(box.type)) {
    // version (1) and flags (3), then creation and modification times: 4 bytes each, or 8 in version 1.
    const length = bytes[box.body] === 1 ? 16 : 8;
    if (box.body + 4 + length > box.end) throw new Unreadable();
    edits.push(() => bytes.fill(0, box.body + 4, box.body + 4 + length));
  } else if (CONTAINERS.has(box.type)) {
    for (const child of boxes(bytes, view, box.body, box.end)) visit(bytes, view, child, edits);
  }
}

/** The boxes between start and end; throws Unreadable if they don't add up. */
function boxes(bytes: Uint8Array, view: DataView, start: number, end: number): Box[] {
  const found: Box[] = [];
  let at = start;
  while (at < end) {
    if (at + 8 > end) throw new Unreadable();
    let size = view.getUint32(at);
    const type = String.fromCharCode(...bytes.subarray(at + 4, at + 8));
    let header = 8;
    if (size === 1) {
      // A 64-bit size follows the type.
      if (at + 16 > end) throw new Unreadable();
      size = view.getUint32(at + 8) * 2 ** 32 + view.getUint32(at + 12);
      header = 16;
    } else if (size === 0) {
      // To the end of the file (only the last box).
      size = end - at;
    }
    if (size < header || at + size > end) throw new Unreadable();
    found.push({ type, start: at, body: at + header, end: at + size });
    at += size;
  }
  return found;
}
