// The account on this device: setting it up, registering it, logging in.
//
// Setup happens in two launches (design §7). The first creates the keys and
// asks Telegram to reopen the app with "reg" + the account key's fingerprint
// as its start parameter. Only Telegram sets that parameter, so the second
// launch's signed launch data proves the Telegram user chose this account
// key, and that launch registers it.
//
// Keys on this device:
// - account key: copyable (sealed in the vault for linking devices later),
//   used through a non-extractable working copy;
// - device signing key: non-extractable, never leaves this device;
// - vault key: non-extractable, seals the copyable keys.

import { randomBytes, toBase64Url, utf8 } from '../crypto/encoding';
import { exportPublicKey, fingerprint, generateCopyableSigningKey, generateSigningKey, SIGNING_IMPORT } from '../crypto/keys';
import { LABEL_REGISTER, LABEL_REQUEST, LABEL_SESSION, sign, signEncoded } from '../crypto/signing';
import { generateVaultKey, seal, unseal } from '../crypto/vault';
import { ApiError, checkTokenResponse, type Api, type TokenResponse } from '../net/api';
import { encodeDeviceList, firstList } from '../protocol/device-list';
import { deleteMeta, getKeyPair, getMeta, putKey, putMeta, putSealed, type Database, type StoredKeyPair, wipe } from '../storage/db';
import { withLock } from '../sync/locks';

/** The start parameter prefix of a registration launch. */
export const REGISTRATION_PREFIX = 'reg';

const META_ACCOUNT = 'account';
const META_PENDING_LINK = 'pending-link';
/** Set when someone chose to start over on this device, so the setup that follows asks to confirm. */
export const META_START_OVER = 'start-over';
const KEY_ACCOUNT = 'account-key';
const KEY_DEVICE = 'device-signing-key';
const KEY_VAULT = 'vault-key';
const LOCK = 'account';

const ID = /^[A-Za-z0-9_-]{22}$/;
const FINGERPRINT = /^[A-Za-z0-9_-]{43}$/;
const LINK_PARAM = /^[A-Za-z0-9_-]{1,64}$/;

export type AccountRecord =
  /** Keys exist; waiting for the registration launch. */
  | { state: 'pending'; deviceId: string; fingerprint: string }
  | { state: 'registered'; deviceId: string; fingerprint: string; accountId: string }
  /** The account was replaced by a registration elsewhere. */
  | { state: 'replaced'; deviceId: string; fingerprint: string; accountId: string };

/** This launch's start parameter isn't the registration this device prepared. */
export class SetupMismatch extends Error {
  constructor() {
    super('registration launch does not match this setup');
    this.name = 'SetupMismatch';
  }
}

/** Reads the account record, ignoring anything malformed. */
export async function readAccount(db: Database): Promise<AccountRecord | undefined> {
  const value = await getMeta<Record<string, unknown>>(db, META_ACCOUNT);
  if (!value || typeof value !== 'object') return undefined;
  const { state, deviceId, fingerprint: fp, accountId } = value;
  if (typeof deviceId !== 'string' || !ID.test(deviceId) || typeof fp !== 'string' || !FINGERPRINT.test(fp)) return undefined;
  if (state === 'pending') return { state, deviceId, fingerprint: fp };
  if ((state === 'registered' || state === 'replaced') && typeof accountId === 'string' && ID.test(accountId)) {
    return { state, deviceId, fingerprint: fp, accountId };
  }
  return undefined;
}

/**
 * Prepares a setup and returns the start parameter the registration launch
 * must carry. A pending setup is reused, so asking again reopens with the same
 * code; otherwise (no account, a replaced one, or with restart a registered
 * one the server no longer knows) new keys are created.
 */
