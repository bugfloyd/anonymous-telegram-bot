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
  takePendingLink,
} from '../account/account';
import { checkAccountInfo, createLink, listLinks, revokeLink, type AccountInfo, type LinkRecord } from '../account/links';
import type { Config } from '../config';
import { ApiError, NetworkError, type Api } from '../net/api';
import { createSession, type Session } from '../net/session';
import { BundleMismatch, checkBundle, linkUrl, parsePayload, verifyBundle, type LinkRef } from '../protocol/link';
import type { Launch } from '../platform/launch';
import type { Bridge } from '../platform/telegram';
import type { Database } from '../storage/db';
import type { SyncEvent } from '../sync/locks';

export type ErrorKind = 'network' | 'expired' | 'rate-limited' | 'logged-out' | 'generic';

/** A short message on the home screen after an action. */
export type HomeNotice = 'created' | 'revoked' | 'limit' | 'invitation' | 'network' | 'generic';

/** What opening someone's link found. */
export type LinkStatus = 'checking' | 'verified' | 'own' | 'not-found' | 'mismatch' | 'network';

export type Screen =
  | { kind: 'loading' }
  | { kind: 'unconfigured' }
  | { kind: 'outside-telegram' }
  | { kind: 'welcome'; mismatch: boolean }
  | { kind: 'reopening' }
  | { kind: 'registering' }
  | { kind: 'confirm-replace'; busy: boolean }
  | { kind: 'home'; offline: boolean; account: AccountInfo | null; links: LinkView[]; busy: boolean; notice: HomeNotice | null }
  | { kind: 'link'; status: LinkStatus }
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

/** A link of this account, with its shareable URL. */
export interface LinkView extends LinkRecord {
  url: string;
}

/** How long the old copy waits before closing, so Telegram handles the link first (tested in phase 0). */
const CLOSE_DELAY_MS = 400;

export function createApp(deps: Deps) {
  const screen = signal<Screen>({ kind: 'loading' });
  const session: Session | null = deps.api ? createSession(deps.db, deps.api) : null;
  // The link this launch was opened with is shown once, not on every restart.
  let launchLink: LinkRef | null = parsePayload(deps.launch.startParam);
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
    if (account?.state === 'registered') return enter();
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

  /** A registered device: log in, then show the link it was opened with, or home. */
  async function enter(): Promise<void> {
    let offline = false;
    try {
      session!.use(await logIn(deps.db, deps.api!));
    } catch (err) {
      if (handleGone(err)) return;
      if (err instanceof ApiError && err.code === 'login_refused') {
        fail('logged-out', () => setUp({ restart: true }));
        return;
      }
      // Offline or the server is down: the device still works on what it has.
      offline = true;
    }
    const ref = launchLink ?? parsePayload((await takePendingLink(deps.db)) ?? '');
    launchLink = null;
    if (ref) return openLink(ref);
    return showHome({ offline });
  }

  /** Shows a replaced account loudly. Returns whether err was that. */
  function handleGone(err: unknown): boolean {
    if (!(err instanceof ApiError && err.code === 'account_replaced')) return false;
    screen.value = { kind: 'replaced' };
    deps.broadcast({ type: 'account-changed' });
    return true;
  }

  async function showHome(options: { offline?: boolean; notice?: HomeNotice | null } = {}): Promise<void> {
    const { config, db } = deps;
    let offline = options.offline ?? false;
    let account: AccountInfo | null = null;
    if (!offline) {
      try {
        account = await session!.call('GET', '/account', undefined, checkAccountInfo);
      } catch (err) {
        if (handleGone(err)) return;
        offline = true;
      }
    }
    const links = (await listLinks(db)).map((l) => ({ ...l, url: linkUrl(config!.botUsername, config!.appName, l) }));
    screen.value = { kind: 'home', offline, account, links, busy: false, notice: options.notice ?? null };
  }

  /** Runs a link action from the home screen, then shows home again with its outcome. */
  async function linkAction(action: () => Promise<unknown>, done: HomeNotice): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'home' || current.busy) return;
    screen.value = { ...current, busy: true, notice: null };
    let notice: HomeNotice = done;
    try {
      await action();
    } catch (err) {
      if (handleGone(err)) return;
      if (err instanceof ApiError && err.code === 'invitation_required') notice = 'invitation';
      else if (err instanceof ApiError && err.code === 'link_limit') notice = 'limit';
      else if (err instanceof NetworkError) notice = 'network';
      else notice = 'generic';
    }
    deps.broadcast({ type: 'data-changed' });
    return showHome({ notice });
  }

  /** Checks someone's link: its bundle must match the fingerprint in the link. */
  async function openLink(ref: LinkRef): Promise<void> {
    if ((await listLinks(deps.db)).some((l) => l.id === ref.id)) {
      screen.value = { kind: 'link', status: 'own' };
      return;
    }
    screen.value = { kind: 'link', status: 'checking' };
    try {
      const bundle = await deps.api!.request('GET', `/links/${ref.id}`, {}, checkBundle);
      await verifyBundle(ref, bundle, Math.floor(Date.now() / 1000));
      screen.value = { kind: 'link', status: 'verified' };
    } catch (err) {
      let status: LinkStatus = 'network';
      if (err instanceof ApiError && err.code === 'not_found') status = 'not-found';
      else if (err instanceof BundleMismatch) status = 'mismatch';
      screen.value = { kind: 'link', status };
      if (status === 'network') retry = () => openLink(ref);
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
      session!.use(await register(deps.db, deps.api!, deps.launch, { replace }));
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
    // A link opened before setup: continue there.
    launchLink = null;
    const ref = parsePayload((await takePendingLink(deps.db)) ?? '');
    if (ref) return openLink(ref);
    return showHome();
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
    home: () => showHome(),
    createLink: () => linkAction(() => createLink(deps.db, session!), 'created'),
    revokeLink: (id: string) => linkAction(() => revokeLink(deps.db, session!, id), 'revoked'),
    /** Opens Telegram's share sheet for a link. */
    share: (url: string) => deps.bridge?.post('web_app_open_tg_link', { path_full: `/share/url?url=${encodeURIComponent(url)}` }),
  };
}

export type App = ReturnType<typeof createApp>;

/** Whether the typed confirmation matches the phrase, ignoring case and spacing. */
export function confirmationMatches(typed: string, phrase: string): boolean {
  const norm = (s: string) => s.normalize('NFC').trim().replace(/\s+/g, ' ').toLocaleLowerCase();
  return norm(typed) !== '' && norm(typed) === norm(phrase);
}
