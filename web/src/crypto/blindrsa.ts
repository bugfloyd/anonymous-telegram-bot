// RSA blind signatures, client side (RFC 9474), variant
// RSABSSA-SHA384-PSS-Deterministic, as Privacy Pass token type 2 uses.
//
// The client blinds a message, the server signs it without seeing it, and the
// client unblinds the signature. The result is an ordinary RSA-PSS signature
// (SHA-384, 48-byte salt) that WebCrypto verifies, and that the server can't
// link to the request it signed. Checked with the RFC's vector
// (blindrsa-vectors.json).
//
// Only public-key operations run here (r^e with the small public exponent,
// and multiplications), so BigInt is fine; WebCrypto verifies the result.

const HASH = 'SHA-384';
const H_LEN = 48;
export const SALT_LENGTH = 48;

export interface IssuerKey {
  /** WebCrypto RSA-PSS key, for verifying. */
  key: CryptoKey;
  n: bigint;
  e: bigint;
  /** Modulus length in bytes. */
  k: number;
}

export function os2ip(bytes: Uint8Array): bigint {
  let x = 0n;
  for (const b of bytes) x = (x << 8n) | BigInt(b);
  return x;
}

export function i2osp(x: bigint, length: number): Uint8Array<ArrayBuffer> {
  const out = new Uint8Array(length);
  for (let i = length - 1; i >= 0; i--, x >>= 8n) out[i] = Number(x & 0xffn);
  if (x !== 0n) throw new Error('integer too large');
  return out;
}

export function modPow(base: bigint, exp: bigint, mod: bigint): bigint {
  let result = 1n;
  base %= mod;
  while (exp > 0n) {
    if (exp & 1n) result = (result * base) % mod;
    base = (base * base) % mod;
    exp >>= 1n;
  }
  return result;
}

export function modInverse(a: bigint, m: bigint): bigint {
  let [oldR, r] = [a % m, m];
  let [oldS, s] = [1n, 0n];
  while (r !== 0n) {
    const q = oldR / r;
    [oldR, r] = [r, oldR - q * r];
    [oldS, s] = [s, oldS - q * s];
  }
  if (oldR !== 1n) throw new Error('not invertible');
  return ((oldS % m) + m) % m;
}

function gcd(a: bigint, b: bigint): bigint {
  while (b !== 0n) [a, b] = [b, a % b];
  return a;
}

async function sha384(data: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  return new Uint8Array(await crypto.subtle.digest(HASH, Uint8Array.from(data)));
}

async function mgf1(seed: Uint8Array, length: number): Promise<Uint8Array<ArrayBuffer>> {
  const out = new Uint8Array(length);
  for (let counter = 0, filled = 0; filled < length; counter++, filled += H_LEN) {
    const c = new Uint8Array([counter >>> 24, (counter >>> 16) & 0xff, (counter >>> 8) & 0xff, counter & 0xff]);
    const block = await sha384(Uint8Array.from([...seed, ...c]));
    out.set(block.subarray(0, Math.min(H_LEN, length - filled)), filled);
  }
  return out;
}

/** EMSA-PSS-ENCODE (RFC 8017 §9.1.1) with SHA-384 and MGF1-SHA-384. */
export async function emsaPssEncode(message: Uint8Array, emBits: number, salt: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const mHash = await sha384(message);
  const emLen = Math.ceil(emBits / 8);
  if (emLen < H_LEN + salt.length + 2) throw new Error('encoding error');
  const h = await sha384(Uint8Array.from([...new Uint8Array(8), ...mHash, ...salt]));
  const db = new Uint8Array(emLen - H_LEN - 1);
  db[db.length - salt.length - 1] = 0x01;
  db.set(salt, db.length - salt.length);
  const mask = await mgf1(h, db.length);
  for (let i = 0; i < db.length; i++) db[i]! ^= mask[i]!;
  db[0]! &= 0xff >>> (8 * emLen - emBits);
  return Uint8Array.from([...db, ...h, 0xbc]);
}

/** Imports the issuer's public key (SPKI DER). */
export async function importIssuerKey(spki: Uint8Array<ArrayBuffer>): Promise<IssuerKey> {
  const key = await crypto.subtle.importKey('spki', spki, { name: 'RSA-PSS', hash: HASH }, true, ['verify']);
  const jwk = await crypto.subtle.exportKey('jwk', key);
  const b64 = (s: string) => Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
  const nBytes = b64(jwk.n!);
  return { key, n: os2ip(nBytes), e: os2ip(b64(jwk.e!)), k: nBytes.length };
}

export interface BlindState {
  /** r⁻¹ mod n, kept to unblind the signature. */
  inv: bigint;
}

function randomUnit(n: bigint, k: number): bigint {
  for (;;) {
    const r = os2ip(crypto.getRandomValues(new Uint8Array(k))) % n;
    if (r > 1n && gcd(r, n) === 1n) return r;
  }
}

/**
 * Blind (RFC 9474 §4.2) a message for the issuer to sign. salt and inv are
 * only given by tests; normally both are random.
 */
export async function blind(
  pk: Pick<IssuerKey, 'n' | 'e' | 'k'>,
  message: Uint8Array,
  test?: { salt: Uint8Array; inv: bigint },
): Promise<{ blinded: Uint8Array<ArrayBuffer>; state: BlindState }> {
  const modBits = pk.n.toString(2).length;
  const salt = test?.salt ?? crypto.getRandomValues(new Uint8Array(SALT_LENGTH));
  const m = os2ip(await emsaPssEncode(message, modBits - 1, salt));
  if (gcd(m, pk.n) !== 1n) throw new Error('invalid input');
  const r = test ? modInverse(test.inv, pk.n) : randomUnit(pk.n, pk.k);
  const inv = test?.inv ?? modInverse(r, pk.n);
  const z = (m * modPow(r, pk.e, pk.n)) % pk.n;
  return { blinded: i2osp(z, pk.k), state: { inv } };
}

/** Unblinds the issuer's signature (RFC 9474 §4.4) and checks it. */
export async function finalize(pk: IssuerKey, message: Uint8Array, blindSignature: Uint8Array, state: BlindState): Promise<Uint8Array<ArrayBuffer>> {
  if (blindSignature.length !== pk.k) throw new Error('bad blind signature');
  const z = os2ip(blindSignature);
  if (z >= pk.n) throw new Error('bad blind signature');
  const signature = i2osp((z * state.inv) % pk.n, pk.k);
  if (!(await verify(pk, message, signature))) throw new Error('signature does not verify');
  return signature;
}

/** Verifies an unblinded signature: plain RSA-PSS, SHA-384, 48-byte salt. */
export function verify(pk: Pick<IssuerKey, 'key'>, message: Uint8Array, signature: Uint8Array): Promise<boolean> {
  return crypto.subtle.verify({ name: 'RSA-PSS', saltLength: SALT_LENGTH }, pk.key, Uint8Array.from(signature), Uint8Array.from(message));
}