export function prepareSetup(db: Database, options: { restart?: boolean } = {}): Promise<string> {
  return withLock(LOCK, async () => {
    const current = await readAccount(db);
    if (current?.state === 'registered' && !options.restart) throw new Error('already registered');
    if (current?.state === 'pending' && (await getKeyPair(db, KEY_ACCOUNT)) && (await getKeyPair(db, KEY_DEVICE))) {
      return REGISTRATION_PREFIX + current.fingerprint;
    }

    // A new account starts with nothing from an earlier one on this device
    // (its mailboxes and links are gone on the server), except a link opened
    // just before and the choice to start over.
    await wipe(db, [META_PENDING_LINK, META_START_OVER]);
    const vaultKey = await generateVaultKey();
    const account = await generateCopyableSigningKey();
    const sealed = await seal(vaultKey, account.privateKey, KEY_ACCOUNT);
    const working = await unseal(vaultKey, sealed, KEY_ACCOUNT, SIGNING_IMPORT, ['sign']);
    const device = await generateSigningKey();
    const deviceId = toBase64Url(randomBytes(16));
    const fp = await fingerprint(account.publicKey);

    await putKey(db, KEY_VAULT, vaultKey);
    await putSealed(db, KEY_ACCOUNT, sealed);
    await putKey(db, KEY_ACCOUNT, { privateKey: working, publicKey: account.publicKey });
    await putKey(db, KEY_DEVICE, { privateKey: device.privateKey, publicKey: device.publicKey });
    await putMeta(db, META_ACCOUNT, { state: 'pending', deviceId, fingerprint: fp } satisfies AccountRecord);
    return REGISTRATION_PREFIX + fp;
  });
}

/** The t.me path that reopens the app as a registration launch. */
export function registrationPath(botUsername: string, appName: string, startParam: string): string {
  return `/${botUsername}/${appName}?startapp=${startParam}`;
}

/** Whether a start parameter is a registration launch's. */
export function isRegistrationLaunch(startParam: string): boolean {
  return startParam.startsWith(REGISTRATION_PREFIX) && FINGERPRINT.test(startParam.slice(REGISTRATION_PREFIX.length));
}

/**
 * Registers the pending setup with this launch's initData. Throws
 * SetupMismatch if the launch isn't for this setup, and ApiError for the
 * server's refusals (account_exists means replace is needed).
 */
export function register(
  db: Database,
  api: Api,
  launch: { initData: string; startParam: string },
  options: { replace: boolean; now?: () => number },
): Promise<TokenResponse> {
  const now = options.now ?? (() => Math.floor(Date.now() / 1000));
  return withLock(LOCK, async () => {
    const current = await readAccount(db);
    if (current?.state !== 'pending' || launch.startParam !== REGISTRATION_PREFIX + current.fingerprint) {
      throw new SetupMismatch();
    }
    const account = await requirePair(db, KEY_ACCOUNT);
    const device = await requirePair(db, KEY_DEVICE);
    const accountKey = await exportPublicKey(account.publicKey);
    const deviceKey = await exportPublicKey(device.publicKey);

    const list = encodeDeviceList(firstList(current.deviceId, deviceKey, now()));
    const parts = await registerParts(launch.initData, accountKey, current.deviceId, deviceKey);
    const body = {
      initData: launch.initData,
      accountKey: toBase64Url(accountKey),
      device: current.deviceId,
      deviceKey: toBase64Url(deviceKey),
      deviceList: toBase64Url(list),
      deviceListSig: toBase64Url(await signEncoded(account.privateKey, list)),
      accountSig: toBase64Url(await sign(account.privateKey, LABEL_REGISTER, ...parts)),
      deviceSig: toBase64Url(await sign(device.privateKey, LABEL_REGISTER, ...parts)),
      replace: options.replace,
    };
    const result = await api.post('/register', body, checkTokenResponse);
    await putMeta(db, META_ACCOUNT, {
      state: 'registered',
      deviceId: current.deviceId,
      fingerprint: current.fingerprint,
      accountId: result.account,
    } satisfies AccountRecord);
    return result;
  });
}

