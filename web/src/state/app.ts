// What the app shows, and the actions that move between screens. The UI only
// reads `screen` and calls actions; it never touches storage, crypto or the
// network itself.

import { signal } from '@preact/signals';
import {
  isRegistrationLaunch,
  logIn,
  prepareSetup,
  readAccount,
  register,
  registrationPath,
  rememberLink,
  REGISTRATION_PREFIX,
  SetupMismatch,
} from '../account/account';
import type { Config } from '../config';
import { ApiError, NetworkError, type Api } from '../net/api';
import type { Launch } from '../platform/launch';
import type { Bridge } from '../platform/telegram';
import type { Database } from '../storage/db';
import type { SyncEvent } from '../sync/locks';

export type ErrorKind = 'network' | 'expired' | 'rate-limited' | 'logged-out' | 'generic';

export type Screen =
  | { kind: 'loading' }
  | { kind: 'unconfigured' }
  | { kind: 'outside-telegram' }
  | { kind: 'welcome'; mismatch: boolean }
  | { kind: 'reopening' }
  | { kind: 'registering' }
  | { kind: 'confirm-replace'; busy: boolean }
  | { kind: 'ready'; offline: boolean }
  | { kind: 'replaced' }
  | { kind: 'error'; error: ErrorKind };

export interface Deps {
  config: Config | null;
  api: Api | null;
  db: Database;
  launch: Launch;
  bridge: Bridge | null;
  /** Tells the other copies of the app on this device. */
  broadcast(event: SyncEvent): void;
  /** Waits before closing the old copy after asking Telegram to reopen. */
  closeDelayMs?: number;
}

/** How long the old copy waits before closing, so Telegram handles the link first (tested in phase 0). */
const CLOSE_DELAY_MS = 400;

export function createApp(deps: Deps) {
  const screen = signal<Screen>({ kind: 'loading' });
  // What "try again" does from the error screen.
  let retry: () => Promise<void> = () => start();

  const fail = (error: ErrorKind, again: () => Promise<void>) => {
    retry = again;
    screen.value = { kind: 'error', error };
  };

  async function start(): Promise<void> {
    const { config, api, db, launch, bridge } = deps;
    if (!config || !api) {
      screen.value = { kind: 'unconfigured' };
      return;
    }
    const account = await readAccount(db);
    if (account?.state === 'registered') return checkSession();
    if (account?.state === 'replaced') {
      screen.value = { kind: 'replaced' };
      return;
    }
    if (!bridge) {
      screen.value = { kind: 'outside-telegram' };
      return;
    }
    if (isRegistrationLaunch(launch.startParam)) {
      if (account?.state === 'pending' && launch.startParam === REGISTRATION_PREFIX + account.fingerprint) {
        return runRegister(false);
      }
      screen.value = { kind: 'welcome', mismatch: true };
      return;
    }
    if (launch.startParam) await rememberLink(db, launch.startParam);
    screen.value = { kind: 'welcome', mismatch: false };
  }

  async function checkSession(): Promise<void> {
    try {
      await logIn(deps.db, deps.api!);
      screen.value = { kind: 'ready', offline: false };
    } catch (err) {
      if (err instanceof ApiError && err.code === 'account_replaced') {
        screen.value = { kind: 'replaced' };
        deps.broadcast({ type: 'account-changed' });
      } else if (err instanceof ApiError && err.code === 'login_refused') {
        fail('logged-out', () => setUp({ restart: true }));
      } else {
        // Offline or the server is down: the device still works on what it has.
        screen.value = { kind: 'ready', offline: true };
      }
    }
  }

  /** Creates the keys (or reuses a pending setup) and asks Telegram to reopen as the registration launch. */
  async function setUp(options: { restart?: boolean } = {}): Promise<void> {
    const { config, bridge, db } = deps;
    if (!config || !bridge) return;
    const code = await prepareSetup(db, options);
    screen.value = { kind: 'reopening' };
    bridge.post('web_app_open_tg_link', { path_full: registrationPath(config.botUsername, config.appName, code) });
    // The new launch is a second copy of the app; this one steps aside.
    setTimeout(() => bridge.post('web_app_close'), deps.closeDelayMs ?? CLOSE_DELAY_MS);
  }

  async function runRegister(replace: boolean): Promise<void> {
    screen.value = replace ? { kind: 'confirm-replace', busy: true } : { kind: 'registering' };
    try {
      await register(deps.db, deps.api!, deps.launch, { replace });
    } catch (err) {
      if (err instanceof SetupMismatch) screen.value = { kind: 'welcome', mismatch: true };
      else if (err instanceof ApiError && err.code === 'account_exists') screen.value = { kind: 'confirm-replace', busy: false };
      // The launch data is only accepted for 10 minutes: reopen for a fresh one.
      else if (err instanceof ApiError && err.code === 'init_data_rejected') fail('expired', () => setUp());
      else if (err instanceof ApiError && err.code === 'rate_limited') fail('rate-limited', () => runRegister(replace));
      else if (err instanceof NetworkError) fail('network', () => runRegister(replace));
      else fail('generic', () => runRegister(replace));
      return;
    }
    deps.broadcast({ type: 'account-changed' });
    if (!deps.launch.allowsWriteToPm) deps.bridge?.post('web_app_request_write_access');
    screen.value = { kind: 'ready', offline: false };
  }

  return {
    screen,
    start,
    setUp: () => setUp(),
    setUpAgain: () => setUp({ restart: true }),
    confirmReplace: () => runRegister(true),
    cancelReplace: () => {
      screen.value = { kind: 'welcome', mismatch: false };
    },
    retry: () => retry(),
  };
}

export type App = ReturnType<typeof createApp>;

/** Whether the typed confirmation matches the phrase, ignoring case and spacing. */
export function confirmationMatches(typed: string, phrase: string): boolean {
  const norm = (s: string) => s.normalize('NFC').trim().replace(/\s+/g, ' ').toLocaleLowerCase();
  return norm(typed) !== '' && norm(typed) === norm(phrase);
}
