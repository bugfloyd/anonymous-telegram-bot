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
import {
  checkCreated,
  checkInvitations,
  checkLevels,
  checkRedeemed,
  invitationUrl,
  normalizeCode,
  parseInvitationPayload,
  type InvitationCode,
  type InvitationsInfo,
  type LevelAllowance,
} from '../account/invitations';
import { checkAccountInfo, createLink, listLinks, revokeLink, type AccountInfo, type LinkRecord } from '../account/links';
import type { Config } from '../config';
import { ApiError, checkObject, NetworkError, type Api } from '../net/api';
import { createSession, type Session } from '../net/session';
import { BundleMismatch, checkBundle, linkUrl, parsePayload, verifyBundle, type LinkRef } from '../protocol/link';
import type { Launch } from '../platform/launch';
import type { Bridge } from '../platform/telegram';
import type { Database } from '../storage/db';
import type { SyncEvent } from '../sync/locks';

export type ErrorKind = 'network' | 'expired' | 'rate-limited' | 'logged-out' | 'generic';

/** A short message on the home screen after an action. */
export type HomeNotice =
  | 'created'
  | 'revoked'
  | 'limit'
  | 'invitation'
  | 'joined'
  | 'invalid-invitation'
  | 'already-member'
  | 'rate-limited'
  | 'network'
  | 'generic';

export type InvitationsNotice = 'created' | 'none-left' | 'network' | 'generic';

export type AdminNotice = 'saved' | 'granted' | 'not-member' | 'not-found' | 'bad-link' | 'network' | 'generic';