/** The parts both keys sign to register (backend: api.RegisterParts). */
export async function registerParts(
  initData: string,
  accountKey: Uint8Array,
  deviceId: string,
  deviceKey: Uint8Array,
): Promise<Uint8Array[]> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', utf8(initData)));
  return [digest, accountKey, utf8(deviceId), deviceKey];
}

/** The parts a login signs (backend: session.LoginParts). */
export function loginParts(accountId: string, deviceId: string, timestamp: number, nonce: string): string[] {
  return [accountId, deviceId, String(timestamp), nonce];
}

/**
 * Logs this device in. If the account was replaced, records that and
 * rethrows the ApiError (code account_replaced).
 */
export async function logIn(db: Database, api: Api, now: () => number = () => Math.floor(Date.now() / 1000)): Promise<TokenResponse> {
  const current = await readAccount(db);
  if (current?.state !== 'registered') throw new Error('not registered');
  const device = await requirePair(db, KEY_DEVICE);
  const timestamp = now();
  const nonce = toBase64Url(randomBytes(16));
  const signature = await sign(device.privateKey, LABEL_SESSION, ...loginParts(current.accountId, current.deviceId, timestamp, nonce));
  try {
    return await api.post(
      '/session',
      { account: current.accountId, device: current.deviceId, timestamp, nonce, signature: toBase64Url(signature) },
      checkTokenResponse,
    );
  } catch (err) {
    if (err instanceof ApiError && err.code === 'account_replaced') await markReplaced(db, current.accountId);
    throw err;
  }
}

/** Records that the server says this account was replaced. */
export function markReplaced(db: Database, accountId: string): Promise<void> {
  return withLock(LOCK, async () => {
    const latest = await readAccount(db);
    if (latest?.state === 'registered' && latest.accountId === accountId) {
      await putMeta(db, META_ACCOUNT, { ...latest, state: 'replaced' } satisfies AccountRecord);
    }
  });
}

/** The parts a signed request covers (backend: session.RequestParts). */
export async function requestParts(
  accountId: string,
  deviceId: string,
  method: string,
  path: string,
  timestamp: number,
  nonce: string,
  body: string,
): Promise<(string | Uint8Array)[]> {
  const digest = new Uint8Array(await crypto.subtle.digest('SHA-256', utf8(body)));
  return [accountId, deviceId, method, path, String(timestamp), nonce, digest];
}

/**
 * The X-Device-Signature header for a sensitive request: this device signs
 * the method, path, time, a fresh nonce and the exact body text.
 */
export async function signRequest(
  db: Database,
  method: string,
  path: string,
  body: string,
  now: () => number = () => Math.floor(Date.now() / 1000),
): Promise<string> {
  const current = await readAccount(db);
  if (current?.state !== 'registered') throw new Error('not registered');
  const device = await requirePair(db, KEY_DEVICE);
  const timestamp = now();
  const nonce = toBase64Url(randomBytes(16));
  const parts = await requestParts(current.accountId, current.deviceId, method, path, timestamp, nonce, body);
  const signature = await sign(device.privateKey, LABEL_REQUEST, ...parts);
  return `${timestamp}.${nonce}.${toBase64Url(signature)}`;
}

/**
 * Remembers a link this launch was opened with, so the app can continue
 * there once set up. Never sent anywhere.
 */
export async function rememberLink(db: Database, startParam: string): Promise<void> {
  if (LINK_PARAM.test(startParam) && !isRegistrationLaunch(startParam)) await putMeta(db, META_PENDING_LINK, startParam);
}

/** Returns the link remembered before setup, once. */
export async function takePendingLink(db: Database): Promise<string | undefined> {
  const value = await getMeta<unknown>(db, META_PENDING_LINK);
  if (value !== undefined) await deleteMeta(db, META_PENDING_LINK);
  return typeof value === 'string' ? value : undefined;
}

async function requirePair(db: Database, name: string): Promise<StoredKeyPair> {
  const pair = await getKeyPair(db, name);
  if (!pair) throw new Error(`missing key ${name}`);
  return pair;
}
