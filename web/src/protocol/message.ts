// The inner message: what only the two people in a conversation can read
// (design §10, docs/protocol.md).
//
//   inner = tag (32) ‖ UTF-8 JSON
//
// The tag is HMAC-SHA256 over the exact JSON bytes, with a key derived from
// ECDH(sender's conversation key, recipient's key) and both public keys. The
// recipient could have computed it too, so a message can't be proven to anyone
// else (deniable); the server holds neither key, so it can't forge one. The
// message names the key it was written for (`to`), so it can't be re-addressed.

import { concat, fromBase64Url, toBase64Url, utf8 } from '../crypto/encoding';
import { importAgreementPublicKey, SUITE } from '../crypto/keys';
import { message as labelled } from '../crypto/signing';
import { keyId, MAX_INNER, openEnvelope, sealEnvelope, type RecipientKey } from './envelope';

export const LABEL_AUTH = 'incognitochat/v2/message-auth';
export const PROTOCOL_VERSION = 1;
const TAG_LENGTH = 32;
/** Room left for everything but the text. */
export const MAX_TEXT_BYTES = 12_000;
const MAX_SEEN = 100;

export type Kind = 'message' | 'seen' | 'key_update' | 'end';
const KINDS: readonly Kind[] = ['message', 'seen', 'key_update', 'end'];

/**
 * What an attachment is shown as: music is `audio`, a round video message
 * `round`. Kinds this version doesn't know are accepted and shown as files,
 * so a newer sender's attachments still arrive (see shownAs).
 */
export type MediaKind = 'photo' | 'video' | 'gif' | 'voice' | 'audio' | 'round' | 'file';
const MEDIA_KINDS: readonly MediaKind[] = ['photo', 'video', 'gif', 'voice', 'audio', 'round', 'file'];
/** Most waveform bars a voice message carries. */
export const MAX_WAVEFORM = 100;
/** Longest video, voice or music length, in seconds. */
const MAX_LENGTH = 24 * 60 * 60;
/** Largest file that can be attached. */
export const MAX_FILE_BYTES = 25 * 1024 * 1024;
const MAX_PREVIEW = 3000;

/**
 * An attachment: the encrypted file is uploaded separately (docs/protocol.md,
 * Media); the message carries how to fetch, open and delete it, and what it is.
 */
export interface Media {
  /** A MediaKind, or a newer kind this version shows as a file. */
  t: string;
  /** Upload ID (16 bytes). */
  id: string;
  /** The file's AES-256-GCM key (32 bytes). */
  k: string;
  /** Delete secret (16 bytes): the recipient deletes the upload with it once stored. */
  d: string;
  /** The file's size in bytes, before padding. */
  s: number;
  /** File name. */
  n?: string;
  /** MIME type. */
  m?: string;
  /** Pixel width and height (photos, videos, GIFs, video messages). */
  w?: number;
  h?: number;
  /** Length in whole seconds (videos, voice, music, video messages). */
  l?: number;
  /** Voice: the waveform, one byte (0–255) per bar, base64url. */
  v?: string;
  /** A tiny JPEG preview, base64url, shown until the file is here. */
  p?: string;
}

export interface Message {
  v: typeof PROTOCOL_VERSION;
  kind: Kind;
  /** Conversation ID, 16 random bytes. */
  conv: string;
  /** Message ID, 16 random bytes. */
  id: string;
  /** Sent time, Unix milliseconds, from the sender's clock. */
  at: number;
  /** The sender's conversation public key (65 bytes); a new one means the sender rotated. */
  from: string;
  /** The ID of the recipient key this was written for (8 bytes). */
  to: string;
  /** Where to write back: a mailbox ID (16 bytes), on first messages or when it changes. */
  reply?: string;
  /** Sender tag (32 bytes), on first messages to a link: lets the owner block a person, not just a conversation. */
  tag?: string;
  /** The ID of the last message received from the other side, so drops show. */
  last?: string;
  text?: string;
  /** The message this one answers. */
  replyTo?: string;
  /** For kind "seen": the IDs of messages read. */
  seen?: string[];
  /** An attachment (kind "message"). */
  media?: Media;
}

/** What a sender fills in; the protocol fields are added when sealing. */
export type Draft = Omit<Message, 'v' | 'from' | 'to'>;

export class MessageError extends Error {
  constructor(reason: string) {
    super(`message: ${reason}`);
    this.name = 'MessageError';
  }
}

