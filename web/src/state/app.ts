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
import { checkAccountInfo, createLink, listLinks, replaceLink, revokeLink, type AccountInfo, type LinkRecord } from '../account/links';
import type { Config } from '../config';
import type { TokenIssuer } from '../account/wallet';
import { importIssuerKey } from '../crypto/blindrsa';
import { createMessaging, NoTokens, type Messaging } from '../messaging/engine';
import { listConversations, listMessages, type MessageView } from '../messaging/store';
import { ApiError, checkObject, NetworkError, type Api } from '../net/api';
import { createSession, type Session } from '../net/session';
import { BundleMismatch, checkBundle, linkUrl, parsePayload, verifyBundle, type Bundle, type LinkRef } from '../protocol/link';
import type { Launch } from '../platform/launch';
import { openPopup, type Bridge, type PopupButton } from '../platform/telegram';
import { deleteMeta, getMeta, putMeta, type Database } from '../storage/db';
import type { SyncEvent } from '../sync/locks';

/** Set when someone chose to start over on this device, so the setup that follows asks to confirm. */
const META_START_OVER = 'start-over';

export type ErrorKind = 'network' | 'expired' | 'rate-limited' | 'logged-out' | 'generic';

/** A short message on the home screen after an action. */
export type HomeNotice =
  | 'blocked'
  | 'deleted'
  | 'replaced'
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

/** Why a message couldn't be sent. */
export type SendError = 'no-tokens' | 'rate-limited' | 'network' | 'generic';

/** A conversation in the list. */
export interface ConversationView {
  id: string;
  label: string;
  role: 'owner' | 'sender';
  unread: number;
  lastAt: number;
  status: 'active' | 'ended';
  /** The latest message, if any. */
  last: { text: string; out: boolean; status: MessageView['status'] } | null;
}

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
  /** This Telegram account is set up on another device (one device per account). */
  | { kind: 'elsewhere' }
  | {
      kind: 'home';
      tab: 'chats' | 'links';
      conversations: ConversationView[];
      offline: boolean;
      account: AccountInfo | null;
      invitations: InvitationsInfo | null;
      links: LinkView[];
      busy: boolean;
      notice: HomeNotice | null;
      /** Offer to turn on Telegram notices (the bot may not write yet). */
      askNotices: boolean;
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
  | { kind: 'link'; status: LinkStatus; canWrite: boolean; busy: boolean; error: SendError | null }
  | {
      kind: 'conversation';
      id: string;
      label: string;
      role: 'owner' | 'sender';
      ended: boolean;
      messages: MessageView[];
      busy: boolean;
      error: SendError | null;
    }
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
  /** The token issuer to use instead of the build's (tests). */
  issuer?: TokenIssuer;
  /** Run the inbox poll and upkeep in the background (off in tests). */
  background?: boolean;
}

/** How often an open app checks its inbox (until sockets arrive in phase 3). */
const POLL_MS = 20_000;

/** A link of this account, with its shareable URL. */
export interface LinkView extends LinkRecord {
  url: string;
}

/** How long the old copy waits before closing, so Telegram handles the link first (tested in phase 0). */
const CLOSE_DELAY_MS = 400;

