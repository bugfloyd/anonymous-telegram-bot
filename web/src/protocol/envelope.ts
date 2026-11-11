// The envelope: all the server sees of a message (design §10, docs/protocol.md).
//
//   envelope = suite (1) ‖ wrap count (1) ‖ wraps ‖ body
//   wrap     = key ID (8) ‖ HPKE enc (65) ‖ wrapped content key (48)
//   body     = nonce (12) ‖ AES-256-GCM(content key, padded inner message)
//
// The content key is random per envelope and wrapped to each recipient key
// with single-shot HPKE (hpke.ts). A wrap's info includes the SHA-256 of the
// body, so it can't be moved to another message. Messages to a peer carry one
// wrap; the list allows per-device keys later without a format change.

import { concat, randomBytes } from '../crypto/encoding';
import { open, seal } from '../crypto/hpke';
import { SUITE } from '../crypto/keys';
import { message } from '../crypto/signing';

export const LABEL_KEY_ID = 'incognitochat/v2/key-id';
export const LABEL_WRAP = 'incognitochat/v2/wrap';
export const LABEL_BODY = 'incognitochat/v2/body';

export const KEY_ID_LENGTH = 8;
const ENC_LENGTH = 65;
const WRAPPED_KEY_LENGTH = 32 + 16;
export const WRAP_LENGTH = KEY_ID_LENGTH + ENC_LENGTH + WRAPPED_KEY_LENGTH;
export const MAX_WRAPS = 16;
const NONCE_LENGTH = 12;
const TAG_LENGTH = 16;

/** Padded sizes of the inner message: every message is one of these long. */
export const PADDED_SIZES = [512, 2048, 8192, 16384] as const;
const LARGEST = PADDED_SIZES[3];
/** The most bytes an inner message can have before padding. */
export const MAX_INNER = LARGEST - 4;
/** The largest envelope there can be. */
export const MAX_ENVELOPE = 2 + MAX_WRAPS * WRAP_LENGTH + NONCE_LENGTH + LARGEST + TAG_LENGTH;

export class EnvelopeError extends Error {
  constructor(reason: string) {
    super(`envelope: ${reason}`);
    this.name = 'EnvelopeError';
  }
}

/** A recipient key's ID: the first 8 bytes of SHA-256 over its labelled point. */
export async function keyId(publicKey: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const digest = await crypto.subtle.digest('SHA-256', message(LABEL_KEY_ID, new Uint8Array([SUITE]), publicKey));
  return new Uint8Array(digest).slice(0, KEY_ID_LENGTH);
}

/** Pads to the next size in PADDED_SIZES: a 4-byte length, the bytes, zeros. */
export function pad(inner: Uint8Array): Uint8Array<ArrayBuffer> {
  if (inner.length > MAX_INNER) throw new EnvelopeError('message too long');
  const size = PADDED_SIZES.find((s) => s >= inner.length + 4)!;
  const out = new Uint8Array(size);
  new DataView(out.buffer).setUint32(0, inner.length);
  out.set(inner, 4);
  return out;
}

export function unpad(padded: Uint8Array): Uint8Array<ArrayBuffer> {
  if (!(PADDED_SIZES as readonly number[]).includes(padded.length)) throw new EnvelopeError('bad padded size');
  const length = new DataView(padded.buffer, padded.byteOffset).getUint32(0);
  if (length > padded.length - 4) throw new EnvelopeError('bad length');
  for (let i = 4 + length; i < padded.length; i++) if (padded[i] !== 0) throw new EnvelopeError('bad padding');
  return padded.slice(4, 4 + length);
}

async function aes(key: Uint8Array<ArrayBuffer>, usage: 'encrypt' | 'decrypt'): Promise<CryptoKey> {
  return crypto.subtle.importKey('raw', key, { name: 'AES-GCM' }, false, [usage]);
}

const bodyAad = () => message(LABEL_BODY, new Uint8Array([SUITE]));

async function wrapInfo(id: Uint8Array, body: Uint8Array<ArrayBuffer>): Promise<Uint8Array<ArrayBuffer>> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', body));
  return message(LABEL_WRAP, new Uint8Array([SUITE]), id, digest);
}