/** A conversation key: ECDH, with its 65-byte public point. */
export interface ConversationKey {
  privateKey: CryptoKey;
  publicKey: Uint8Array<ArrayBuffer>;
}

/** 16 random bytes, as a conversation or message ID. */
export function newId(): string {
  return toBase64Url(crypto.getRandomValues(new Uint8Array(16)));
}

async function authKey(privateKey: CryptoKey, peer: Uint8Array<ArrayBuffer>, senderKey: Uint8Array, recipientKey: Uint8Array): Promise<CryptoKey> {
  const shared = await crypto.subtle.deriveBits({ name: 'ECDH', public: await importAgreementPublicKey(peer) }, privateKey, 256);
  const ikm = await crypto.subtle.importKey('raw', shared, 'HKDF', false, ['deriveKey']);
  return crypto.subtle.deriveKey(
    { name: 'HKDF', hash: 'SHA-256', salt: new Uint8Array(0), info: labelled(LABEL_AUTH, new Uint8Array([SUITE]), senderKey, recipientKey) },
    ikm,
    { name: 'HMAC', hash: 'SHA-256', length: 256 },
    false,
    ['sign', 'verify'],
  );
}

/** Seals a message from our conversation key to the recipient's public key. */
export async function sealMessage(draft: Draft, sender: ConversationKey, recipient: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  const full: Message = {
    v: PROTOCOL_VERSION,
    ...draft,
    from: toBase64Url(sender.publicKey),
    to: toBase64Url(await keyId(recipient)),
  };
  checkMessage(full);
  const json = utf8(JSON.stringify(full));
  if (TAG_LENGTH + json.length > MAX_INNER) throw new MessageError('too long');
  const key = await authKey(sender.privateKey, recipient, sender.publicKey, recipient);
  const tag = new Uint8Array(await crypto.subtle.sign('HMAC', key, json));
  return sealEnvelope([recipient], concat(tag, json));
}

/**
 * Opens an envelope with one of our keys and checks the message's tag.
 * Throws EnvelopeError or MessageError if anything is wrong.
 */
export async function openMessage(
  envelope: Uint8Array,
  findKey: (keyId: Uint8Array) => Promise<RecipientKey | undefined>,
): Promise<{ message: Message; keyId: Uint8Array<ArrayBuffer> }> {
  const { inner, keyId: opened, key } = await openEnvelope(envelope, findKey);
  if (inner.length <= TAG_LENGTH) throw new MessageError('too short');
  const tag = inner.slice(0, TAG_LENGTH);
  const json = inner.slice(TAG_LENGTH);
  let parsed: unknown;
  try {
    parsed = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(json));
  } catch {
    throw new MessageError('not JSON');
  }
  const message = checkMessage(parsed);
  if (message.to !== toBase64Url(opened)) throw new MessageError('written for another key');
  const sender = fromBase64Url(message.from);
  let auth: CryptoKey;
  try {
    auth = await authKey(key.privateKey, sender, sender, Uint8Array.from(key.publicKey));
  } catch {
    throw new MessageError('bad sender key');
  }
  if (!(await crypto.subtle.verify('HMAC', auth, tag, json))) throw new MessageError('bad tag');
  return { message, keyId: opened };
}

/** Checks a message's fields; returns it typed. Unknown fields are ignored. */
export function checkMessage(value: unknown): Message {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new MessageError('not an object');
  const m = value as Record<string, unknown>;
  if (m.v !== PROTOCOL_VERSION) throw new MessageError('unknown version');
  if (typeof m.kind !== 'string' || !KINDS.includes(m.kind as Kind)) throw new MessageError('unknown kind');
  bytes(m.conv, 16, 'conv');
  bytes(m.id, 16, 'id');
  if (!Number.isSafeInteger(m.at) || (m.at as number) <= 0) throw new MessageError('bad time');
  bytes(m.from, 65, 'from');
  bytes(m.to, 8, 'to');
  if (m.reply !== undefined) bytes(m.reply, 16, 'reply');
  if (m.tag !== undefined) bytes(m.tag, 32, 'tag');
  if (m.last !== undefined) bytes(m.last, 16, 'last');
  if (m.replyTo !== undefined) bytes(m.replyTo, 16, 'replyTo');
  if (m.text !== undefined && (typeof m.text !== 'string' || utf8(m.text).length > MAX_TEXT_BYTES)) throw new MessageError('bad text');
  if (m.seen !== undefined) {
    if (!Array.isArray(m.seen) || m.seen.length === 0 || m.seen.length > MAX_SEEN) throw new MessageError('bad seen');
    m.seen.forEach((s) => bytes(s, 16, 'seen'));
  }
  if (m.media !== undefined) checkMedia(m.media);
  switch (m.kind) {
    case 'message':
      // Text, an attachment, or both (a caption).
      if (m.media === undefined && (typeof m.text !== 'string' || m.text.trim() === '')) throw new MessageError('empty message');
      break;
    case 'seen':
      if (m.seen === undefined || m.text !== undefined) throw new MessageError('bad seen');
      break;
    default:
      if (m.text !== undefined || m.seen !== undefined || m.media !== undefined) throw new MessageError(`bad ${m.kind}`);
  }
  if (m.kind === 'seen' && m.media !== undefined) throw new MessageError('bad seen');
  return m as unknown as Message;
}