/** An invitation code with its shareable URL. */
export interface InvitationView extends InvitationCode {
  url: string;
}

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
  | {
      kind: 'home';
      offline: boolean;
      account: AccountInfo | null;
      invitations: InvitationsInfo | null;
      links: LinkView[];
      busy: boolean;
      notice: HomeNotice | null;
    }
  | {
      kind: 'invitations';
      info: Omit<InvitationsInfo, 'codes'> & { codes: InvitationView[] };
      busy: boolean;
      notice: InvitationsNotice | null;
      /** The code just created, to highlight. */
      created: string | null;
    }
  | { kind: 'admin'; levels: LevelAllowance[]; busy: boolean; notice: AdminNotice | null }
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
  // What this launch was opened with (a link or an invitation) is handled
  // once, not on every restart.
  let launchParam: string | null = isRegistrationLaunch(deps.launch.startParam) ? null : deps.launch.startParam || null;
  deps.bridge?.on('back_button_pressed', () => void showHome());

  const backButton = (visible: boolean) => deps.bridge?.post('web_app_setup_back_button', { is_visible: visible });
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
    return continueLaunch(offline);
  }

  /** After logging in or registering: go where the launch (or a launch before setup) pointed. */
  async function continueLaunch(offline: boolean): Promise<void> {
    const param = launchParam ?? (await takePendingLink(deps.db)) ?? '';
    launchParam = null;
    const ref = parsePayload(param);
    if (ref) return openLink(ref);
    const code = parseInvitationPayload(param);
    if (code && !offline) return redeem(code);
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
    let invitations: InvitationsInfo | null = null;
    if (!offline) {
      try {
        [account, invitations] = await Promise.all([
          session!.call('GET', '/account', undefined, checkAccountInfo),
          session!.call('GET', '/invitations', undefined, checkInvitations),
        ]);
      } catch (err) {
        if (handleGone(err)) return;
        offline = true;
      }
    }
    const links = (await listLinks(db)).map((l) => ({ ...l, url: linkUrl(config!.botUsername, config!.appName, l) }));
    backButton(false);
    screen.value = { kind: 'home', offline, account, invitations, links, busy: false, notice: options.notice ?? null };
  }

  /** Joins with an invitation code (typed, pasted, or from an invitation link). */
  async function redeem(input: string): Promise<void> {
    const code = normalizeCode(input);
    if (!code) return showHome({ notice: 'invalid-invitation' });
    const current = screen.value;
    if (current.kind === 'home') screen.value = { ...current, busy: true, notice: null };
    let notice: HomeNotice = 'joined';
    try {
      await session!.call('POST', '/invitations/redeem', { code }, checkRedeemed);
    } catch (err) {
      if (handleGone(err)) return;
      if (err instanceof ApiError && err.code === 'invalid_invitation') notice = 'invalid-invitation';
      else if (err instanceof ApiError && err.code === 'already_member') notice = 'already-member';
      else if (err instanceof ApiError && err.code === 'rate_limited') notice = 'rate-limited';
      else if (err instanceof NetworkError) notice = 'network';
      else notice = 'generic';
    }
    return showHome({ notice });
  }

  async function openInvitations(notice: InvitationsNotice | null = null, created: string | null = null): Promise<void> {
    const { config } = deps;
    let info: InvitationsInfo;
    try {
      info = await session!.call('GET', '/invitations', undefined, checkInvitations);
    } catch (err) {
      if (handleGone(err)) return;
      return showHome({ notice: err instanceof NetworkError ? 'network' : 'generic' });
    }
    const codes = info.codes.map((c) => ({ ...c, url: invitationUrl(config!.botUsername, config!.appName, c.code) }));
    backButton(true);
    screen.value = { kind: 'invitations', info: { ...info, codes }, busy: false, notice, created };
  }

  async function createInvitation(uses: number): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'invitations' || current.busy) return;
    screen.value = { ...current, busy: true, notice: null };
    try {
      const code = await session!.call('POST', '/invitations', { uses }, checkCreated, { sign: true });
      return openInvitations('created', code);
    } catch (err) {
      if (handleGone(err)) return;
      const notice: InvitationsNotice =
        err instanceof ApiError && err.code === 'no_invitations_left' ? 'none-left' : err instanceof NetworkError ? 'network' : 'generic';
      return openInvitations(notice);
    }
  }

  async function openAdmin(notice: AdminNotice | null = null): Promise<void> {
    let levels: LevelAllowance[];
    try {
      levels = await session!.call('GET', '/admin/levels', undefined, checkLevels);
    } catch (err) {
      if (handleGone(err)) return;
      return showHome({ notice: err instanceof NetworkError ? 'network' : 'generic' });
    }
    backButton(true);
    screen.value = { kind: 'admin', levels, busy: false, notice };
  }

  /** Runs an admin change, then shows the admin screen again with its outcome. */
  async function adminAction(action: () => Promise<unknown>, done: AdminNotice): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'admin' || current.busy) return;
    screen.value = { ...current, busy: true, notice: null };
    let notice: AdminNotice = done;
    try {
      await action();
    } catch (err) {
      if (handleGone(err)) return;
      if (err instanceof ApiError && err.code === 'not_member') notice = 'not-member';
      else if (err instanceof ApiError && err.code === 'not_found') notice = 'not-found';
      else if (err instanceof NetworkError) notice = 'network';
      else notice = 'generic';
    }
    return openAdmin(notice);
  }

  /** Grants a member extra invitations, naming them by one of their links. */
  function grant(linkText: string, invitations: number): Promise<void> {
    const ref = parsePayload(startParamOf(linkText));
    if (!ref) {
      const current = screen.value;
      if (current.kind === 'admin') screen.value = { ...current, notice: 'bad-link' };
      return Promise.resolve();
    }
    return adminAction(() => session!.call('POST', '/admin/grant', { link: ref.id, invitations }, checkObject, { sign: true }), 'granted');
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
    // A link or invitation opened before setup: continue there.
    launchParam = null;
    return continueLaunch(false);
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
    redeem,
    invitations: () => openInvitations(),
    createInvitation,
    admin: () => openAdmin(),
    setLevel: (level: number, invitations: number) =>
      adminAction(() => session!.call('PUT', '/admin/levels', { level, invitations }, checkObject, { sign: true }), 'saved'),
    grant,
  };
}

export type App = ReturnType<typeof createApp>;

/** The start parameter in a pasted t.me link, or the text itself. */
function startParamOf(text: string): string {
  const trimmed = text.trim();
  try {
    return new URL(trimmed).searchParams.get('startapp') ?? '';
  } catch {
    return trimmed;
  }
}

/** Whether the typed confirmation matches the phrase, ignoring case and spacing. */
export function confirmationMatches(typed: string, phrase: string): boolean {
  const norm = (s: string) => s.normalize('NFC').trim().replace(/\s+/g, ' ').toLocaleLowerCase();
  return norm(typed) !== '' && norm(typed) === norm(phrase);
}