/** Encrypts an (unpadded) inner message to one or more recipient public keys. */
export async function sealEnvelope(recipients: Uint8Array<ArrayBuffer>[], inner: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  if (recipients.length === 0 || recipients.length > MAX_WRAPS) throw new EnvelopeError('bad recipient count');
  const contentKey = randomBytes(32);
  const nonce = randomBytes(NONCE_LENGTH);
  const ciphertext = await crypto.subtle.encrypt({ name: 'AES-GCM', iv: nonce, additionalData: bodyAad() }, await aes(contentKey, 'encrypt'), pad(inner));
  const body = concat(nonce, new Uint8Array(ciphertext));
  const wraps: Uint8Array[] = [];
  for (const pk of recipients) {
    const id = await keyId(pk);
    const { enc, ct } = await seal(pk, await wrapInfo(id, body), new Uint8Array(0), contentKey);
    wraps.push(id, enc, ct);
  }
  return concat(new Uint8Array([SUITE, recipients.length]), ...wraps, body);
}

/** A private key that may open envelopes, found by its key ID. */
export interface RecipientKey {
  privateKey: CryptoKey;
  publicKey: Uint8Array;
}

export interface ParsedEnvelope {
  wraps: { keyId: Uint8Array<ArrayBuffer>; enc: Uint8Array<ArrayBuffer>; ct: Uint8Array<ArrayBuffer> }[];
  body: Uint8Array<ArrayBuffer>;
}

/** Splits an envelope into wraps and body, checking only the layout. */
export function parseEnvelope(envelope: Uint8Array): ParsedEnvelope {
  if (envelope.length < 2 || envelope.length > MAX_ENVELOPE) throw new EnvelopeError('bad size');
  if (envelope[0] !== SUITE) throw new EnvelopeError('unknown suite');
  const count = envelope[1]!;
  if (count === 0 || count > MAX_WRAPS) throw new EnvelopeError('bad wrap count');
  const bodyStart = 2 + count * WRAP_LENGTH;
  const bodyLength = envelope.length - bodyStart;
  if (!PADDED_SIZES.some((s) => bodyLength === NONCE_LENGTH + s + TAG_LENGTH)) throw new EnvelopeError('bad body size');
  const wraps: ParsedEnvelope['wraps'] = [];
  for (let i = 0; i < count; i++) {
    const at = 2 + i * WRAP_LENGTH;
    wraps.push({
      keyId: envelope.slice(at, at + KEY_ID_LENGTH),
      enc: envelope.slice(at + KEY_ID_LENGTH, at + KEY_ID_LENGTH + ENC_LENGTH),
      ct: envelope.slice(at + KEY_ID_LENGTH + ENC_LENGTH, at + WRAP_LENGTH),
    });
  }
  return { wraps, body: envelope.slice(bodyStart) };
}

/**
 * Opens an envelope with whichever of our keys it is wrapped to. Returns the
 * inner message and the ID of the key that opened it. Throws EnvelopeError if
 * no key fits or anything was changed.
 */
export async function openEnvelope(
  envelope: Uint8Array,
  findKey: (keyId: Uint8Array) => Promise<RecipientKey | undefined>,
): Promise<{ inner: Uint8Array<ArrayBuffer>; keyId: Uint8Array<ArrayBuffer>; key: RecipientKey }> {
  const { wraps, body } = parseEnvelope(envelope);
  for (const wrap of wraps) {
    const key = await findKey(wrap.keyId);
    if (!key) continue;
    let contentKey: Uint8Array<ArrayBuffer>;
    try {
      contentKey = await open(wrap.enc, key.privateKey, key.publicKey, await wrapInfo(wrap.keyId, body), new Uint8Array(0), wrap.ct);
    } catch {
      throw new EnvelopeError('wrap does not open');
    }
    try {
      const padded = await crypto.subtle.decrypt(
        { name: 'AES-GCM', iv: body.slice(0, NONCE_LENGTH), additionalData: bodyAad() },
        await aes(contentKey, 'decrypt'),
        body.slice(NONCE_LENGTH),
      );
      return { inner: unpad(new Uint8Array(padded)), keyId: wrap.keyId, key };
    } catch (err) {
      if (err instanceof EnvelopeError) throw err;
      throw new EnvelopeError('body does not open');
    }
  }
  throw new EnvelopeError('no key for this envelope');
}