function checkMedia(value: unknown): void {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new MessageError('bad media');
  const a = value as Record<string, unknown>;
  if (typeof a.t !== 'string' || !/^[a-z_]{1,16}$/.test(a.t)) throw new MessageError('bad media kind');
  bytes(a.id, 16, 'media id');
  bytes(a.k, 32, 'media key');
  bytes(a.d, 16, 'media secret');
  if (!Number.isSafeInteger(a.s) || (a.s as number) < 1 || (a.s as number) > MAX_FILE_BYTES) throw new MessageError('bad media size');
  if (a.n !== undefined && (typeof a.n !== 'string' || a.n.length === 0 || a.n.length > 255)) throw new MessageError('bad media name');
  if (a.m !== undefined && (typeof a.m !== 'string' || !/^[\w.+-]+\/[\w.+-]+$/.test(a.m) || a.m.length > 100)) throw new MessageError('bad media type');
  for (const dim of ['w', 'h'] as const) {
    if (a[dim] !== undefined && (!Number.isSafeInteger(a[dim]) || (a[dim] as number) < 1 || (a[dim] as number) > 20_000)) throw new MessageError('bad media size');
  }
  if (a.l !== undefined && (!Number.isSafeInteger(a.l) || (a.l as number) < 0 || (a.l as number) > MAX_LENGTH)) throw new MessageError('bad media length');
  if (a.v !== undefined) {
    let bars: Uint8Array;
    try {
      if (typeof a.v !== 'string' || !/^[A-Za-z0-9_-]+$/.test(a.v)) throw new Error();
      bars = fromBase64Url(a.v);
    } catch {
      throw new MessageError('bad media waveform');
    }
    if (bars.length === 0 || bars.length > MAX_WAVEFORM || toBase64Url(bars) !== a.v) throw new MessageError('bad media waveform');
  }
  if (a.p !== undefined && (typeof a.p !== 'string' || a.p.length > MAX_PREVIEW || !/^[A-Za-z0-9_-]+$/.test(a.p))) throw new MessageError('bad media preview');
}

/** How an attachment is shown: its kind, or as a file if this version doesn't know the kind. */
export function shownAs(media: { t: string }): MediaKind {
  return MEDIA_KINDS.includes(media.t as MediaKind) ? (media.t as MediaKind) : 'file';
}

const EXTENSIONS: Record<string, string> = {
  'image/jpeg': 'jpg',
  'image/gif': 'gif',
  'video/mp4': 'mp4',
  'video/quicktime': 'mov',
  'video/webm': 'webm',
  'audio/mp4': 'm4a',
  'audio/webm': 'webm',
  'audio/ogg': 'ogg',
  'audio/mpeg': 'mp3',
};

/** The name to save an attachment under: its own, or one made from its kind and type ("video.mp4"). */
export function fileName(media: { t: string; n?: string; m?: string }): string {
  if (media.n) return media.n;
  const base = { photo: 'photo', video: 'video', gif: 'animation', voice: 'voice', audio: 'audio', round: 'video', file: 'file' }[shownAs(media)];
  const ext = media.m ? EXTENSIONS[media.m] : undefined;
  return ext ? `${base}.${ext}` : base;
}

/** Throws unless value is the canonical unpadded base64url of `length` bytes. */
function bytes(value: unknown, length: number, field: string): void {
  if (typeof value !== 'string' || !/^[A-Za-z0-9_-]+$/.test(value)) throw new MessageError(`bad ${field}`);
  let decoded: Uint8Array;
  try {
    decoded = fromBase64Url(value);
  } catch {
    throw new MessageError(`bad ${field}`);
  }
  if (decoded.length !== length || toBase64Url(decoded) !== value) throw new MessageError(`bad ${field}`);
}