export function createApp(deps: Deps) {
  const screen = signal<Screen>({ kind: 'loading' });
  // The screen now, read fresh (TypeScript would otherwise keep a type
  // narrowed by an earlier assignment, across awaits).
  const currentScreen = (): Screen => screen.value;
  const session: Session | null = deps.api ? createSession(deps.db, deps.api) : null;
  // What this launch was opened with (a link or an invitation) is handled
  // once, not on every restart.
  let launchParam: string | null = isRegistrationLaunch(deps.launch.startParam) ? null : deps.launch.startParam || null;
  deps.bridge?.on('back_button_pressed', () => void showHome());
  let messaging: Messaging | null = null;
  let tab: 'chats' | 'links' = 'chats';
  let backgroundStarted = false;
  // The link last checked and found genuine, to write to.
  let openedLink: { ref: LinkRef; bundle: Bundle } | null = null;

  // Telegram notices (design §12) are set up with this launch's signed data,
  // and only from a plain or registration launch: never one that names a
  // link or an invitation, which would tell the server whose link this is.
  const signedStart = new URLSearchParams(deps.launch.initData).get('start_param') ?? '';
  const noticeLaunch = deps.launch.initData !== '' && (signedStart === '' || isRegistrationLaunch(signedStart));
  let writeAllowed = deps.launch.allowsWriteToPm;
  let noticesTried = false;
  deps.bridge?.on('write_access_requested', ({ status }) => {
    if (status !== 'allowed') return;
    writeAllowed = true;
    void setUpNotices();
  });

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
    if (launch.startParam) {
      await rememberLink(db, launch.startParam);
    } else if (!account && (await accountElsewhere())) {
      // Say so rather than offer setup, which would replace that account.
      screen.value = { kind: 'elsewhere' };
      return;
    }
    screen.value = { kind: 'welcome', mismatch: false };
  }

  /**
   * Whether this Telegram account is set up on another device. Asked only on
   * a plain launch: launch data that names a link would tell the server whose
   * link this is. If the answer can't be had, setup shows as usual.
   */
  async function accountElsewhere(): Promise<boolean> {
    if (!noticeLaunch || deps.launch.startParam !== '') return false;
    try {
      const out = await deps.api!.post('/accounts/lookup', { initData: deps.launch.initData }, checkObject);
      return out.exists === true;
    } catch {
      return false;
    }
  }

  /**
   * "Start over on this device", from the other-device screen: on a
   * registration launch, straight to the typed confirmation; otherwise set up
   * first, and the confirmation comes when the server says the account exists.
   */
  async function startOver(): Promise<void> {
    const account = await readAccount(deps.db);
    if (account?.state === 'pending' && deps.launch.startParam === REGISTRATION_PREFIX + account.fingerprint) {
      screen.value = { kind: 'confirm-replace', busy: false };
      return;
    }
    await putMeta(deps.db, META_START_OVER, true);
    return setUp();
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
    await startMessaging(!offline);
    return continueLaunch(offline);
  }

  /** Creates the messaging engine and, outside tests, the inbox poll and upkeep. */
  async function startMessaging(online: boolean): Promise<void> {
    if (!messaging && deps.config && deps.api) {
      try {
        const issuer = deps.issuer ?? { key: await importIssuerKey(deps.config.issuer.spki), keyId: deps.config.issuer.keyId };
        messaging = createMessaging({ db: deps.db, api: deps.api, session: session!, issuer });
      } catch {
        // Without a usable issuer key the app still works, but can't send.
        return;
      }
    }
    if (online) await sync();
    if (deps.background === false || backgroundStarted || typeof document === 'undefined') return;
    backgroundStarted = true;
    setInterval(() => {
      if (document.visibilityState === 'visible') void sync();
    }, POLL_MS);
    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'visible') void sync();
    });
    // Upkeep at a random moment soon after opening, unrelated to anything
    // the user does, so the first send rarely has to fetch tokens itself.
    setTimeout(() => void messaging?.maintain().catch(() => {}), 2_000 + Math.random() * 8_000);
  }

  /** Fetches new messages; refreshes the screen if any arrived. */
  async function sync(): Promise<void> {
    if (!messaging) return;
    let received = 0;
    try {
      received = await messaging.sync();
    } catch (err) {
      handleGone(err);
      return;
    }
    if (received > 0) {
      deps.broadcast({ type: 'data-changed' });
      await refresh();
    }
  }

  /** Redraws the current screen from storage (after new messages, or another copy's change). */
  async function refresh(): Promise<void> {
    const current = screen.value;
    if (current.kind === 'home' && !current.busy) {
      screen.value = { ...current, conversations: await conversations() };
    } else if (current.kind === 'conversation' && !current.busy) {
      // Not marked seen here: only once they're in view (markSeen, from the screen).
      screen.value = { ...current, messages: await listMessages(deps.db, current.id) };
    }
  }

  async function conversations(): Promise<ConversationView[]> {
    // Blocked and deleted conversations are gone from the list.
    const list = (await listConversations(deps.db)).filter(
      (c): c is typeof c & { status: 'active' | 'ended' } => c.status !== 'blocked' && !c.hidden,
    );
    return Promise.all(
      list.map(async (c) => {
        const messages = await listMessages(deps.db, c.id);
        const last = messages[messages.length - 1];
        return {
          id: c.id,
          label: c.label,
          role: c.role,
          unread: c.unread,
          lastAt: c.lastAt,
          status: c.status,
          last: last ? { text: last.text, out: last.out, status: last.status } : null,
        };
      }),
    );
  }

  /**
   * Tells the other side, at once, what we've just read (design D12). In the
   * background: the screen doesn't wait, and a failure is retried next time.
   */
  function seen(id: string): void {
    void messaging
      ?.markSeen(id)
      .then(async () => {
        const now = currentScreen();
        if (now.kind === 'conversation' && now.id === id) screen.value = { ...now, messages: await listMessages(deps.db, id) };
      })
      .catch(() => {});
  }

  /** Blocks the open conversation, silently, then goes back to the list. */
  async function block(): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'conversation' || !messaging) return;
    screen.value = { ...current, busy: true };
    try {
      await messaging.block(current.id);
    } catch (err) {
      if (handleGone(err)) return;
      screen.value = { ...current, busy: false, error: sendError(err) };
      return;
    }
    deps.broadcast({ type: 'data-changed' });
    return showHome({ tab: 'chats', notice: 'blocked' });
  }

  /**
   * Deletes a conversation from this device (and first blocks it, if asked),
   * then shows the list.
   */
  async function deleteConversation(id: string, alsoBlock: boolean): Promise<void> {
    if (!messaging) return;
    const current = screen.value;
    if ((current.kind === 'home' || current.kind === 'conversation') && current.busy) return;
    if (current.kind === 'home' || current.kind === 'conversation') screen.value = { ...current, busy: true };
    try {
      if (alsoBlock) await messaging.block(id);
      await messaging.remove(id);
    } catch (err) {
      if (handleGone(err)) return;
      return showHome({ tab: 'chats', notice: err instanceof NetworkError ? 'network' : 'generic' });
    }
    deps.broadcast({ type: 'data-changed' });
    return showHome({ tab: 'chats', notice: alsoBlock ? 'blocked' : 'deleted' });
  }

  async function openConversation(id: string): Promise<void> {
    const list = await listConversations(deps.db);
    const c = list.find((x) => x.id === id);
    if (!c || c.status === 'blocked') return showHome();
    await messaging?.markRead(id);
    backButton(true);
    screen.value = {
      kind: 'conversation',
      id,
      label: c.label,
      role: c.role,
      ended: c.status === 'ended',
      messages: await listMessages(deps.db, id),
      busy: false,
      error: null,
    };
    seen(id);
  }

  function sendError(err: unknown): SendError {
    if (err instanceof NoTokens) return 'no-tokens';
    if (err instanceof ApiError && err.code === 'rate_limited') return 'rate-limited';
    if (err instanceof NetworkError) return 'network';
    return 'generic';
  }

  /** Sends a message in the open conversation. */
  async function sendMessage(text: string): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'conversation' || current.busy || !messaging || text.trim() === '') return;
    screen.value = { ...current, error: null };
    let error: SendError | null = null;
    // Shown at once as "sending", updated when delivered.
    const show = async () => {
      const now = currentScreen();
      if (now.kind === 'conversation' && now.id === current.id) {
        screen.value = { ...now, error, messages: await listMessages(deps.db, current.id) };
      }
    };
    try {
      await messaging.send(current.id, text.trim(), undefined, show);
    } catch (err) {
      if (handleGone(err)) return;
      error = sendError(err);
    }
    deps.broadcast({ type: 'data-changed' });
    await show();
  }

  /** Writes the first message to the link just checked, starting a conversation. */
  async function writeToLink(text: string): Promise<void> {
    const current = screen.value;
    if (current.kind !== 'link' || current.busy || !messaging || !openedLink || text.trim() === '') return;
    screen.value = { ...current, busy: true, error: null };
    let opened: string | null = null;
    try {
      // The conversation opens as soon as the message is stored, shown as sending.
      await messaging.startConversation(openedLink.ref, openedLink.bundle, text.trim(), async (id) => {
        opened = id;
        await openConversation(id);
      });
      deps.broadcast({ type: 'data-changed' });
      await refresh();
    } catch (err) {
      if (handleGone(err)) return;
      const error = sendError(err);
      const now = currentScreen();
      if (opened && now.kind === 'conversation' && now.id === opened) {
        screen.value = { ...now, error, messages: await listMessages(deps.db, opened) };
      } else {
        screen.value = { ...current, busy: false, error };
      }
    }
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

  async function showHome(options: { offline?: boolean; notice?: HomeNotice | null; tab?: 'chats' | 'links' } = {}): Promise<void> {
    if (options.tab) tab = options.tab;
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
    screen.value = {
      kind: 'home',
      tab,
      conversations: await conversations(),
      offline,
      account,
      invitations,
      links,
      busy: false,
      notice: options.notice ?? null,
      askNotices: noticeLaunch && !!account && !account.notices && !writeAllowed,
    };
    if (account && !account.notices && writeAllowed && !noticesTried) void setUpNotices();
  }

  /**
   * Turns on Telegram notices for this account. Failures are left alone:
   * a later launch tries again while the account says they're off.
   */
  async function setUpNotices(): Promise<void> {
    if (!noticeLaunch || !session) return;
    noticesTried = true;
    try {
      await session.call('POST', '/notices', { initData: deps.launch.initData }, checkObject);
    } catch {
      return;
    }
    const current = screen.value;
    if (current.kind === 'home' && current.account) {
      screen.value = { ...current, account: { ...current.account, notices: true }, askNotices: false };
    }
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
    return showHome({ notice, tab: 'links' });
  }

  /** Checks someone's link: its bundle must match the fingerprint in the link. */
  async function openLink(ref: LinkRef): Promise<void> {
    const linkScreen = (status: LinkStatus) => ({ kind: 'link' as const, status, canWrite: false, busy: false, error: null });
    // Our link, or one it replaced (the same fingerprint).
    if ((await listLinks(deps.db)).some((l) => l.id === ref.id || l.fingerprint === ref.fingerprint)) {
      screen.value = linkScreen('own');
      return;
    }
    // Already writing to this person: continue that conversation. A new one
    // would gain nothing, since the owner sees the same sender tag and label.
    // Through a replaced link, only once they've answered: before that, it
    // still writes to the old link's mailbox, which is gone.
    const existing = (await listConversations(deps.db)).find(
      (c) =>
        c.role === 'sender' &&
        c.status !== 'blocked' &&
        c.link.fingerprint === ref.fingerprint &&
        (c.link.id === ref.id || c.lastReceived !== null),
    );
    if (existing) return openConversation(existing.id);
    screen.value = linkScreen('checking');
    openedLink = null;
    try {
      const bundle = await deps.api!.request('GET', `/links/${ref.id}`, {}, checkBundle);
      await verifyBundle(ref, bundle, Math.floor(Date.now() / 1000));
      openedLink = { ref, bundle };
      screen.value = { ...linkScreen('verified'), canWrite: messaging !== null };
    } catch (err) {
      let status: LinkStatus = 'network';
      if (err instanceof ApiError && err.code === 'not_found') status = 'not-found';
      else if (err instanceof BundleMismatch) status = 'mismatch';
      screen.value = linkScreen(status);
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
      else if (err instanceof ApiError && err.code === 'account_exists') {
        // Set up on another device: say so, unless starting over was chosen already.
        const chosen = (await getMeta<unknown>(deps.db, META_START_OVER)) === true;
        await deleteMeta(deps.db, META_START_OVER);
        screen.value = chosen ? { kind: 'confirm-replace', busy: false } : { kind: 'elsewhere' };
      }
      // The launch data is only accepted for 10 minutes: reopen for a fresh one.
      else if (err instanceof ApiError && err.code === 'init_data_rejected') fail('expired', () => setUp());
      else if (err instanceof ApiError && err.code === 'rate_limited') fail('rate-limited', () => runRegister(replace));
      else if (err instanceof NetworkError) fail('network', () => runRegister(replace));
      else fail('generic', () => runRegister(replace));
      return;
    }
    deps.broadcast({ type: 'account-changed' });
    if (!deps.launch.allowsWriteToPm) deps.bridge?.post('web_app_request_write_access');
    await startMessaging(true);
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
      screen.value = { kind: 'elsewhere' };
    },
    startOver,
    close: () => deps.bridge?.post('web_app_close'),
    retry: () => retry(),
    home: () => showHome(),
    /** The open conversation's newest messages are in view: tell the other side. */
    markSeen: () => {
      const current = currentScreen();
      if (current.kind === 'conversation') seen(current.id);
    },
    /** Asks Telegram to let the bot write; notices are set up once it may. */
    allowNotices: () => deps.bridge?.post('web_app_request_write_access'),
    deleteConversation,
    /** Shows a native popup; resolves with the button pressed, or null. */
    popup: (params: { title?: string; message: string; buttons: PopupButton[] }) =>
      deps.bridge ? openPopup(deps.bridge, params) : Promise.resolve(null),
    haptic: () => deps.bridge?.post('web_app_trigger_haptic_feedback', { type: 'impact', impact_style: 'medium' }),
    showTab: (t: 'chats' | 'links') => showHome({ tab: t }),
    sync,
    refresh,
    openConversation,
    sendMessage,
    block,
    writeToLink,
    createLink: () => linkAction(() => createLink(deps.db, session!), 'created'),
    /** Gives the account a new link in place of its current one. */
    replaceLink: () => linkAction(() => replaceLink(deps.db, session!), 'replaced'),
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
