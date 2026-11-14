import { signal, useSignal } from '@preact/signals';
import { createContext, createPortal, type ComponentChildren } from 'preact';
import { useContext, useEffect, useLayoutEffect, useRef } from 'preact/hooks';
import { locale, relativeTime, t, type Language } from './i18n';
import type { Key } from './i18n/en';
import type { MessageView } from './messaging/store';
import type { KeepMedia, MediaSettings } from './messaging/settings';
import { fromBase64Url } from './crypto/encoding';
import { showableAs } from './media/prepare';
import {
  canRecord,
  hasCamera,
  MAX_ROUND_MS,
  MAX_VOICE_MS,
  RecordingUnavailable,
  startRecording,
  startRound,
  type Recorder,
  type Recording,
  type RoundRecorder,
  type RoundRecording,
} from './media/record';
import { MAX_FILE_BYTES, shownAs, type Sticker } from './protocol/message';
import { ART_CREDIT, ART_LICENSE, findSticker, loadArt, PACKS, type StickerInfo } from './stickers/catalog';
import { EMOJI_GROUPS, type EmojiGroup } from './emoji/data';
import { deleteBefore, insertAt, type Caret } from './emoji/edit';
import {
  confirmationMatches,
  type AdminNotice,
  type App as AppState,
  type ErrorKind,
  type HomeNotice,
  type InvitationsNotice,
  type LinkStatus,
  type LinkView,
  type Screen,
  type SendAs,
  type SendError,
} from './state/app';

const ERROR_TEXT: Record<ErrorKind, Key> = {
  network: 'errorNetwork',
  expired: 'errorExpired',
  'rate-limited': 'errorRateLimited',
  'logged-out': 'errorLoggedOut',
  generic: 'errorGeneric',
};

const LINK_TEXT: Record<LinkStatus, Key> = {
  checking: 'linkChecking',
  verified: 'linkVerified',
  own: 'linkOwn',
  'not-found': 'linkNotFound',
  mismatch: 'linkMismatch',
  network: 'errorNetwork',
};

const NOTICE_TEXT: Record<HomeNotice, Key> = {
  blocked: 'noticeBlocked',
  deleted: 'noticeDeleted',
  unblocked: 'noticeUnblocked',
  replaced: 'noticeReplaced',
  created: 'noticeCreated',
  revoked: 'noticeRevoked',
  limit: 'noticeLimit',
  invitation: 'needInvitation',
  joined: 'noticeJoined',
  'invalid-invitation': 'noticeInvalidInvitation',
  'already-member': 'noticeAlreadyMember',
  'own-invitation': 'noticeOwnInvitation',
  'rate-limited': 'noticeRateLimited',
  network: 'errorNetwork',
  generic: 'errorGeneric',
};

const INVITATIONS_NOTICE_TEXT: Record<InvitationsNotice, Key> = {
  created: 'noticeInvitationCreated',
  'none-left': 'noticeNoneLeft',
  network: 'errorNetwork',
  generic: 'errorGeneric',
};

const SEND_ERROR_TEXT: Record<SendError, Key> = {
  'no-tokens': 'errorNoTokens',
  'rate-limited': 'errorTooFast',
  network: 'errorNetwork',
  'too-big': 'errorTooBig',
  unreadable: 'errorUnreadable',
  generic: 'errorGeneric',
};

const ADMIN_NOTICE_TEXT: Record<AdminNotice, Key> = {
  saved: 'noticeSaved',
  granted: 'noticeGranted',
  'not-member': 'noticeNotMember',
  'not-found': 'noticeLinkUnknown',
  'bad-link': 'noticeLinkUnknown',
  network: 'errorNetwork',
  generic: 'errorGeneric',
};

/** What open overlays tell the back button (AppState.onBack). */
const BackStack = createContext<(close: () => void) => () => void>(() => () => {});

/**
 * While open, the back button (Telegram's, or the phone's) closes this
 * first, as in Telegram, instead of leaving the screen.
 */
function useBackCloses(open: boolean, close: () => void): void {
  const register = useContext(BackStack);
  const latest = useRef(close);
  latest.current = close;
  useEffect(() => (open ? register(() => latest.current()) : undefined), [open]);
}

export function App({ app, language }: { app: AppState; language: Language }) {
  return (
    <BackStack.Provider value={app.onBack}>
      <Screen app={app} language={language} />
      <DeleteDialog app={app} language={language} />
      {toast.value && (
        <p class="toast" role="status">
          {toast.value}
        </p>
      )}
    </BackStack.Provider>
  );
}

function Screen({ app, language }: { app: AppState; language: Language }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const screen = app.screen.value;

  switch (screen.kind) {
    case 'loading':
      return <Page title={tr('appName')} body={tr('loading')} busy />;
    case 'unconfigured':
      return <Page title={tr('appName')} body={tr('notConfigured')} />;
    case 'outside-telegram':
      return <Page title={tr('appName')} body={tr('openInTelegram')} />;
    case 'welcome':
      return (
        <Page title={tr('welcomeTitle')} body={tr('welcomeBody')}>
          {screen.deleted && (
            <p class="notice" role="status">
              {tr('accountDeleted')}
            </p>
          )}
          {screen.mismatch && <p class="notice">{tr('setupMismatch')}</p>}
          <Button onClick={app.setUp}>{tr('setUp')}</Button>
        </Page>
      );
    case 'reopening':
      return (
        <Page title={tr('appName')} body={tr('reopening')} busy>
          <Button kind="secondary" onClick={app.setUp}>
            {tr('reopenAgain')}
          </Button>
        </Page>
      );
    case 'registering':
      return <Page title={tr('appName')} body={tr('registering')} busy />;
    case 'elsewhere':
      return (
        <Page title={tr('elsewhereTitle')} body={tr('elsewhereBody')}>
          <Button onClick={app.close}>{tr('closeApp')}</Button>
          <p class="muted">{tr('elsewhereLost')}</p>
          <Button kind="secondary" onClick={app.startOver}>
            {tr('elsewhereStartOver')}
          </Button>
        </Page>
      );
    case 'confirm-replace':
      return <ConfirmReplace app={app} language={language} busy={screen.busy} />;
    case 'confirm-delete':
      return <ConfirmDelete app={app} language={language} busy={screen.busy} error={screen.error} />;
    case 'blocked':
      return <Blocked app={app} language={language} screen={screen} />;
    case 'storage':
      return <Storage app={app} language={language} screen={screen} />;
    case 'home':
      return <Home app={app} language={language} screen={screen} />;
    case 'invitations':
      return <Invitations app={app} language={language} screen={screen} />;
    case 'admin':
      return <Admin app={app} language={language} screen={screen} />;
    case 'link':
      return (
        <Page title={tr('linkTitle')} busy={screen.status === 'checking'}>
          <p class={screen.status === 'mismatch' ? 'warning' : 'notice'} role={screen.status === 'mismatch' ? 'alert' : undefined}>
            {tr(LINK_TEXT[screen.status])}
          </p>
          {screen.status === 'verified' && screen.canWrite && (
            <>
              <p class="muted">{tr('firstMessageHint')}</p>
              {screen.error && (
                <p class="notice" role="alert">
                  {tr(SEND_ERROR_TEXT[screen.error])}
                </p>
              )}
              <Composer language={language} busy={screen.busy} onSend={app.writeToLink} />
            </>
          )}
          {screen.status === 'network' && <Button onClick={app.retry}>{tr('tryAgain')}</Button>}
          {screen.status !== 'checking' && (
            <Button kind="secondary" onClick={app.home}>
              {tr('goHome')}
            </Button>
          )}
        </Page>
      );
    case 'conversation':
      return <Conversation app={app} language={language} screen={screen} />;
    case 'replaced':
      return (
        <Page title={tr('replacedTitle')}>
          <p class="warning" role="alert">
            {tr('replacedBody')}
          </p>
          <Button kind="secondary" onClick={app.setUpAgain}>
            {tr('setUpAgain')}
          </Button>
        </Page>
      );
    case 'error':
      return (
        <Page title={tr('appName')}>
          <p class="notice" role="alert">
            {tr(ERROR_TEXT[screen.error])}
          </p>
          <Button onClick={app.retry}>{screen.error === 'expired' || screen.error === 'logged-out' ? tr('continue') : tr('tryAgain')}</Button>
        </Page>
      );
  }
}

function Home({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'home' }> }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const { account, invitations, links, busy, notice, offline } = screen;
  // One link per account (older ones, from before, go when it's replaced).
  const link = links[links.length - 1];
  const canCreate = !offline && !!account?.canCreateLinks && !link;
  const mayJoin = !!invitations && invitations.inviteOnly && !invitations.member && !invitations.admin;
  const mayInvite = !!invitations && invitations.inviteOnly && (invitations.member || invitations.admin);
  // The chat swiped open to show its Delete button.
  const revealed = useSignal<string | null>(null);
  const tabs = (
    <div class="tabs" role="tablist">
      {(['chats', 'links'] as const).map((tabName) => (
        <button
          type="button"
          role="tab"
          aria-selected={screen.tab === tabName}
          class={screen.tab === tabName ? 'tab active' : 'tab'}
          onClick={() => void app.showTab(tabName)}
        >
          {tr(tabName === 'chats' ? 'tabChats' : 'tabLinks')}
          {tabName === 'chats' && screen.conversations.some((c) => c.unread > 0) && <span class="dot" aria-hidden="true" />}
        </button>
      ))}
    </div>
  );
  if (screen.tab === 'chats') {
    return (
      <main class="page">
        {tabs}
        {offline && <p class="notice">{tr('offline')}</p>}
        {notice && (
          <p class="notice" role="status">
            {tr(NOTICE_TEXT[notice])}
          </p>
        )}
        {screen.askNotices && (
          <div class="card">
            <p>{tr('noticesAsk')}</p>
            <button type="button" class="button secondary" onClick={() => app.allowNotices()}>
              {tr('noticesAllow')}
            </button>
          </div>
        )}
        {screen.conversations.length === 0 ? (
          <p class="muted">{tr('noConversations')}</p>
        ) : (
          <ul class="chats">
            {screen.conversations.map((c) => (
              <ChatRow
                key={c.id}
                app={app}
                language={language}
                chat={c}
                revealed={revealed.value === c.id}
                onReveal={(id) => (revealed.value = id)}
              />
            ))}
          </ul>
        )}
        {screen.blockedCount > 0 && (
          <button type="button" class="link-button muted-link" onClick={() => void app.blocked()}>
            {tr('blockedCount', { count: screen.blockedCount })}
          </button>
        )}
      </main>
    );
  }
  return (
    <Page title={tr('homeTitle')} body={tr('homeIntro')}>
      {tabs}
      {offline && <p class="notice">{tr('offline')}</p>}
      {notice && (
        <p class="notice" role="status">
          {tr(NOTICE_TEXT[notice])}
        </p>
      )}
      {account && !account.canCreateLinks && notice !== 'invitation' && <p class="notice">{tr('needInvitation')}</p>}
      {mayJoin && <JoinForm app={app} language={language} busy={busy} />}
      {link ? (
        <ul class="links">
          <LinkItem key={link.id} app={app} language={language} link={link} busy={busy || offline} />
        </ul>
      ) : (
        <p class="muted">{tr('noLinks')}</p>
      )}
      {canCreate && (
        <Button disabled={busy} onClick={app.createLink}>
          {tr('createLink')}
        </Button>
      )}
      {(mayInvite || invitations?.admin) && (
        <div class="row">
          {mayInvite && (
            <Button kind="secondary" disabled={busy} onClick={app.invitations}>
              {tr('invitationsTitle')}
            </Button>
          )}
          {invitations?.admin && (
            <Button kind="secondary" disabled={busy} onClick={app.admin}>
              {tr('adminTitle')}
            </Button>
          )}
        </div>
      )}
      {busy && <div class="spinner" aria-hidden="true" />}
      <button type="button" class="link-button muted-link" disabled={busy} onClick={() => void app.storage()}>
        {tr('storageTitle')}
      </button>
      <button type="button" class="link-button delete-account" disabled={busy || offline} onClick={app.askDeleteAccount}>
        {tr('deleteAccount')}
      </button>
    </Page>
  );
}

/** How close to the end of a chat still counts as being at the end, in px. */
const AT_END = 80;
const isAtEnd = () => window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - AT_END;
const scrollToEnd = (behavior: ScrollBehavior = 'auto') => window.scrollTo({ top: document.documentElement.scrollHeight, behavior });
const visible = () => document.visibilityState === 'visible';

/**
 * Keeps a chat where Telegram would: it opens at the newest message, and
 * jumps there when you send. New messages scroll into view only if you were
 * already at the end; otherwise you stay where you are and a button shows
 * how many came in. Messages count as seen only once they're in view.
 */
function useChatScroll(app: AppState, id: string, messages: { id: string; out: boolean }[]) {
  const atEnd = useRef(true);
  const shown = useRef<{ conv: string; last: string | null } | null>(null);
  // Scrolled away from the end, and how many new messages came in since.
  const away = useSignal(false);
  const fresh = useSignal(0);

  const arrived = () => {
    atEnd.current = true;
    away.value = false;
    fresh.value = 0;
    if (visible()) app.markSeen();
  };

  useEffect(() => {
    const onScroll = () => {
      const now = isAtEnd();
      if (now && !atEnd.current) arrived();
      atEnd.current = now;
      away.value = !now;
    };
    // The keyboard opening shrinks the view: stay at the end if we were.
    const onResize = () => atEnd.current && scrollToEnd();
    const onVisible = () => visible() && atEnd.current && app.markSeen();
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onResize);
    document.addEventListener('visibilitychange', onVisible);
    return () => {
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', onResize);
      document.removeEventListener('visibilitychange', onVisible);
    };
  }, []);

  useLayoutEffect(() => {
    const last = messages[messages.length - 1]?.id ?? null;
    const before = shown.current;
    shown.current = { conv: id, last };
    if (before?.conv !== id) {
      // Just opened: start at the newest message.
      scrollToEnd();
      atEnd.current = true;
      away.value = false;
      fresh.value = 0;
      return;
    }
    if (last === before.last) return;
    const added = messages.slice(messages.findIndex((m) => m.id === before.last) + 1);
    if (added.some((m) => m.out) || atEnd.current) {
      scrollToEnd();
      arrived();
    } else {
      fresh.value += added.length;
    }
  }, [id, messages]);

  return { away: away.value, fresh: fresh.value, toEnd: () => scrollToEnd('smooth') };
}

function Conversation({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'conversation' }> }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const confirmingBlock = useSignal(false);
  useBackCloses(confirmingBlock.value, () => (confirmingBlock.value = false));
  const scroll = useChatScroll(app, screen.id, screen.messages);
  const sending = app.preparing.value.filter((p) => p.conv === screen.id).length;
  useEffect(() => {
    if (sending > 0) window.scrollTo({ top: document.documentElement.scrollHeight });
  }, [sending]);
  // The message being answered, the one whose menu is open, and the one just jumped to.
  const replying = useSignal<MessageView | null>(null);
  const menu = useSignal<MessageView | null>(null);
  useBackCloses(!!menu.value, () => (menu.value = null));
  const highlight = useSignal<string | null>(null);
  const byId = new Map(screen.messages.map((m) => [m.id, m]));
  useEffect(() => {
    replying.value = null;
    menu.value = null;
  }, [screen.id]);
  const jumpTo = (id: string) => {
    const el = document.getElementById(`m-${id}`);
    if (!el) return;
    el.scrollIntoView({ block: 'center', behavior: 'smooth' });
    highlight.value = id;
    setTimeout(() => highlight.value === id && (highlight.value = null), 1500);
  };
  const time = new Intl.DateTimeFormat(locale(language), { hour: 'numeric', minute: '2-digit' });
  useEffect(() => {
    const timer = setTimeout(loadStickerArt, 300);
    return () => clearTimeout(timer);
  }, []);
  return (
    <main class="page conversation">
      <header class="conversation-head">
        <bdi dir="ltr" class="label">
          {screen.label}
        </bdi>
        <span class="muted">{tr(screen.role === 'owner' ? 'aboutOwnerConversation' : 'aboutSenderConversation')}</span>
        {screen.blocked ? null : confirmingBlock.value ? (
          <div class="row">
            <p class="warning">{tr(screen.role === 'owner' ? 'blockConfirmOwner' : 'blockConfirmSender')}</p>
            <Button kind="danger" disabled={screen.busy} onClick={app.block}>
              {tr('blockYes')}
            </Button>
            <Button kind="secondary" disabled={screen.busy} onClick={() => (confirmingBlock.value = false)}>
              {tr('cancel')}
            </Button>
          </div>
        ) : (
          <div class="head-actions">
            <button type="button" class="link-button" disabled={screen.busy} onClick={() => void confirmDelete(app, language, screen.id, screen.label, !screen.blocked && !screen.ended)}>
              {tr('delete')}
            </button>
            <button type="button" class="link-button" disabled={screen.busy} onClick={() => (confirmingBlock.value = true)}>
              {tr('block')}
            </button>
          </div>
        )}
      </header>
      <ol class="messages">
        {screen.messages.map((m) => (
          <Bubble
            key={m.id}
            app={app}
            message={m}
            quoted={m.replyTo ? (byId.get(m.replyTo) ?? null) : undefined}
            peer={screen.label}
            language={language}
            time={time}
            highlighted={highlight.value === m.id}
            canReply={!screen.blocked && !screen.ended}
            canReact={canReact(screen, m)}
            onReact={(e) => void app.react(m.id, e)}
            onDoubleTap={() => {
              app.haptic();
              void app.react(m.id, DOUBLE_TAP_REACTION);
            }}
            onReply={() => (replying.value = m)}
            onMenu={() => (menu.value = m)}
            onQuote={() => m.replyTo && jumpTo(m.replyTo)}
          />
        ))}
      {app.preparing.value
        .filter((p) => p.conv === screen.id)
        .map((p) => (
          <li key={p.key} class={p.as === 'round' ? 'bubble out bare' : 'bubble out'} aria-busy="true">
            {p.as === 'round' ? (
              <div class="round">
                {p.url ? <video src={p.url} muted playsInline autoPlay loop /> : p.still && <img class="still" src={p.still} alt="" />}
                <span class="media-progress">
                  <Ring fraction={undefined} />
                </span>
              </div>
            ) : p.as !== 'file' && p.url ? (
              <div class="photo">
                {p.as === 'video' ? <video src={p.url} muted playsInline preload="metadata" /> : <img src={p.url} alt="" />}
                <span class="media-progress">
                  <Ring fraction={undefined} />
                </span>
              </div>
            ) : (
              <div class="file">
                <span class="file-icon">
                  <Ring fraction={undefined} />
                </span>
                <span class="file-info">
                  <span class="file-name" dir="auto">
                    {p.name}
                  </span>
                  <span class="file-size">{formatSize(language, p.size)}</span>
                </span>
              </div>
            )}
            {p.caption && (
              <span class="text" dir="auto">
                {p.caption}
              </span>
            )}
          </li>
        ))}
      </ol>
      {menu.value && (
        <MessageMenu
          message={menu.value}
          language={language}
          canReply={!screen.blocked && !screen.ended}
          canReact={canReact(screen, menu.value)}
          peer={screen.label}
          canDeleteForBoth={canReact(screen, menu.value) && menu.value.out}
          onClose={() => (menu.value = null)}
          onReact={(m, e) => void app.react(m.id, e)}
          onReply={(m) => (replying.value = m)}
          onDelete={(m, both) => {
            if (replying.value?.id === m.id) replying.value = null;
            void (both ? app.deleteForEveryone(m.id) : app.deleteMessage(m.id));
          }}
        />
      )}
      <div class="conversation-foot">
        {(scroll.away || scroll.fresh > 0) && (
          <button
            type="button"
            class="to-end"
            onClick={scroll.toEnd}
            aria-label={scroll.fresh > 0 ? tr('newMessages', { count: scroll.fresh }) : tr('toLatest')}
          >
            <svg viewBox="0 0 16 16" aria-hidden="true">
              <path d="M3.5 6l4.5 4.5L12.5 6" />
            </svg>
            {scroll.fresh > 0 && <span class="badge">{t(language, 'unreadCount', { count: scroll.fresh })}</span>}
          </button>
        )}
        {screen.blocked ? (
          <>
            <p class="notice">{tr('blockedConversation')}</p>
            {screen.error && (
              <p class="notice" role="alert">
                {tr(SEND_ERROR_TEXT[screen.error])}
              </p>
            )}
            <Button disabled={screen.busy} onClick={() => app.unblock(screen.id)}>
              {tr('unblock')}
            </Button>
          </>
        ) : screen.ended ? (
          <p class="notice">{tr('conversationEnded')}</p>
        ) : (
          <>
            {screen.error && (
              <p class="notice" role="alert">
                {tr(SEND_ERROR_TEXT[screen.error])}
              </p>
            )}
            <Composer
              language={language}
              busy={screen.busy}
              reply={
                replying.value
                  ? { name: replying.value.out ? tr('you') : screen.label, text: contentLine(language, replying.value) }
                  : null
              }
              onCancelReply={() => (replying.value = null)}
              onSend={async (text) => {
                const to = replying.value?.id;
                replying.value = null;
                await app.sendMessage(text, to);
              }}
              onSticker={async (sticker) => {
                const to = replying.value?.id;
                replying.value = null;
                await app.sendMessage({ sticker }, to);
              }}
              {...(app.media
                ? {
                    onAttach: async (file: File, as: SendAs, caption: string) => {
                      const to = replying.value?.id;
                      replying.value = null;
                      await app.sendMedia(file, as, caption, to);
                    },
                    onVoice: async (recording: Recording) => {
                      const to = replying.value?.id;
                      replying.value = null;
                      await app.sendVoice(recording, to);
                    },
                    onRound: async (recording: Promise<RoundRecording | null>, still: string | null) => {
                      const to = replying.value?.id;
                      replying.value = null;
                      await app.sendRound(recording, still, to);
                    },
                  }
                : {})}
            />
          </>
        )}
      </div>
    </main>
  );
}

/** Asks, in Telegram's own popup, whether to delete a conversation from this device, or delete and block it. */
/** The conversation the delete dialog is open for: its label, and whether it's open (so it can be deleted for both sides). */
const deleting = signal<{ id: string; label: string; active: boolean } | null>(null);

/** Asks before deleting a conversation, with blocking and deleting for both sides as extra, unchecked choices. */
function confirmDelete(_app: AppState, _language: Language, id: string, label: string, active: boolean): void {
  deleting.value = { id, label, active };
}

function DeleteDialog({ app, language }: { app: AppState; language: Language }) {
  const alsoBlock = useSignal(false);
  const forBoth = useSignal(false);
  const first = useRef<HTMLButtonElement>(null);
  const target = deleting.value;
  const close = () => {
    deleting.value = null;
    alsoBlock.value = false;
    forBoth.value = false;
  };
  useBackCloses(!!target, close);
  useEffect(() => {
    if (target) first.current?.focus();
  }, [target?.id]);
  if (!target) return null;
  const confirm = () => {
    const block = alsoBlock.value;
    const both = target.active && forBoth.value;
    close();
    void app.deleteConversation(target.id, block, both);
  };
  return (
    <div class="overlay" onClick={close} onKeyDown={(e) => e.key === 'Escape' && close()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="delete-text" onClick={(e) => e.stopPropagation()}>
        <p id="delete-text">{t(language, target.active && forBoth.value ? 'deleteConfirmBoth' : 'deleteConfirm')}</p>
        {target.active && (
          <label class="check">
            <input type="checkbox" checked={forBoth.value} onChange={(e) => (forBoth.value = e.currentTarget.checked)} />
            <span>{t(language, 'alsoDeleteFor', { name: `\u2068${target.label}\u2069` })}</span>
          </label>
        )}
        <label class="check">
          <input type="checkbox" checked={alsoBlock.value} onChange={(e) => (alsoBlock.value = e.currentTarget.checked)} />
          <span>{t(language, 'alsoBlock')}</span>
        </label>
        <div class="row">
          <button type="button" class="button danger" ref={first} onClick={confirm}>
            {t(language, 'delete')}
          </button>
          <Button kind="secondary" onClick={close}>
            {t(language, 'cancel')}
          </Button>
        </div>
      </div>
    </div>
  );
}

/** The Blocked list: open a blocked conversation (read-only), or unblock it. */
function Blocked({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'blocked' }> }) {
  const tr = (key: Key) => t(language, key);
  return (
    <Page title={tr('blockedTitle')} busy={screen.busy}>
      {screen.notice && (
        <p class="notice" role="status">
          {tr(NOTICE_TEXT[screen.notice])}
        </p>
      )}
      {screen.conversations.length === 0 ? (
        <p class="muted">{tr('noBlocked')}</p>
      ) : (
        <ul class="chats">
          {screen.conversations.map((c) => (
            <li key={c.id} class="blocked-row">
              <button type="button" class="chat" onClick={() => void app.openConversation(c.id)}>
                <bdi dir="ltr" class="label">
                  {c.label}
                </bdi>
                <span class="preview" dir="auto">
                  {c.last ? contentLine(language, c.last) : ''}
                </span>
              </button>
              <Button kind="secondary" disabled={screen.busy} onClick={() => app.unblock(c.id)}>
                {tr('unblock')}
              </Button>
            </li>
          ))}
        </ul>
      )}
    </Page>
  );
}

const KEEP_TEXT: Record<KeepMedia, Key> = { forever: 'keepForever', month: 'keepMonth', week: 'keepWeek' };
const AUTO_TEXT: Record<keyof MediaSettings['auto'], Key> = { photos: 'autoPhotos', voice: 'autoVoice', gifs: 'autoGifs' };

/** Data and storage: what stays on this device, and what's fetched by itself. */
function Storage({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'storage' }> }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const { settings, usage, busy, cleared } = screen;
  const size = (bytes: number) => formatSize(language, bytes);
  const clear = async (conv?: string) => {
    const choice = await app.popup({
      message: tr('clearMediaConfirm'),
      buttons: [
        { id: 'clear', type: 'destructive', text: tr('clearMedia') },
        { id: 'cancel', type: 'cancel' },
      ],
    });
    if (choice === 'clear') void app.clearMedia(conv);
  };
  return (
    <Page title={tr('storageTitle')} busy={busy}>
      {cleared !== null && cleared > 0 && (
        <p class="notice" role="status">
          {tr('storageCleared', { size: size(cleared) })}
        </p>
      )}
      {usage && <p>{usage.media > 0 ? tr('storageMedia', { size: size(usage.media) }) : tr('noStoredMedia')}</p>}
      {usage?.app && <p class="muted">{tr('storageApp', { size: size(usage.app.usage) })}</p>}
      <fieldset class="settings" disabled={busy}>
        <legend>{tr('keepMedia')}</legend>
        {(['forever', 'month', 'week'] as const).map((keep) => (
          <label class="option" key={keep}>
            <input type="radio" name="keep-media" checked={settings.keep === keep} onChange={() => void app.setKeepMedia(keep)} />
            {tr(KEEP_TEXT[keep])}
          </label>
        ))}
      </fieldset>
      <fieldset class="settings" disabled={busy}>
        <legend>{tr('autoDownload')}</legend>
        {(['photos', 'voice', 'gifs'] as const).map((kind) => (
          <label class="option" key={kind}>
            <input type="checkbox" checked={settings.auto[kind]} onChange={(e) => void app.setAutoDownload(kind, e.currentTarget.checked)} />
            {tr(AUTO_TEXT[kind])}
          </label>
        ))}
      </fieldset>
      {usage && usage.chats.length > 0 && (
        <section class="settings">
          <h2>{tr('storageChats')}</h2>
          <ul class="chats">
            {usage.chats.map((c) => (
              <li key={c.id} class="storage-row">
                <span class="storage-chat">
                  <bdi dir="ltr" class="label">
                    {c.label}
                  </bdi>
                  <span class="muted">{size(c.bytes)}</span>
                </span>
                <Button kind="secondary" disabled={busy} onClick={() => void clear(c.id)}>
                  {tr('clearMedia')}
                </Button>
              </li>
            ))}
          </ul>
          <button type="button" class="link-button" disabled={busy} onClick={() => void clear()}>
            {tr('clearAllMedia')}
          </button>
        </section>
      )}
      <p class="muted">{tr('storageNote')}</p>
    </Page>
  );
}

/** Asks, in Telegram's own popup, before swapping the link for a new one. */
async function confirmReplace(app: AppState, language: Language): Promise<void> {
  const choice = await app.popup({
    message: t(language, 'newLinkConfirm'),
    buttons: [
      { id: 'replace', type: 'default', text: t(language, 'newLink') },
      { id: 'cancel', type: 'cancel' },
    ],
  });
  if (choice === 'replace') await app.replaceLink();
}

/** Telegram-style delivery marks: a clock while sending, one check when sent, two once seen. */
function Ticks({ status, language }: { status: MessageView['status']; language: Language }) {
  switch (status) {
    case 'sending':
      return (
        <svg class="tick" viewBox="0 0 12 12" role="img" aria-label={t(language, 'sending')}>
          <circle cx="6" cy="6" r="4.75" />
          <path d="M6 3.5V6l1.75 1.25" />
        </svg>
      );
    case 'sent':
      return (
        <svg class="tick" viewBox="0 0 16 12" role="img" aria-label={t(language, 'sent')}>
          <path d="M1.5 6.5l3.5 3.5L14.5 1.5" />
        </svg>
      );
    case 'seen':
      return (
        <svg class="tick" viewBox="0 0 20 12" role="img" aria-label={t(language, 'seen')}>
          <path d="M1.5 6.5l3.5 3.5L14.5 1.5" />
          <path d="M9.5 9.5l.5.5L18.5 1.5" />
        </svg>
      );
    case 'failed':
      return (
        <svg class="tick failed" viewBox="0 0 12 12" role="img" aria-label={t(language, 'notSent')}>
          <circle cx="6" cy="6" r="4.75" />
          <path d="M6 3.5v3M6 8.5v.01" />
        </svg>
      );
    default:
      return null;
  }
}

/** The Delete button behind a swiped chat, in px. */
const ACTION_WIDTH = 80;
const LONG_PRESS_MS = 500;
/** How far a finger moves before it counts as a swipe or a scroll, in px. */
const SLOP = 10;

type Gesture = { x: number; y: number; pointer: number; mode: 'pending' | 'swipe' | 'scroll'; base: number; timer: number; pressed: boolean };

/**
 * A chat in the list. Tap opens it; swiping it toward the start reveals
 * Delete; a long press (or a right click) asks to delete it straight away.
 */
function ChatRow({
  app,
  language,
  chat: c,
  revealed,
  onReveal,
}: {
  app: AppState;
  language: Language;
  chat: Extract<Screen, { kind: 'home' }>['conversations'][number];
  revealed: boolean;
  onReveal: (id: string | null) => void;
}) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const gesture = useRef<Gesture | null>(null);
  const suppressClick = useRef(false);
  // How far the chat is pulled aside while dragging; null when not dragging.
  const dragging = useSignal<number | null>(null);
  const shown = dragging.value ?? (revealed ? ACTION_WIDTH : 0);
  // Swipes go toward the inline start: left in English, right in Persian.
  const direction = () => (document.documentElement.dir === 'rtl' ? -1 : 1);

  const longPress = () => {
    const g = gesture.current;
    if (g && (g.pressed || g.mode !== 'pending')) return;
    if (g) {
      g.pressed = true;
      clearTimeout(g.timer);
      suppressClick.current = true;
    }
    onReveal(null);
    app.haptic();
    void confirmDelete(app, language, c.id, c.label, c.status === 'active');
  };
  const onPointerDown = (e: PointerEvent) => {
    if (e.button !== 0) return;
    gesture.current = {
      x: e.clientX,
      y: e.clientY,
      pointer: e.pointerId,
      mode: 'pending',
      base: revealed ? ACTION_WIDTH : 0,
      timer: window.setTimeout(longPress, LONG_PRESS_MS),
      pressed: false,
    };
  };
  const onPointerMove = (e: PointerEvent) => {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointer || g.pressed) return;
    const dx = e.clientX - g.x;
    const dy = e.clientY - g.y;
    if (g.mode === 'pending') {
      if (Math.abs(dx) < SLOP && Math.abs(dy) < SLOP) return;
      clearTimeout(g.timer);
      g.mode = Math.abs(dx) > Math.abs(dy) ? 'swipe' : 'scroll';
      if (g.mode === 'swipe') (e.currentTarget as Element).setPointerCapture(e.pointerId);
    }
    if (g.mode === 'swipe') dragging.value = Math.min(ACTION_WIDTH, Math.max(0, g.base - dx * direction()));
  };
  const onPointerEnd = (e: PointerEvent) => {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointer) return;
    clearTimeout(g.timer);
    gesture.current = null;
    if (g.mode !== 'swipe') return;
    suppressClick.current = true;
    const open = (dragging.value ?? 0) > ACTION_WIDTH / 2;
    dragging.value = null;
    onReveal(open ? c.id : null);
  };
  const onClick = () => {
    if (suppressClick.current) {
      suppressClick.current = false;
      return;
    }
    if (revealed) return onReveal(null);
    void app.openConversation(c.id);
  };

  return (
    <li class={dragging.value === null ? 'chat-row' : 'chat-row dragging'}>
      <button
        type="button"
        class="chat-action"
        tabIndex={shown > 0 ? 0 : -1}
        aria-hidden={shown === 0}
        onClick={() => {
          onReveal(null);
          void confirmDelete(app, language, c.id, c.label, c.status === 'active');
        }}
      >
        {tr('delete')}
      </button>
      <button
        type="button"
        class="chat"
        style={{ transform: `translateX(${-shown * direction()}px)` }}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onPointerCancel={onPointerEnd}
        onContextMenu={(e) => {
          e.preventDefault();
          longPress();
        }}
        onClick={onClick}
      >
        <span class="chat-top">
          <bdi dir="ltr" class="label">
            {c.label}
          </bdi>
          <span class="muted chat-time">
            {c.last?.out && <Ticks status={c.last.status} language={language} />}
            {relativeTime(language, new Date(c.lastAt))}
          </span>
        </span>
        <span class="chat-bottom">
          <span class="preview" dir="auto">
            {c.last ? (c.last.out ? tr('youWrote', { text: contentLine(language, c.last) }) : contentLine(language, c.last)) : ''}
          </span>
          {c.unread > 0 && <span class="badge">{t(language, 'unreadCount', { count: c.unread })}</span>}
        </span>
      </button>
    </li>
  );
}

function Composer({
  language,
  busy,
  onSend,
  reply = null,
  onCancelReply,
  onAttach,
  onVoice,
  onRound,
  onSticker,
}: {
  language: Language;
  busy: boolean;
  onSend: (text: string) => Promise<void>;
  /** The message being answered: who wrote it, and its text. */
  reply?: { name: string; text: string } | null;
  onCancelReply?: () => void;
  /** Sends a picked file (absent when attachments are off). */
  onAttach?: (file: File, as: SendAs, caption: string) => Promise<void>;
  /** Sends a recorded voice message (absent when attachments are off). */
  onVoice?: (recording: Recording) => Promise<void>;
  /** Sends a recorded video message, as it's finished off, with its last frame (absent when attachments are off). */
  onRound?: (recording: Promise<RoundRecording | null>, still: string | null) => Promise<void>;
  /** Sends a sticker. */
  onSticker?: (sticker: Sticker) => Promise<void>;
}) {
  const text = useSignal('');
  const stickers = useSignal(false);
  useBackCloses(stickers.value, () => (stickers.value = false));
  // Where the cursor was in the message box, for typing from the emoji panel.
  const caret = useRef<Caret>({ start: 0, end: 0 });
  const keepCaret = (box: HTMLTextAreaElement) => (caret.current = { start: box.selectionStart, end: box.selectionEnd });
  const typeIn = (edit: (text: string, at: Caret) => { text: string; caret: Caret }) => {
    const done = edit(text.value, caret.current);
    text.value = done.text;
    caret.current = done.caret;
  };
  const rec = useRecorder(language, onVoice, onRound);
  const attachMenu = useSignal(false);
  useBackCloses(attachMenu.value, () => (attachMenu.value = false));
  const picked = useSignal<Picked | null>(null);
  const photoInput = useRef<HTMLInputElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  // From the gallery, a photo, video or GIF is shown as one; from Files, it's a file unless changed.
  const pick = (fromGallery: boolean) => (e: Event) => {
    const input = e.currentTarget as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (file) picked.value = { file, shown: fromGallery };
  };
  const input = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    if (reply) input.current?.focus();
  }, [reply?.text]);
  const send = async (e: MouseEvent) => {
    const value = text.value;
    if (value.trim() === '') return;
    // The button goes (the microphone takes its place): from the keyboard, back to the box.
    if (document.activeElement === e.currentTarget) input.current?.focus();
    text.value = '';
    await onSend(value);
  };
  const recording = rec.state !== 'idle';
  // One row to start, as in Telegram; it grows with the text up to MAX_ROWS, then scrolls.
  useLayoutEffect(() => {
    const box = input.current;
    if (!box) return;
    const atEnd = isAtEnd();
    const style = getComputedStyle(box);
    const px = (v: string) => parseFloat(v) || 0;
    const borders = px(style.borderTopWidth) + px(style.borderBottomWidth);
    const max = (px(style.lineHeight) || 24) * MAX_ROWS + px(style.paddingTop) + px(style.paddingBottom) + borders;
    box.style.height = 'auto';
    const wanted = box.scrollHeight + borders;
    box.style.height = `${Math.min(wanted, max)}px`;
    box.style.overflowY = wanted > max ? 'auto' : 'hidden';
    // Growing pushes the end of the chat under the composer: keep it in view.
    if (atEnd) scrollToEnd();
  }, [text.value, recording]);
  const locked = rec.state === 'locked';
  // As Telegram's bar: one rounded field with the sticker button, the text,
  // the attach button and the record (or send) button; mirrored for
  // right-to-left. The record button keeps its element (and the press on
  // it) from idle to holding to locked, so the children are flat and keyed.
  return (
    <div class="composer-wrap" onKeyDown={(e) => e.key === 'Escape' && recording && rec.cancel()}>
      {reply && (
        <div class="reply-bar">
          <Quote name={t(language, 'replyingTo', { name: reply.name })} text={reply.text} />
          <button type="button" class="reply-cancel" aria-label={t(language, 'cancelReply')} onClick={onCancelReply}>
            ×
          </button>
        </div>
      )}
      <div class={rec.mode === 'round' && recording ? 'composer recording-round' : 'composer'}>
        {!recording && onSticker ? (
          <button
            key="stickers"
            type="button"
            class="bar-icon"
            aria-label={t(language, 'emojiAndStickers')}
            aria-expanded={stickers.value}
            disabled={busy}
            onClick={() => {
              const atEnd = isAtEnd();
              stickers.value = !stickers.value;
              if (stickers.value) {
                input.current?.blur();
                loadStickerArt();
                if (atEnd) requestAnimationFrame(() => scrollToEnd());
              } else input.current?.focus();
            }}
          >
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              {stickers.value ? (
                <path d="M3 6h18v12H3zM7 10h.01M11 10h.01M15 10h.01M7 14h10" />
              ) : (
                <path d="M12 21a9 9 0 100-18 9 9 0 000 18zM8.5 14.5a4.5 4.5 0 007 0M9 9.5h.01M15 9.5h.01" />
              )}
            </svg>
          </button>
        ) : null}
        {locked ? (
          <button key="trash" type="button" class="bar-icon" aria-label={t(language, 'cancelRecording')} onClick={rec.cancel}>
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />
            </svg>
          </button>
        ) : null}
        {onAttach ? <input key="gallery" type="file" accept="image/*,video/*" hidden ref={photoInput} onChange={pick(true)} /> : null}
        {onAttach ? <input key="files" type="file" hidden ref={fileInput} onChange={pick(false)} /> : null}
        {recording ? (
          <div key="bar" class="recorder" role="status">
            <span class="rec-dot" />
            <bdi class="rec-time" dir="ltr">
              {formatLength(language, Math.floor(rec.elapsed / 1000))}
            </bdi>
            {locked ? (
              rec.mode === 'voice' && (
                <span class="meter" dir="ltr" aria-hidden="true">
                  {rec.levels.map((level, i) => (
                    <span key={i} style={{ blockSize: `${Math.max(8, level * 100)}%` }} />
                  ))}
                </span>
              )
            ) : (
              <span class="slide-cancel" style={{ transform: `translateX(${-rec.slide * (document.documentElement.dir === 'rtl' ? -1 : 1)}px)`, opacity: Math.max(0.2, 1 - rec.slide / 120) }}>
                <svg class="icon-stroke chevron" viewBox="0 0 24 24" aria-hidden="true">
                  <path d="M15 5l-7 7 7 7" />
                </svg>
                {t(language, 'slideToCancel')}
              </span>
            )}
          </div>
        ) : (
          <textarea
            key="text"
            ref={input}
            // Empty, it follows the page (the placeholder starts at the right in Persian); typed in, the text.
            dir={text.value ? 'auto' : undefined}
            rows={1}
            maxLength={MAX_TEXT}
            placeholder={t(language, 'messagePlaceholder')}
            value={text.value}
            disabled={busy}
            onInput={(e) => {
              text.value = e.currentTarget.value;
              keepCaret(e.currentTarget);
            }}
            onSelect={(e) => keepCaret(e.currentTarget)}
            onBlur={(e) => keepCaret(e.currentTarget)}
            onFocus={() => (stickers.value = false)}
          />
        )}
        {!recording && onAttach ? (
          <button key="attach" type="button" class="bar-icon attach-icon" aria-label={t(language, 'attach')} disabled={busy} onClick={() => (attachMenu.value = true)}>
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M16.5 6.5l-8 8a2.5 2.5 0 003.5 3.5l8.5-8.5a4.5 4.5 0 00-6.4-6.4L5.5 11.7a6.5 6.5 0 009.2 9.2l6.8-6.8" />
            </svg>
          </button>
        ) : null}
        {rec.available && (recording || text.value.trim() === '') ? (
          <button
            key="record"
            type="button"
            class={rec.state === 'holding' || rec.state === 'starting' ? 'round-button record holding' : 'round-button record'}
            aria-label={t(language, locked ? 'send' : rec.mode === 'round' ? 'recordRound' : 'recordVoice')}
            disabled={busy}
            {...rec.press}
          >
            {(rec.state === 'holding' || rec.state === 'starting') && (
              <span class="lock-hint" style={{ transform: `translateY(${-rec.lift}px)` }} aria-hidden="true">
                <svg class="icon-stroke" viewBox="0 0 24 24">
                  <path d="M7 11V8a5 5 0 0110 0v3M6 11h12v9H6z" />
                </svg>
                <svg class="icon-stroke" viewBox="0 0 24 24">
                  <path d="M6 15l6-6 6 6" />
                </svg>
              </span>
            )}
            <svg class={locked ? 'icon-stroke send-icon' : 'icon-stroke'} viewBox="0 0 24 24" aria-hidden="true">
              <path
                d={
                  locked
                    ? 'M3 11.5L21 3l-8.5 18-2-7.5z'
                    : rec.mode === 'round'
                      ? 'M3 7h12v10H3zM15 10.5l6-3.5v10l-6-3.5'
                      : 'M12 3a3 3 0 00-3 3v6a3 3 0 006 0V6a3 3 0 00-3-3zM5 11a7 7 0 0014 0M12 18v3'
                }
              />
            </svg>
          </button>
        ) : (
          <button
            key="send"
            type="button"
            class="round-button"
            aria-label={t(language, 'send')}
            disabled={busy || text.value.trim() === ''}
            // Keeps the focus (and the keyboard) in the message box, as in Telegram.
            onMouseDown={(e) => e.preventDefault()}
            onClick={(e) => void send(e)}
          >
            <svg class="icon-stroke send-icon" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M3 11.5L21 3l-8.5 18-2-7.5z" />
            </svg>
          </button>
        )}
      </div>
      {stickers.value && onSticker && !recording && (
        <StickerPanel
          language={language}
          busy={busy}
          onPick={(s) => void onSticker({ id: s.id, e: s.e })}
          onEmoji={(e) => typeIn((value, at) => insertAt(value, at, e, MAX_TEXT))}
          onDelete={() => typeIn(deleteBefore)}
        />
      )}
      {rec.mode === 'round' && recording && <RoundStage live={rec.live} elapsed={rec.elapsed} locked={locked} language={language} />}
      {attachMenu.value && (
        <div class="overlay" onClick={() => (attachMenu.value = false)} onKeyDown={(e) => e.key === 'Escape' && (attachMenu.value = false)}>
          <div class="dialog menu" role="dialog" aria-modal="true" aria-label={t(language, 'attach')} onClick={(e) => e.stopPropagation()}>
            <button
              type="button"
              class="menu-item"
              onClick={() => {
                attachMenu.value = false;
                photoInput.current?.click();
              }}
            >
              {t(language, 'attachPhoto')}
            </button>
            <button
              type="button"
              class="menu-item"
              onClick={() => {
                attachMenu.value = false;
                fileInput.current?.click();
              }}
            >
              {t(language, 'attachFile')}
            </button>
          </div>
        </div>
      )}
      {picked.value && onAttach && (
        <AttachDialog
          language={language}
          picked={picked.value}
          onCancel={() => (picked.value = null)}
          onSend={(as, caption) => {
            const { file } = picked.value!;
            picked.value = null;
            void onAttach(file, as, caption);
          }}
        />
      )}
    </div>
  );
}

/** The sticker images, once loaded (stickers/catalog.ts). */
const stickerArt = signal<Readonly<Record<string, string>> | null>(null);
function loadStickerArt(): void {
  if (!stickerArt.peek()) void loadArt().then((art) => (stickerArt.value = art), () => {});
}

/** Stickers sent since the app opened, newest first: the Recent tab. Kept in memory only. */
const recentStickers = signal<string[]>([]);
const PACK_NAMES: Record<string, Key> = { faces: 'packFaces', love: 'packLove', animals: 'packAnimals' };

type PanelSection = 'emoji' | 'stickers';
/** The panel's section last used, while the app is open. */
const panelSection = signal<PanelSection>('emoji');
/** Emoji used in this session, newest first (not kept on the device, like recent stickers). */
const recentEmoji = signal<string[]>([]);
const EMOJI_LISTS = Object.fromEntries(EMOJI_GROUPS.map((g) => [g.id, g.emoji.split(' ')])) as Record<EmojiGroup['id'], string[]>;
const EMOJI_GROUP_TEXT: Record<EmojiGroup['id'], Key> = {
  people: 'emojiPeople',
  nature: 'emojiNature',
  food: 'emojiFood',
  activity: 'emojiActivity',
  travel: 'emojiTravel',
  objects: 'emojiObjects',
  symbols: 'emojiSymbols',
  flags: 'emojiFlags',
};
const EMOJI_GROUP_ICON: Record<EmojiGroup['id'], string> = {
  people: '😀',
  nature: '🐻',
  food: '🍔',
  activity: '⚽',
  travel: '🚗',
  objects: '💡',
  symbols: '🔣',
  flags: '🏳️',
};

/** The emoji and sticker panel under the message box, as in Telegram: emoji go into the text, a sticker is sent at once. */
function StickerPanel({
  language,
  busy,
  onPick,
  onEmoji,
  onDelete,
}: {
  language: Language;
  busy: boolean;
  onPick: (sticker: StickerInfo) => void;
  onEmoji: (emoji: string) => void;
  onDelete: () => void;
}) {
  const section = panelSection.value;
  const mode = (to: PanelSection) => (
    <button type="button" class="panel-mode" aria-pressed={section === to} onClick={() => (panelSection.value = to)}>
      {t(language, to === 'emoji' ? 'emoji' : 'stickers')}
    </button>
  );
  return (
    <div class="sticker-panel">
      {section === 'emoji' ? <EmojiSection language={language} onEmoji={onEmoji} /> : <StickerSection language={language} busy={busy} onPick={onPick} />}
      <div class="panel-switch">
        {mode('emoji')}
        {mode('stickers')}
        {section === 'emoji' && (
          <button type="button" class="bar-icon panel-delete" aria-label={t(language, 'deleteCharacter')} onClick={onDelete}>
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M21 5H8l-6 7 6 7h13zM11 9l6 6M17 9l-6 6" />
            </svg>
          </button>
        )}
      </div>
    </div>
  );
}

function EmojiSection({ language, onEmoji }: { language: Language; onEmoji: (emoji: string) => void }) {
  const recent = recentEmoji.value;
  const group = useSignal<EmojiGroup['id'] | 'recent'>(recent.length > 0 ? 'recent' : 'people');
  const shown = group.value === 'recent' ? recent : EMOJI_LISTS[group.value];
  const pick = (emoji: string) => {
    // On the Recent tab, the order stays put under the finger.
    if (!(group.peek() === 'recent' && recentEmoji.peek().includes(emoji))) {
      recentEmoji.value = [emoji, ...recentEmoji.peek().filter((e) => e !== emoji)].slice(0, 32);
    }
    onEmoji(emoji);
  };
  return (
    <>
      <div class="sticker-tabs" role="tablist">
        {recent.length > 0 && (
          <button type="button" role="tab" class="sticker-tab" aria-selected={group.value === 'recent'} aria-label={t(language, 'recentStickers')} onClick={() => (group.value = 'recent')}>
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M12 7v5l3 2M21 12a9 9 0 11-9-9 9 9 0 019 9z" />
            </svg>
          </button>
        )}
        {EMOJI_GROUPS.map((g) => (
          <button
            key={g.id}
            type="button"
            role="tab"
            class="sticker-tab emoji-tab"
            aria-selected={group.value === g.id}
            aria-label={t(language, EMOJI_GROUP_TEXT[g.id])}
            onClick={() => (group.value = g.id)}
          >
            {EMOJI_GROUP_ICON[g.id]}
          </button>
        ))}
      </div>
      {/* Keyed by tab, so each one starts at its top. */}
      <div key={group.value} class="emoji-grid" role="tabpanel">
        {shown.map((e) => (
          <button key={e} type="button" class="emoji-cell" onClick={() => pick(e)}>
            {e}
          </button>
        ))}
      </div>
    </>
  );
}

/** The stickers: a tab per pack (and Recent), a tap sends. */
function StickerSection({ language, busy, onPick }: { language: Language; busy: boolean; onPick: (sticker: StickerInfo) => void }) {
  const art = stickerArt.value;
  const recent = recentStickers.value.map(findSticker).filter((s): s is StickerInfo => !!s);
  const tab = useSignal(recent.length > 0 ? 'recent' : PACKS[0]!.id);
  const shown = tab.value === 'recent' ? recent : (PACKS.find((p) => p.id === tab.value)?.stickers ?? []);
  const pick = (sticker: StickerInfo) => {
    recentStickers.value = [sticker.id, ...recentStickers.peek().filter((id) => id !== sticker.id)].slice(0, 20);
    onPick(sticker);
  };
  return (
    <>
      <div class="sticker-tabs" role="tablist">
        {recent.length > 0 && (
          <button type="button" role="tab" class="sticker-tab" aria-selected={tab.value === 'recent'} aria-label={t(language, 'recentStickers')} onClick={() => (tab.value = 'recent')}>
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M12 7v5l3 2M21 12a9 9 0 11-9-9 9 9 0 019 9z" />
            </svg>
          </button>
        )}
        {PACKS.map((p) => {
          const cover = art?.[p.stickers[0]!.id];
          const name = PACK_NAMES[p.id] ? t(language, PACK_NAMES[p.id]!) : p.id;
          return (
            <button key={p.id} type="button" role="tab" class="sticker-tab" aria-selected={tab.value === p.id} aria-label={name} onClick={() => (tab.value = p.id)}>
              {cover ? <img src={cover} alt="" /> : <span aria-hidden="true">{p.stickers[0]!.e}</span>}
            </button>
          );
        })}
      </div>
      <div key={tab.value} class="sticker-grid" role="tabpanel">
        {shown.map((s) => (
          <button key={s.id} type="button" class="sticker-cell" aria-label={s.e} disabled={busy} onClick={() => pick(s)}>
            {art?.[s.id] ? <img src={art[s.id]} alt="" draggable={false} /> : <span aria-hidden="true">{s.e}</span>}
          </button>
        ))}
        <details class="sticker-credit">
          <summary>{ART_CREDIT}</summary>
          <pre dir="ltr">{ART_LICENSE}</pre>
        </details>
      </div>
    </>
  );
}

/** A sticker in a message; one this version doesn't have (a newer pack) shows as its emoji. */
function StickerView({ sticker, language }: { sticker: Sticker; language: Language }) {
  const emoji = findSticker(sticker.id)?.e ?? sticker.e ?? '';
  const url = stickerArt.value?.[sticker.id];
  const label = `${emoji} ${t(language, 'sticker')}`.trim();
  return url ? (
    <img class="sticker" src={url} alt={label} draggable={false} />
  ) : (
    <span class="sticker missing" role="img" aria-label={label}>
      {emoji || '🖼️'}
    </span>
  );
}

const MIC_TEXT: Record<RecordingUnavailable['reason'], Key> = { denied: 'micDenied', missing: 'micMissing', unsupported: 'micUnavailable' };
const CAMERA_TEXT: Record<RecordingUnavailable['reason'], Key> = { denied: 'cameraDenied', missing: 'cameraMissing', unsupported: 'cameraUnavailable' };

type RecordMode = 'voice' | 'round';
/** What the record button records, voice or video messages: a tap switches it. Remembered while the app is open. */
const recordMode = signal<RecordMode>('voice');
/** The most rows the message box grows to; past that, it scrolls. */
const MAX_ROWS = 6;
/** The longest message, in UTF-16 code units (the box's maxLength). */
const MAX_TEXT = 4000;
/** A press longer than this records; a shorter one is a tap. */
const HOLD_MS = 250;
/** How far up a press slides to lock the recording, and toward the start to drop it, in px. */
const LOCK_LIFT = 72;
const CANCEL_SLIDE = 110;
/** A microphone or camera slower than this to start was most likely behind a permission prompt. */
const PROMPT_MS = 1000;

interface Press {
  x: number;
  y: number;
  pointer: number;
  /** Until the press counts as a hold. */
  timer: number;
  /** A tap on the send button of a locked recording. */
  send: boolean;
  locked: boolean;
  cancelled: boolean;
}

/**
 * Recording voice and video messages from the composer, as in Telegram:
 * hold the button to record and let go to send; slide toward the start to
 * drop it, or up to lock it (then the bin drops it and the button sends). A
 * tap switches between voice and video. From the keyboard, Enter starts a
 * locked recording. A press that a permission prompt (or anything else)
 * takes away isn't dropped: the recording goes on, locked. Leaving the
 * conversation drops a recording in progress;
 * at the longest length (15 minutes, a minute for video) it's sent.
 */
function useRecorder(
  language: Language,
  onVoice: ((recording: Recording) => Promise<void>) | undefined,
  onRound: ((recording: Promise<RoundRecording | null>, still: string | null) => Promise<void>) | undefined,
) {
  const state = useSignal<'idle' | 'starting' | 'holding' | 'locked'>('idle');
  const mode = useSignal<RecordMode>('voice');
  const voice = useRef<Recorder | null>(null);
  const live = useSignal<RoundRecorder | null>(null);
  const elapsed = useSignal(0);
  const levels = useSignal<number[]>([]);
  const slide = useSignal(0);
  const lift = useSignal(0);
  const press = useRef<Press | null>(null);
  const alive = useRef(true);
  const camera = useSignal(false);
  useEffect(() => {
    void hasCamera().then((has) => (camera.value = has));
    return () => {
      alive.current = false;
      voice.current?.cancel();
      live.peek()?.cancel();
    };
  }, []);
  const reset = () => {
    state.value = 'idle';
    voice.current = null;
    live.value = null;
    slide.value = 0;
    lift.value = 0;
  };
  const cancel = () => {
    voice.current?.cancel();
    live.peek()?.cancel();
    reset();
  };
  const finish = async () => {
    const v = voice.current;
    const r = live.peek();
    reset();
    try {
      if (v && onVoice) {
        const result = await v.stop();
        if (result) await onVoice(result);
      } else if (r && onRound) {
        // Shown in the chat at once: finishing the recording can take seconds.
        const still = r.snapshot();
        await onRound(r.stop(), still);
      }
    } catch {
      say(t(language, v ? 'micUnavailable' : 'cameraUnavailable'));
    }
  };
  // Started from the keyboard, or the press is still on (or locked) and wasn't slid away.
  const wanted = (p: Press | null) => alive.current && state.peek() === 'starting' && (!p || ((press.current === p || p.locked) && !p.cancelled));
  const begin = async (p: Press | null) => {
    const m = recordMode.peek();
    mode.value = m;
    state.value = 'starting';
    elapsed.value = 0;
    levels.value = [];
    // A permission prompt takes the press away (Telegram on Android asks
    // every time): it ends without a let-go, or the page loses focus, or it's
    // just slow. Then the recording isn't dropped but starts locked.
    const asked = performance.now();
    let interrupted = false;
    const interrupt = () => (interrupted = true);
    window.addEventListener('blur', interrupt);
    document.addEventListener('visibilitychange', interrupt);
    try {
      const r = m === 'voice' ? await startRecording((level) => (levels.value = [...levels.peek().slice(-31), level])) : await startRound();
      if (p && !p.cancelled && press.current !== p && (interrupted || performance.now() - asked > PROMPT_MS)) p.locked = true;
      if (!wanted(p)) {
        // Let go (or slid away) before the microphone or camera was ready.
        r.cancel();
        if (alive.current && state.peek() === 'starting') reset();
        return;
      }
      if (m === 'voice') voice.current = r as Recorder;
      else live.value = r as RoundRecorder;
      state.value = !p || p.locked ? 'locked' : 'holding';
    } catch (err) {
      if (state.peek() === 'starting') reset();
      const texts = m === 'voice' ? MIC_TEXT : CAMERA_TEXT;
      say(t(language, err instanceof RecordingUnavailable ? texts[err.reason] : texts.unsupported));
    } finally {
      window.removeEventListener('blur', interrupt);
      document.removeEventListener('visibilitychange', interrupt);
    }
  };
  const toggle = () => {
    if (!onRound || !camera.value) return say(t(language, 'holdToRecord'));
    recordMode.value = recordMode.peek() === 'voice' ? 'round' : 'voice';
    say(t(language, recordMode.value === 'voice' ? 'holdForVoice' : 'holdForRound'));
  };
  const end = (e: PointerEvent, cancelled: boolean) => {
    const p = press.current;
    if (!p || p.pointer !== e.pointerId) return;
    press.current = null;
    if (p.send) {
      if (!cancelled && state.peek() === 'locked') void finish();
    } else if (p.timer) {
      clearTimeout(p.timer);
      if (!cancelled) toggle();
    } else if (!p.cancelled && !p.locked && state.peek() === 'holding') {
      // Taken away mid-recording (a dialog, a system gesture): kept, locked.
      if (cancelled) state.value = 'locked';
      else void finish();
    } else if (!p.cancelled && !p.locked) {
      // Still starting. Taken away (a permission prompt): it starts locked.
      // Let go: begin() drops it once it's ready, unless that took long.
      if (cancelled) p.locked = true;
      slide.value = 0;
      lift.value = 0;
    }
  };
  const recording = state.value === 'holding' || state.value === 'locked';
  useEffect(() => {
    if (!recording) return;
    const timer = setInterval(() => {
      const r = voice.current ?? live.peek();
      elapsed.value = r?.elapsed() ?? 0;
      if (elapsed.value >= (mode.peek() === 'round' ? MAX_ROUND_MS : MAX_VOICE_MS)) void finish();
    }, 100);
    return () => clearInterval(timer);
  }, [recording]);
  return {
    available: !!onVoice && canRecord(),
    state: state.value,
    mode: state.value === 'idle' ? recordMode.value : mode.value,
    elapsed: elapsed.value,
    levels: levels.value,
    live: live.value,
    slide: slide.value,
    lift: lift.value,
    cancel,
    press: {
      onPointerDown: (e: PointerEvent) => {
        if (e.button !== 0) return;
        const old = press.current;
        if (old) {
          if (old.pointer === e.pointerId || old.timer) return;
          // That press never ended here (a prompt took it): it's locked now, and this tap sends.
          press.current = null;
          if (!old.cancelled && !old.send && !old.locked) {
            old.locked = true;
            if (state.peek() === 'holding') state.value = 'locked';
          }
        }
        const p: Press = { x: e.clientX, y: e.clientY, pointer: e.pointerId, timer: 0, send: false, locked: false, cancelled: false };
        if (state.peek() === 'locked') {
          press.current = { ...p, send: true };
          return;
        }
        if (state.peek() !== 'idle') return;
        try {
          (e.currentTarget as Element).setPointerCapture(e.pointerId);
        } catch {
          // Touches stay with their element anyway.
        }
        p.timer = window.setTimeout(() => {
          p.timer = 0;
          void begin(p);
        }, HOLD_MS);
        press.current = p;
      },
      onPointerMove: (e: PointerEvent) => {
        const p = press.current;
        if (!p || p.pointer !== e.pointerId || p.send || p.timer || p.cancelled || p.locked) return;
        const toStart = (p.x - e.clientX) * (document.documentElement.dir === 'rtl' ? -1 : 1);
        const up = p.y - e.clientY;
        slide.value = Math.max(0, toStart);
        lift.value = Math.max(0, Math.min(LOCK_LIFT, up));
        if (toStart > CANCEL_SLIDE) {
          p.cancelled = true;
          cancel();
        } else if (up >= LOCK_LIFT) {
          p.locked = true;
          slide.value = 0;
          lift.value = 0;
          if (state.peek() === 'holding') state.value = 'locked';
        }
      },
      onPointerUp: (e: PointerEvent) => end(e, false),
      onPointerCancel: (e: PointerEvent) => end(e, true),
      // Presses are handled above; a click without one is the keyboard.
      onClick: (e: MouseEvent) => {
        if (e.detail !== 0) return;
        if (state.peek() === 'idle') void begin(null);
        else if (state.peek() === 'locked') void finish();
      },
      onContextMenu: (e: MouseEvent) => e.preventDefault(),
    },
  };
}

/**
 * While recording a video message: the camera's picture in a circle above
 * the composer, with the time used of the minute around it, and (once
 * locked) a button to switch cameras.
 */
function RoundStage({ live, elapsed, locked, language }: { live: RoundRecorder | null; elapsed: number; locked: boolean; language: Language }) {
  const feed = useRef<HTMLDivElement>(null);
  useLayoutEffect(() => {
    const box = feed.current;
    if (!box || !live) return;
    live.video.classList.toggle('mirrored', !live.canvas && live.mirrored());
    box.append(live.video);
    if (live.canvas) box.append(live.canvas);
    return () => {
      live.video.remove();
      live.canvas?.remove();
    };
  }, [live]);
  const fraction = Math.min(1, elapsed / MAX_ROUND_MS);
  return createPortal(
    <div class="round-stage">
      <div class="round-live">
        <div class="round-feed" ref={feed} />
        {!live && <span class="spinner" />}
        <svg class="round-ring" viewBox="0 0 100 100" aria-hidden="true">
          <circle cx="50" cy="50" r="48.5" pathLength={1} style={{ strokeDashoffset: `${1 - fraction}` }} />
        </svg>
      </div>
      {locked && live?.flip && (
        <button type="button" class="viewer-button flip" aria-label={t(language, 'flipCamera')} onClick={() => void live.flip?.()}>
          <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
            <path d="M4 9h3l2-3h6l2 3h3v10H4zM9.5 14a2.5 2.5 0 004.9.7M14.5 13a2.5 2.5 0 00-4.9-.7M14.5 11.2v1.8h-1.8M9.5 15.8V14h1.8" />
          </svg>
        </button>
      )}
    </div>,
    document.body,
  );
}

/** A picked file, and whether it starts out shown as a photo, video or GIF (rather than as a file). */
interface Picked {
  file: File;
  shown: boolean;
}

/** The choice for each kind a file can be shown as: compress a photo, or send a video or GIF as one. */
const SHOW_AS: Record<'photo' | 'video' | 'gif' | 'audio', Key> = { photo: 'compressImage', video: 'sendAsVideo', gif: 'sendAsGif', audio: 'sendAsMusic' };

/** Before sending a picked file: a preview, a caption, and (for photos, videos and GIFs) whether to send it as one. */
function AttachDialog({
  language,
  picked,
  onCancel,
  onSend,
}: {
  language: Language;
  picked: Picked;
  onCancel: () => void;
  onSend: (as: SendAs, caption: string) => void;
}) {
  useBackCloses(true, onCancel);
  const tr = (key: Key) => t(language, key);
  const kind = showableAs(picked.file);
  // Music shows as music from Files too, as in Telegram.
  const shown = useSignal(kind !== null && (picked.shown || kind === 'audio'));
  const caption = useSignal('');
  const preview = useSignal<string | null>(null);
  // A video this browser can't play shows as a file here (and is sent as one).
  const unplayable = useSignal(false);
  useEffect(() => {
    if (kind !== 'photo' && kind !== 'gif' && kind !== 'video') return;
    const url = URL.createObjectURL(picked.file);
    preview.value = url;
    return () => URL.revokeObjectURL(url);
  }, [picked.file]);
  const showable = kind === 'video' && unplayable.value ? null : kind;
  const as: SendAs = showable && shown.value ? showable : 'file';
  const tooBig = picked.file.size > MAX_FILE_BYTES && as !== 'photo';
  return (
    <div class="overlay" onKeyDown={(e) => e.key === 'Escape' && onCancel()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-label={tr('attach')}>
        {preview.value && kind === 'video' && !unplayable.value ? (
          <video class="attach-preview" src={preview.value} muted playsInline autoPlay loop onError={() => (unplayable.value = true)} />
        ) : preview.value && (kind === 'photo' || kind === 'gif') ? (
          <img class="attach-preview" src={preview.value} alt="" />
        ) : (
          <div class="file">
            <span class="file-icon">{extension(picked.file.name) || '📄'}</span>
            <span class="file-info">
              <span class="file-name" dir="auto">
                {picked.file.name}
              </span>
              <span class="file-size">{formatSize(language, picked.file.size)}</span>
            </span>
          </div>
        )}
        {showable && (
          <label class="check">
            <input type="checkbox" checked={shown.value} onChange={(e) => (shown.value = e.currentTarget.checked)} />
            <span>{tr(SHOW_AS[showable])}</span>
          </label>
        )}
        {tooBig && <p class="warning">{tr('errorTooBig')}</p>}
        <input
          class="caption"
          type="text"
          dir="auto"
          maxLength={1024}
          placeholder={tr('captionPlaceholder')}
          value={caption.value}
          onInput={(e) => (caption.value = e.currentTarget.value)}
        />
        <div class="row">
          <Button disabled={tooBig} onClick={() => onSend(as, caption.value)}>
            {tr('send')}
          </Button>
          <Button kind="secondary" onClick={onCancel}>
            {tr('cancel')}
          </Button>
        </div>
      </div>
    </div>
  );
}

/** A message in a line: its text, with "📷 Photo", "📹 Video", "📎 name" and so on before a caption, or "😉 Sticker". For chat previews, quotes and the reply bar. */
function contentLine(language: Language, { text, media, sticker }: { text: string; media?: { t: string; n?: string } | undefined; sticker?: Sticker | undefined }): string {
  if (sticker) return `${findSticker(sticker.id)?.e ?? sticker.e ?? ''} ${t(language, 'sticker')}`.trim();
  if (!media) return text;
  const label = {
    photo: `📷 ${t(language, 'photo')}`,
    video: `📹 ${t(language, 'video')}`,
    gif: t(language, 'gif'),
    voice: `🎤 ${t(language, 'voice')}`,
    audio: `🎵 ${songTitle(media.n) || t(language, 'music')}`,
    round: `⏺️ ${t(language, 'roundMessage')}`,
    file: `📎 ${media.n ?? t(language, 'file')}`,
  }[shownAs(media)];
  return text ? `${label}, ${text}` : label;
}

/** A song's title: its file name without the extension. */
function songTitle(name: string | undefined): string {
  return name ? name.replace(/\.[A-Za-z0-9]{1,5}$/, '') : '';
}

function extension(name: string | undefined): string {
  const dot = name?.lastIndexOf('.') ?? -1;
  return dot > 0 ? name!.slice(dot + 1, dot + 5).toUpperCase() : '';
}

function formatSize(language: Language, bytes: number): string {
  const unit = bytes < 1024 * 1024 ? 'kilobyte' : 'megabyte';
  const n = unit === 'kilobyte' ? Math.max(1, Math.round(bytes / 1024)) : bytes / 1024 / 1024;
  // Persian spells the unit out (کیلوبایت), so it reads in the right direction.
  return new Intl.NumberFormat(locale(language), { style: 'unit', unit, unitDisplay: language === 'fa' ? 'long' : 'short', maximumFractionDigits: 1 }).format(n);
}

/** A progress ring (fraction 0–1; undefined spins). */
function Ring({ fraction }: { fraction: number | undefined }) {
  const c = 2 * Math.PI * 9;
  return (
    <svg class={fraction === undefined ? 'ring spin' : 'ring'} viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="9" style={{ strokeDasharray: `${c}`, strokeDashoffset: `${c * (1 - Math.max(0.05, fraction ?? 0.25))}` }} />
    </svg>
  );
}

/** A message's attachment: a photo, GIF, video or file. */
function Attachment({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  switch (shownAs(m.media!)) {
    case 'photo':
    case 'gif':
      return <PhotoView app={app} message={m} language={language} progress={progress} />;
    case 'video':
      return <VideoView app={app} message={m} language={language} progress={progress} />;
    case 'voice':
      return <VoiceView app={app} message={m} language={language} progress={progress} />;
    case 'audio':
      return <AudioView app={app} message={m} language={language} progress={progress} />;
    case 'round':
      return <RoundView app={app} message={m} language={language} progress={progress} />;
    default:
      return <FileView app={app} message={m} language={language} progress={progress} />;
  }
}

/** The object URL of a message's attachment, once it's on this device. */
function useMediaUrl(app: AppState, m: MessageView) {
  const url = useSignal<string | null>(null);
  useEffect(() => {
    let alive = true;
    if (m.file === 'local') void app.mediaUrl(m.id, m.media!.m).then((u) => alive && (url.value = u));
    return () => {
      alive = false;
    };
  }, [m.id, m.file]);
  return url;
}

/** Whether an attachment waits for a tap to download: one received, or one cleared from this device. */
const fetchable = (m: MessageView) => (m.file === 'remote' && !m.out) || m.file === 'cleared';

/** The tiny preview carried in the message, as an image URL. */
function tinyPreview(p: string | undefined): string | null {
  return p ? `data:image/jpeg;base64,${p.replace(/-/g, '+').replace(/_/g, '/')}` : null;
}

/** Progress, a download button, or "no longer available", over a photo or video. */
function MediaState({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  return (
    <>
      {progress !== undefined && (
        <span class="media-progress">
          <Ring fraction={progress} />
        </span>
      )}
      {fetchable(m) && progress === undefined && (
        <button type="button" class="media-progress" aria-label={t(language, 'download')} onClick={() => void app.fetchMedia(m.id)}>
          ↓
        </button>
      )}
      {m.file === 'gone' && <span class="media-gone">{t(language, 'mediaGone')}</span>}
    </>
  );
}

/** A photo or GIF: the tiny preview until it's here, then the image (a GIF plays); tap for full screen. */
function PhotoView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const url = useMediaUrl(app, m);
  const open = useSignal(false);
  const { w = 4, h = 3, p } = m.media!;
  const gif = m.media!.t === 'gif';
  const tiny = tinyPreview(p);
  const alt = t(language, gif ? 'gif' : 'photo');
  return (
    <div class="photo" style={{ aspectRatio: `${w} / ${h}` }}>
      {url.value ? <img src={url.value} alt={alt} onClick={() => (open.value = true)} /> : tiny && <img class="tiny" src={tiny} alt="" />}
      {gif && <span class="media-badge">{t(language, 'gif')}</span>}
      <MediaState app={app} message={m} language={language} progress={progress} />
      {open.value && url.value && (
        <Viewer language={language} onClose={() => (open.value = false)} onSave={() => void save(app, m, language)}>
          <img src={url.value} alt={alt} />
        </Viewer>
      )}
    </div>
  );
}

/**
 * A video: its preview frame, length (and size, until it's here) and a play
 * button; it plays full screen. Fetched on a tap, as Telegram does by default.
 */
function VideoView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const url = useMediaUrl(app, m);
  const open = useSignal(false);
  const { w = 16, h = 9, p, l, s } = m.media!;
  const tiny = tinyPreview(p);
  const length = l !== undefined ? formatLength(language, l) : null;
  const size = fetchable(m) ? formatSize(language, s) : null;
  return (
    <div class="photo" style={{ aspectRatio: `${w} / ${h}` }}>
      {tiny && <img class="tiny" src={tiny} alt="" />}
      {/* "#t" makes browsers that wait for a tap (iPhone) show a first frame. */}
      {url.value && <video class="poster" src={`${url.value}#t=0.1`} muted playsInline preload="metadata" onClick={() => (open.value = true)} />}
      {(length || size) && (
        <span class="media-badge">
          {length && <bdi dir="ltr">{length}</bdi>}
          {length && size && ' · '}
          {size}
        </span>
      )}
      <MediaState app={app} message={m} language={language} progress={progress} />
      {url.value && progress === undefined && (
        <button type="button" class="media-progress" aria-label={t(language, 'play')} onClick={() => (open.value = true)}>
          ▶
        </button>
      )}
      {open.value && url.value && (
        <Viewer language={language} onClose={() => (open.value = false)} onSave={() => void save(app, m, language)}>
          <Player src={url.value} language={language} />
        </Viewer>
      )}
    </div>
  );
}

/** A video playing full screen, with the browser's controls; says so if it can't play here. */
function Player({ src, language }: { src: string; language: Language }) {
  const failed = useSignal(false);
  return failed.value ? (
    <p class="viewer-note">{t(language, 'videoUnplayable')}</p>
  ) : (
    <video src={src} controls autoPlay playsInline onClick={(e) => e.stopPropagation()} onError={() => (failed.value = true)} />
  );
}

/** The voice message, song or video message playing with sound: starting another pauses it. */
let nowPlaying: HTMLMediaElement | null = null;

/** Plays an attachment's audio once it's on this device: play/pause, position, seeking and speed. */
function useAudio(url: string | null, knownLength: number | undefined) {
  const audio = useRef<HTMLAudioElement | null>(null);
  const playing = useSignal(false);
  const time = useSignal(0);
  const length = useSignal<number | null>(null);
  const failed = useSignal(false);
  const rate = useSignal(1);
  useEffect(() => {
    if (!url) return;
    const a = new Audio(url);
    a.preload = 'metadata';
    a.addEventListener('play', () => {
      if (nowPlaying && nowPlaying !== a) nowPlaying.pause();
      nowPlaying = a;
      playing.value = true;
    });
    a.addEventListener('pause', () => (playing.value = false));
    a.addEventListener('ended', () => {
      playing.value = false;
      time.value = 0;
    });
    a.addEventListener('timeupdate', () => (time.value = a.currentTime));
    a.addEventListener('loadedmetadata', () => Number.isFinite(a.duration) && (length.value = a.duration));
    a.addEventListener('error', () => (failed.value = true));
    audio.current = a;
    return () => {
      a.pause();
      if (nowPlaying === a) nowPlaying = null;
      a.removeAttribute('src');
      a.load();
      audio.current = null;
    };
  }, [url]);
  // Recorded WebM has no length in it; the message's own is used then.
  const total = length.value ?? knownLength ?? 0;
  return {
    playing: playing.value,
    failed: failed.value,
    time: time.value,
    total,
    fraction: total > 0 ? Math.min(1, time.value / total) : 0,
    rate: rate.value,
    toggle: () => {
      const a = audio.current;
      if (!a) return;
      if (!a.paused) return a.pause();
      a.playbackRate = rate.value;
      void a.play().catch((err: { name?: string }) => err.name === 'NotSupportedError' && (failed.value = true));
    },
    seek: (fraction: number) => {
      const a = audio.current;
      if (!a || total <= 0) return;
      a.currentTime = Math.max(0, Math.min(1, fraction)) * total;
      time.value = a.currentTime;
    },
    nextRate: () => {
      rate.value = rate.value === 1 ? 1.5 : rate.value === 1.5 ? 2 : 1;
      if (audio.current) audio.current.playbackRate = rate.value;
    },
  };
}

/** The round button of a voice message or song: progress, download, or play/pause. */
function PlayButton({ app, message: m, language, progress, playing, onPlay }: { app: AppState; message: MessageView; language: Language; progress: number | undefined; playing: boolean; onPlay: () => void }) {
  if (progress !== undefined) {
    return (
      <span class="play">
        <Ring fraction={progress} />
      </span>
    );
  }
  if (fetchable(m)) {
    return (
      <button type="button" class="play" aria-label={t(language, 'download')} onClick={() => void app.fetchMedia(m.id)}>
        ↓
      </button>
    );
  }
  return (
    <button type="button" class="play" aria-label={t(language, playing ? 'pause' : 'play')} disabled={m.file !== 'local'} onClick={onPlay}>
      <svg class="icon-fill" viewBox="0 0 24 24" aria-hidden="true">
        <path d={playing ? 'M7 5h4v14H7zM13 5h4v14h-4z' : 'M8 5v14l11-7z'} />
      </svg>
    </button>
  );
}

/** Where a tap or click fell along an element, 0–1 from its left (timelines run left to right). */
function along(e: MouseEvent): number {
  const box = (e.currentTarget as Element).getBoundingClientRect();
  return box.width > 0 ? (e.clientX - box.left) / box.width : 0;
}

/** Stops a press from reaching the message's swipe and long-press gestures. */
const keepPress = (e: PointerEvent) => e.stopPropagation();

/** A voice message: play/pause, the waveform (tap to jump), its length, and the speed. Fetched on opening, as in Telegram. */
function VoiceView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const url = useMediaUrl(app, m);
  const { l, v } = m.media!;
  const player = useAudio(url.value, l);
  let bars: number[];
  try {
    bars = v ? [...fromBase64Url(v)] : [];
  } catch {
    bars = [];
  }
  if (bars.length === 0) bars = new Array<number>(32).fill(0);
  const shown = player.playing || player.time > 0 ? player.time : player.total;
  return (
    <div class="voice">
      <PlayButton app={app} message={m} language={language} progress={progress} playing={player.playing} onPlay={player.toggle} />
      <div class="voice-body">
        <div class="waveform" dir="ltr" onPointerDown={keepPress} onClick={(e) => player.seek(along(e))}>
          {bars.map((b, i) => (
            <span key={i} class={(i + 0.5) / bars.length <= player.fraction ? 'played' : ''} style={{ blockSize: `${Math.max(12, (b / 255) * 100)}%` }} />
          ))}
        </div>
        <span class="media-meta">
          <bdi dir="ltr">{formatLength(language, Math.round(shown))}</bdi>
          {m.file === 'gone' && <span>{t(language, 'mediaGone')}</span>}
          {player.failed && <span>{t(language, 'audioUnplayable')}</span>}
          {url.value && !player.failed && (
            <button type="button" class="rate" aria-label={t(language, 'playbackSpeed')} onPointerDown={keepPress} onClick={player.nextRate}>
              <bdi dir="ltr">{`${player.rate}×`}</bdi>
            </button>
          )}
        </span>
      </div>
    </div>
  );
}

/** A song: play/pause, its title, a seek bar once it's here, and its length (and size, until it's here). Fetched on a tap. */
function AudioView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const url = useMediaUrl(app, m);
  const { l, n, s } = m.media!;
  const player = useAudio(url.value, l);
  const total = player.total ? formatLength(language, Math.round(player.total)) : null;
  return (
    <div class="audio">
      <PlayButton app={app} message={m} language={language} progress={progress} playing={player.playing} onPlay={player.toggle} />
      <div class="audio-body">
        <span class="audio-title" dir="auto">
          {songTitle(n) || t(language, 'music')}
        </span>
        {url.value && (
          <input
            class="seek"
            type="range"
            dir="ltr"
            min={0}
            max={1000}
            value={Math.round(player.fraction * 1000)}
            aria-label={t(language, 'seek')}
            onPointerDown={keepPress}
            onInput={(e) => player.seek(Number(e.currentTarget.value) / 1000)}
          />
        )}
        <span class="media-meta">
          {url.value ? (
            <bdi dir="ltr">
              {formatLength(language, Math.round(player.time))}
              {total && ` / ${total}`}
            </bdi>
          ) : (
            <>
              {total && <bdi dir="ltr">{total}</bdi>}
              <span>{m.file === 'gone' ? t(language, 'mediaGone') : formatSize(language, s)}</span>
            </>
          )}
          {player.failed && <span>{t(language, 'audioUnplayable')}</span>}
        </span>
      </div>
    </div>
  );
}

/**
 * A video message: a circle that plays muted and on a loop while it's on
 * screen; a tap plays it from the start with sound (a ring shows how far),
 * another tap pauses. Fetched on opening, as in Telegram.
 */
function RoundView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const url = useMediaUrl(app, m);
  const { p, l } = m.media!;
  const tiny = tinyPreview(p);
  const video = useRef<HTMLVideoElement>(null);
  // Playing with sound (after a tap), and paused by a tap while so.
  const loud = useSignal(false);
  const held = useSignal(false);
  const time = useSignal(0);
  const length = useSignal<number | null>(null);
  const failed = useSignal(false);
  const quiet = () => {
    const v = video.current;
    loud.value = false;
    held.value = false;
    if (!v) return;
    if (nowPlaying === v) nowPlaying = null;
    v.muted = true;
    v.loop = true;
    void v.play().catch(() => {});
  };
  useEffect(() => {
    const v = video.current;
    if (!v || !url.value) return;
    // Muted, it plays only while on screen.
    const seen = new IntersectionObserver(([entry]) => {
      if (loud.peek()) return;
      if (entry?.isIntersecting) void v.play().catch(() => {});
      else v.pause();
    });
    seen.observe(v);
    return () => {
      seen.disconnect();
      if (nowPlaying === v) nowPlaying = null;
    };
  }, [url.value, failed.value]);
  const tap = () => {
    const v = video.current;
    if (!v) return;
    if (loud.value && !v.paused) {
      held.value = true;
      v.pause();
      return;
    }
    if (!loud.value) {
      loud.value = true;
      v.loop = false;
      v.currentTime = 0;
    }
    held.value = false;
    // Unmuted straight from the tap: phones only allow sound then.
    v.muted = false;
    if (nowPlaying && nowPlaying !== v) nowPlaying.pause();
    nowPlaying = v;
    void v.play().catch(quiet);
  };
  const total = length.value ?? l ?? 0;
  const fraction = loud.value && total > 0 ? Math.min(1, time.value / total) : 0;
  const ready = url.value && !failed.value;
  return (
    <div class={loud.value ? 'round loud' : 'round'}>
      {tiny && <img class="tiny" src={tiny} alt="" />}
      {ready && (
        <video
          ref={video}
          src={url.value!}
          muted={!loud.value}
          loop={!loud.value}
          playsInline
          preload="auto"
          onLoadedMetadata={(e) => Number.isFinite(e.currentTarget.duration) && (length.value = e.currentTarget.duration)}
          onTimeUpdate={(e) => (time.value = e.currentTarget.currentTime)}
          // Paused by another voice or video message starting: back to looping quietly.
          onPause={() => loud.peek() && !held.peek() && quiet()}
          onEnded={() => {
            time.value = 0;
            quiet();
          }}
          onError={() => (failed.value = true)}
        />
      )}
      {loud.value && (
        <svg class="round-ring" viewBox="0 0 100 100" aria-hidden="true">
          <circle cx="50" cy="50" r="48.5" pathLength={1} style={{ strokeDashoffset: `${1 - fraction}` }} />
        </svg>
      )}
      {ready && <button type="button" class="round-tap" aria-label={t(language, loud.value && !held.value ? 'pause' : 'play')} onClick={tap} />}
      {total > 0 && (
        <span class="round-badge">
          <bdi dir="ltr">{formatLength(language, Math.round(loud.value ? time.value : total))}</bdi>
          {!loud.value && (
            <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
              <path d="M4 9h4l5-4v14l-5-4H4zM17 9l4 6M21 9l-4 6" />
            </svg>
          )}
        </span>
      )}
      {failed.value && <span class="media-gone">{t(language, 'videoUnplayable')}</span>}
      <MediaState app={app} message={m} language={language} progress={progress} />
    </div>
  );
}

/** Seconds as "0:42" or "1:02:03". */
function formatLength(language: Language, seconds: number): string {
  const n = new Intl.NumberFormat(locale(language), { useGrouping: false });
  const two = new Intl.NumberFormat(locale(language), { useGrouping: false, minimumIntegerDigits: 2 });
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0 ? `${n.format(h)}:${two.format(m)}:${two.format(s)}` : `${n.format(m)}:${two.format(s)}`;
}

/**
 * A full-screen photo, GIF or video, with Save. It's put on the page itself,
 * not inside its message: there, a drag on the video's seek bar would also
 * swipe the message (moving the viewer with it) and a long press open its menu.
 */
function Viewer({ language, onClose, onSave, children }: { language: Language; onClose: () => void; onSave: () => void; children: ComponentChildren }) {
  const close = useRef<HTMLButtonElement>(null);
  useBackCloses(true, onClose);
  useEffect(() => close.current?.focus(), []);
  return createPortal(
    <div class="viewer" role="dialog" aria-modal="true" onClick={onClose} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      {children}
      <div class="viewer-bar" onClick={(e) => e.stopPropagation()}>
        <button type="button" class="viewer-button" onClick={onSave}>
          {t(language, 'save')}
        </button>
        <button type="button" class="viewer-button close" ref={close} aria-label={t(language, 'closeApp')} onClick={onClose}>
          ×
        </button>
      </div>
    </div>,
    document.body,
  );
}

/** A short message at the bottom of the screen. */
const toast = signal<string | null>(null);
function say(text: string): void {
  toast.value = text;
  setTimeout(() => toast.value === text && (toast.value = null), 3000);
}

/** Saves an attachment, saying so when it can't be saved here. */
async function save(app: AppState, m: MessageView, language: Language): Promise<void> {
  if ((await app.saveMedia(m)) === 'unavailable') say(t(language, 'saveUnavailable'));
}

/** A file: icon, name and size; tap to download it, then to save or share it. */
function FileView({ app, message: m, language, progress }: { app: AppState; message: MessageView; language: Language; progress: number | undefined }) {
  const media = m.media!;
  const busy = progress !== undefined;
  const onClick = () => {
    if (busy) return;
    if (fetchable(m)) void app.fetchMedia(m.id);
    else if (m.file === 'local') void save(app, m, language);
  };
  return (
    <button type="button" class="file" onClick={onClick} disabled={m.file === 'gone'}>
      <span class="file-icon">{busy ? <Ring fraction={progress} /> : fetchable(m) ? '↓' : extension(media.n) || '📄'}</span>
      <span class="file-info">
        <span class="file-name" dir="auto">
          {media.n ?? t(language, 'file')}
        </span>
        <span class="file-size">{m.file === 'gone' ? t(language, 'mediaGone') : formatSize(language, media.s)}</span>
      </span>
    </button>
  );
}

/** A quoted message: whose, and the start of it. */
function Quote({ name, text, onClick }: { name: string; text: string; onClick?: () => void }) {
  const body = (
    <>
      <bdi class="quote-name">{name}</bdi>
      <span class="quote-text" dir="auto">
        {text}
      </span>
    </>
  );
  return onClick ? (
    <button type="button" class="quote" onClick={onClick}>
      {body}
    </button>
  ) : (
    <span class="quote">{body}</span>
  );
}

/** How far a message is pulled before letting go answers it, in px. */
const REPLY_SWIPE = 56;
const STRONG = /[A-Za-z\u00C0-\u02AF\u0370-\u052F\u0590-\u08FF\uFB1D-\uFDFF\uFE70-\uFEFF]/;
const RTL = /[\u0590-\u08FF\uFB1D-\uFDFF\uFE70-\uFEFF]/;

/**
 * Whether a text's last paragraph runs in the page's direction (each one
 * takes its own, from its first strong letter), so its end is on the side
 * where the time goes. With no letters it may follow either: only on a
 * left-to-right page, where both are the same.
 */
function endsInPageDirection(text: string): boolean {
  const pageRtl = document.documentElement.dir === 'rtl';
  const first = text.split('\n').at(-1)?.match(STRONG)?.[0];
  return first ? RTL.test(first) === pageRtl : !pageRtl;
}

/** Two taps closer together than this, in ms, are a double-tap: ❤️, as in Telegram. */
const DOUBLE_TAP_MS = 300;
const DOUBLE_TAP_REACTION = '❤️';
/** Parts of a message that handle taps themselves (players, photos, files, chips): a double-tap there isn't a reaction. */
const OWN_TAPS = 'button, a, input, video, audio, .photo';

/**
 * A message. Swiping it toward the start answers it; a long press (or a
 * right click) opens its menu. A quoted message shows above the text, and
 * tapping it jumps there.
 */
function Bubble({
  app,
  message: m,
  quoted,
  peer,
  language,
  time,
  highlighted,
  canReply,
  canReact,
  onReact,
  onDoubleTap,
  onReply,
  onMenu,
  onQuote,
}: {
  app: AppState;
  message: MessageView;
  /** The message it answers; null if that one is gone from this device. */
  quoted: MessageView | null | undefined;
  peer: string;
  language: Language;
  time: Intl.DateTimeFormat;
  highlighted: boolean;
  canReply: boolean;
  canReact: boolean;
  onReact: (emoji: string) => void;
  /** A double-tap on the message itself: react with ❤️, or take it back. */
  onDoubleTap: () => void;
  onReply: () => void;
  onMenu: () => void;
  onQuote: () => void;
}) {
  const tr = (key: Key) => t(language, key);
  const gesture = useRef<{ x: number; y: number; pointer: number; mode: 'pending' | 'swipe' | 'scroll'; timer: number; pressed: boolean } | null>(null);
  const pulled = useSignal(0);
  const lastTap = useRef<{ at: number; x: number; y: number } | null>(null);
  const item = useRef<HTMLLIElement>(null);
  // A reaction added or taken back changes the message's height. As in
  // Telegram, it grows (or shrinks) upward: its bottom, and everything after
  // it, stays where it is, and what's before it moves. Where its bottom was
  // is read while rendering, from the element still on screen; before the
  // next paint the page scrolls it back there. That also covers the browser
  // having moved it already: a page that got shorter at its end is scrolled
  // back by the browser itself.
  const reactionKey = `${m.reactions?.mine?.e ?? ''}|${m.reactions?.theirs?.e ?? ''}`;
  const shownKey = useRef(reactionKey);
  const bottomBefore = useRef<number | null>(null);
  if (shownKey.current !== reactionKey) {
    shownKey.current = reactionKey;
    bottomBefore.current = item.current?.getBoundingClientRect().bottom ?? null;
  }
  useLayoutEffect(() => {
    const before = bottomBefore.current;
    bottomBefore.current = null;
    const rect = item.current?.getBoundingClientRect();
    // A message below the screen moves nothing that's shown: leave it.
    if (before === null || !rect || rect.bottom === before || before >= window.innerHeight) return;
    window.scrollBy({ top: rect.bottom - before, behavior: 'instant' });
  }, [reactionKey]);
  const direction = () => (document.documentElement.dir === 'rtl' ? -1 : 1);
  const longPress = () => {
    const g = gesture.current;
    if (g && (g.pressed || g.mode !== 'pending')) return;
    if (g) {
      g.pressed = true;
      clearTimeout(g.timer);
    }
    onMenu();
  };
  const onPointerDown = (e: PointerEvent) => {
    if (e.button !== 0) return;
    gesture.current = { x: e.clientX, y: e.clientY, pointer: e.pointerId, mode: 'pending', timer: window.setTimeout(longPress, LONG_PRESS_MS), pressed: false };
  };
  const onPointerMove = (e: PointerEvent) => {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointer || g.pressed) return;
    const dx = e.clientX - g.x;
    const dy = e.clientY - g.y;
    if (g.mode === 'pending') {
      if (Math.abs(dx) < SLOP && Math.abs(dy) < SLOP) return;
      clearTimeout(g.timer);
      g.mode = canReply && Math.abs(dx) > Math.abs(dy) && -dx * direction() > 0 ? 'swipe' : 'scroll';
      if (g.mode === 'swipe') (e.currentTarget as Element).setPointerCapture(e.pointerId);
    }
    if (g.mode === 'swipe') pulled.value = Math.min(REPLY_SWIPE * 1.5, Math.max(0, -dx * direction()));
  };
  const onPointerEnd = (e: PointerEvent) => {
    const g = gesture.current;
    if (!g || e.pointerId !== g.pointer) return;
    clearTimeout(g.timer);
    gesture.current = null;
    if (g.mode === 'swipe' && pulled.value >= REPLY_SWIPE) onReply();
    pulled.value = 0;
    // A tap that didn't move or turn into a long press; a second one soon after, close by, is a double-tap.
    const tap = e.type === 'pointerup' && g.mode === 'pending' && !g.pressed && !(e.target as Element).closest(OWN_TAPS);
    if (!tap || !canReact) {
      lastTap.current = null;
      return;
    }
    const prev = lastTap.current;
    if (prev && e.timeStamp - prev.at < DOUBLE_TAP_MS && Math.abs(e.clientX - prev.x) < SLOP * 2 && Math.abs(e.clientY - prev.y) < SLOP * 2) {
      lastTap.current = null;
      onDoubleTap();
    } else {
      lastTap.current = { at: e.timeStamp, x: e.clientX, y: e.clientY };
    }
  };
  const quoteName = quoted ? (quoted.out ? tr('you') : peer) : '';
  const meta = (
    <>
      <span>{m.status === 'failed' ? tr('notSent') : time.format(new Date(m.at))}</span>
      {m.out && <Ticks status={m.status} language={language} />}
    </>
  );
  // As in Telegram: the time sits on the text's last line where there's room
  // (an invisible copy at the end of the text keeps that room, or wraps to a
  // line of its own); with reactions, it goes on their row instead.
  const inlineTime = !!m.text && !m.reactions?.mine?.e && !m.reactions?.theirs?.e && endsInPageDirection(m.text);
  // A sticker or video message stands on its own, without a bubble, as in Telegram.
  const bare = !!m.sticker || (!!m.media && shownAs(m.media) === 'round' && !m.text);
  // The whole row is the message, as in Telegram: a long press, a double-tap
  // or a swipe in the empty space beside the bubble acts on it too.
  return (
    <li
      ref={item}
      id={`m-${m.id}`}
      class="message-row"
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={onPointerEnd}
      onContextMenu={(e) => {
        e.preventDefault();
        longPress();
      }}
    >
      <div
        class={`bubble ${m.out ? 'out' : 'in'}${bare ? ' bare' : ''}${highlighted ? ' flash' : ''}${pulled.value ? ' pulling' : ''}`}
        style={pulled.value ? { transform: `translateX(${-pulled.value * direction()}px)` } : undefined}
      >
        {pulled.value > 0 && (
          <span class={pulled.value >= REPLY_SWIPE ? 'reply-hint ready' : 'reply-hint'} aria-hidden="true">
            ↩
          </span>
        )}
        {quoted !== undefined &&
          (quoted ? (
            <Quote name={quoteName} text={contentLine(language, quoted)} onClick={onQuote} />
          ) : (
            <Quote name={tr('deletedMessage')} text="" />
          ))}
        {m.media && <Attachment app={app} message={m} language={language} progress={app.transfers.value[m.id]} />}
        {m.sticker && <StickerView sticker={m.sticker} language={language} />}
        {m.text && (
          <span class="text" dir="auto">
            {m.text}
            {inlineTime && (
              <span class="meta-spacer" aria-hidden="true">
                {meta}
              </span>
            )}
          </span>
        )}
        {inlineTime ? (
          <span class="meta floating">{meta}</span>
        ) : (
          <span class="footer">
            <Reactions message={m} language={language} canReact={canReact} onReact={onReact} />
            <span class="meta">{meta}</span>
          </span>
        )}
      </div>
    </li>
  );
}

/** The reactions offered first in a message's menu, as in Telegram; the rest are in the emoji picker. */
const QUICK_REACTIONS = ['👍', '❤️', '😂', '😮', '😢', '🙏', '🔥'];

/** Whether one can react to a message: in an open conversation, and not to one of ours still on its way. */
function canReact(screen: Extract<Screen, { kind: 'conversation' }>, m: MessageView): boolean {
  return !screen.blocked && !screen.ended && !(m.out && (m.status === 'sending' || m.status === 'failed'));
}

/** A message's reactions under it: one chip per emoji, ours highlighted; a tap adds ours, or takes it back. */
function Reactions({ message: m, language, canReact, onReact }: { message: MessageView; language: Language; canReact: boolean; onReact: (emoji: string) => void }) {
  const mine = m.reactions?.mine?.e ?? '';
  const theirs = m.reactions?.theirs?.e ?? '';
  const chips = [...new Set([mine, theirs].filter((e) => e !== ''))];
  if (chips.length === 0) return null;
  return (
    <span class="reactions">
      {chips.map((e) => {
        const count = Number(e === mine) + Number(e === theirs);
        return (
          <button
            key={e}
            type="button"
            class={e === mine ? 'reaction-chip mine' : 'reaction-chip'}
            aria-pressed={e === mine}
            aria-label={e === mine ? `${e} ${t(language, 'yourReaction')}` : e}
            disabled={!canReact}
            onPointerDown={(ev) => ev.stopPropagation()}
            onClick={() => onReact(e)}
          >
            <span class="reaction-emoji">{e}</span>
            {count > 1 && <span>{formatCount(language, count)}</span>}
          </button>
        );
      })}
    </span>
  );
}

const formatCount = (language: Language, n: number) => new Intl.NumberFormat(locale(language)).format(n);

/** The long-press menu of a message: reactions, then Reply, Copy, Delete for me. */
function MessageMenu({
  message,
  language,
  canReply,
  canReact,
  peer,
  canDeleteForBoth,
  onClose,
  onReact,
  onReply,
  onDelete,
}: {
  message: MessageView;
  language: Language;
  canReply: boolean;
  canReact: boolean;
  /** The other side's label, for "Also delete for …". */
  peer: string;
  /** One of ours, sent, in an open conversation: it can be deleted on their side too. */
  canDeleteForBoth: boolean;
  onClose: () => void;
  onReact: (m: MessageView, emoji: string) => void;
  onReply: (m: MessageView) => void;
  onDelete: (m: MessageView, forBoth: boolean) => void;
}) {
  const tr = (key: Key) => t(language, key);
  const confirming = useSignal(false);
  const picking = useSignal(false);
  // As in Telegram, deleting one of ours deletes it for them too unless unticked.
  const forBoth = useSignal(true);
  const mine = message.reactions?.mine?.e ?? '';
  const first = useRef<HTMLButtonElement>(null);
  useEffect(() => first.current?.focus(), [confirming.value]);
  const act = (f: () => void) => () => {
    f();
    onClose();
  };
  return (
    <div class="overlay" onClick={onClose} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      <div class="dialog menu" role="dialog" aria-modal="true" aria-label={tr('messageActions')} onClick={(e) => e.stopPropagation()}>
        {confirming.value ? (
          <>
            <p>{tr(canDeleteForBoth ? 'deleteOwnMessageConfirm' : 'deleteMessageConfirm')}</p>
            {canDeleteForBoth && (
              <label class="check">
                <input type="checkbox" checked={forBoth.value} onChange={(e) => (forBoth.value = e.currentTarget.checked)} />
                {/* The label isolated, so it reads right inside Persian text too. */}
                <span>{t(language, 'alsoDeleteFor', { name: `\u2068${peer}\u2069` })}</span>
              </label>
            )}
            <div class="row">
              <button type="button" class="button danger" ref={first} onClick={act(() => onDelete(message, canDeleteForBoth && forBoth.value))}>
                {tr(canDeleteForBoth ? 'delete' : 'deleteForMe')}
              </button>
              <Button kind="secondary" onClick={onClose}>
                {tr('cancel')}
              </Button>
            </div>
          </>
        ) : picking.value ? (
          <div class="reaction-picker">
            <EmojiSection language={language} onEmoji={(e) => act(() => onReact(message, e))()} />
          </div>
        ) : (
          <>
            {canReact && (
              <div class="reaction-row" role="group" aria-label={tr('reactions')}>
                {QUICK_REACTIONS.map((e) => (
                  <button key={e} type="button" class="reaction-option" aria-pressed={mine === e} onClick={act(() => onReact(message, e))}>
                    {e}
                  </button>
                ))}
                <button type="button" class="reaction-option more" aria-label={tr('moreReactions')} onClick={() => (picking.value = true)}>
                  <svg class="icon-stroke" viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M6 9l6 6 6-6" />
                  </svg>
                </button>
              </div>
            )}
            {canReply && (
              <button type="button" class="menu-item" ref={first} onClick={act(() => onReply(message))}>
                {tr('reply')}
              </button>
            )}
            {message.text && (
              <button
                type="button"
                class="menu-item"
                {...(canReply ? {} : { ref: first })}
                onClick={act(() => void navigator.clipboard?.writeText(message.text).catch(() => {}))}
              >
                {tr('copy')}
              </button>
            )}
            <button type="button" class="menu-item danger" {...(!canReply && !message.text ? { ref: first } : {})} onClick={() => (confirming.value = true)}>
              {tr(canDeleteForBoth ? 'delete' : 'deleteForMe')}
            </button>
          </>
        )}
      </div>
    </div>
  );
}

function JoinForm({ app, language, busy }: { app: AppState; language: Language; busy: boolean }) {
  const code = useSignal('');
  return (
    <div class="card">
      <label class="field">
        <span>{t(language, 'haveInvitation')}</span>
        <input
          type="text"
          dir="ltr"
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          placeholder="abcd-efgh"
          value={code.value}
          disabled={busy}
          onInput={(e) => (code.value = e.currentTarget.value)}
        />
      </label>
      <Button disabled={busy || code.value.trim() === ''} onClick={() => app.redeem(code.value)}>
        {t(language, 'join')}
      </Button>
    </div>
  );
}

function Invitations({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'invitations' }> }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const { info, busy, notice, created } = screen;
  const uses = useSignal('1');
  const n = Number(uses.value);
  const valid = Number.isInteger(n) && n >= 1 && n <= info.maxUses;
  return (
    <Page title={tr('invitationsTitle')}>
      <p>{info.unlimited ? tr('invitationsUnlimited') : info.maxUses > 0 ? tr('invitationsRemaining', { count: info.maxUses }) : tr('invitationsNone')}</p>
      {notice && (
        <p class="notice" role="status">
          {tr(INVITATIONS_NOTICE_TEXT[notice])}
        </p>
      )}
      {info.maxUses > 0 && (
        <div class="card">
          <label class="field">
            <span>{tr('invitationUses')}</span>
            <input
              type="number"
              inputmode="numeric"
              min={1}
              max={info.maxUses}
              value={uses.value}
              disabled={busy}
              onInput={(e) => (uses.value = e.currentTarget.value)}
            />
          </label>
          <Button disabled={busy || !valid} onClick={() => app.createInvitation(n)}>
            {tr('createInvitation')}
          </Button>
        </div>
      )}
      {info.codes.length > 0 && (
        <ul class="links">
          {info.codes.map((c) => (
            <li key={c.code} class={c.code === created ? 'link highlight' : 'link'}>
              <div class="row spread">
                <bdi dir="ltr" class="code">
                  {c.code}
                </bdi>
                <span class="muted">{tr('invitationUsed', { used: c.uses - c.left, total: c.uses })}</span>
              </div>
              {c.left > 0 && (
                <div class="row">
                  <CopyButton language={language} text={c.url} disabled={busy} />
                  <Button kind="secondary" disabled={busy} onClick={() => app.share(c.url)}>
                    {tr('share')}
                  </Button>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}
      <Button kind="secondary" disabled={busy} onClick={app.home}>
        {tr('back')}
      </Button>
      {busy && <div class="spinner" aria-hidden="true" />}
    </Page>
  );
}

function Admin({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'admin' }> }) {
  const tr = (key: Key, values?: Record<string, string | number>) => t(language, key, values);
  const { levels, busy, notice } = screen;
  const level = useSignal('1');
  const allowance = useSignal('');
  const link = useSignal('');
  const extra = useSignal('1');
  const lv = Number(level.value);
  const al = Number(allowance.value);
  const ex = Number(extra.value);
  const levelOk = Number.isInteger(lv) && lv >= 1 && lv <= 100 && allowance.value !== '' && Number.isInteger(al) && al >= 0;
  const grantOk = link.value.trim() !== '' && Number.isInteger(ex) && ex >= 1;
  return (
    <Page title={tr('adminTitle')}>
      {notice && (
        <p class="notice" role="status">
          {tr(ADMIN_NOTICE_TEXT[notice])}
        </p>
      )}
      <h2>{tr('adminLevels')}</h2>
      <p class="muted">{tr('adminLevelsHelp')}</p>
      {levels.length > 0 && (
        <ul class="plain">
          {levels.map((l) => (
            <li key={l.level}>
              {tr('levelLine', { level: l.level, count: l.invitations })}
            </li>
          ))}
        </ul>
      )}
      <div class="card">
        <div class="row">
          <label class="field grow">
            <span>{tr('level')}</span>
            <input type="number" inputmode="numeric" min={1} max={100} value={level.value} disabled={busy} onInput={(e) => (level.value = e.currentTarget.value)} />
          </label>
          <label class="field grow">
            <span>{tr('invitationsLabel')}</span>
            <input type="number" inputmode="numeric" min={0} value={allowance.value} disabled={busy} onInput={(e) => (allowance.value = e.currentTarget.value)} />
          </label>
        </div>
        <Button disabled={busy || !levelOk} onClick={() => app.setLevel(lv, al)}>
          {tr('save')}
        </Button>
      </div>
      <h2>{tr('adminGrant')}</h2>
      <div class="card">
        <label class="field">
          <span>{tr('adminGrantHelp')}</span>
          <input type="text" dir="ltr" autocomplete="off" spellcheck={false} value={link.value} disabled={busy} onInput={(e) => (link.value = e.currentTarget.value)} />
        </label>
        <label class="field">
          <span>{tr('invitationsLabel')}</span>
          <input type="number" inputmode="numeric" min={1} value={extra.value} disabled={busy} onInput={(e) => (extra.value = e.currentTarget.value)} />
        </label>
        <Button disabled={busy || !grantOk} onClick={() => app.grant(link.value, ex)}>
          {tr('grant')}
        </Button>
      </div>
      <Button kind="secondary" disabled={busy} onClick={app.home}>
        {tr('back')}
      </Button>
      {busy && <div class="spinner" aria-hidden="true" />}
    </Page>
  );
}

function CopyButton({ language, text, disabled }: { language: Language; text: string; disabled: boolean }) {
  const copied = useSignal(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      copied.value = true;
      setTimeout(() => (copied.value = false), 2000);
    } catch {
      // Some webviews refuse clipboard access; the text is on screen to copy by hand.
    }
  };
  return (
    <Button kind="secondary" disabled={disabled} onClick={copy}>
      {copied.value ? t(language, 'copied') : t(language, 'copy')}
    </Button>
  );
}

function LinkItem({ app, language, link, busy }: { app: AppState; language: Language; link: LinkView; busy: boolean }) {
  const tr = (key: Key) => t(language, key);
  const copied = useSignal(false);
  const confirming = useSignal(false);
  const input = useRef<HTMLInputElement>(null);
  const copy = async (input: HTMLInputElement | null) => {
    try {
      await navigator.clipboard.writeText(link.url);
      copied.value = true;
      setTimeout(() => (copied.value = false), 2000);
    } catch {
      // Some webviews refuse clipboard access: select it for copying by hand.
      input?.select();
    }
  };
  return (
    <li class="link">
      <input class="link-url" type="text" dir="ltr" readonly value={link.url} ref={input} onFocus={(e) => e.currentTarget.select()} />
      {confirming.value ? (
        <div class="row">
          <p class="warning">{tr('revokeConfirm')}</p>
          <Button kind="danger" disabled={busy} onClick={() => app.revokeLink(link.id)}>
            {tr('revokeYes')}
          </Button>
          <Button kind="secondary" disabled={busy} onClick={() => (confirming.value = false)}>
            {tr('cancel')}
          </Button>
        </div>
      ) : (
        <div class="row">
          <Button kind="secondary" disabled={busy} onClick={() => copy(input.current)}>
            {copied.value ? tr('copied') : tr('copy')}
          </Button>
          <Button kind="secondary" disabled={busy} onClick={() => app.share(link.url)}>
            {tr('share')}
          </Button>
          <Button kind="secondary" disabled={busy} onClick={() => void confirmReplace(app, language)}>
            {tr('newLink')}
          </Button>
          <Button kind="secondary" disabled={busy} onClick={() => (confirming.value = true)}>
            {tr('revoke')}
          </Button>
        </div>
      )}
    </li>
  );
}

function ConfirmReplace({ app, language, busy }: { app: AppState; language: Language; busy: boolean }) {
  const typed = useSignal('');
  const phrase = t(language, 'replacePhrase');
  const ok = confirmationMatches(typed.value, phrase);
  return (
    <Page title={t(language, 'replaceTitle')} body={t(language, 'replaceBody')}>
      <p class="warning">{t(language, 'replaceWarning')}</p>
      <label class="field">
        <span>
          {t(language, 'replaceType', { phrase: '⁨' + phrase + '⁩' })}
        </span>
        <input
          type="text"
          dir="auto"
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          value={typed.value}
          disabled={busy}
          onInput={(e) => (typed.value = e.currentTarget.value)}
        />
      </label>
      <Button kind="danger" disabled={!ok || busy} onClick={app.confirmReplace}>
        {t(language, 'replaceConfirm')}
      </Button>
      <Button kind="secondary" disabled={busy} onClick={app.cancelReplace}>
        {t(language, 'cancel')}
      </Button>
    </Page>
  );
}

/** Deleting the account: what goes, and a typed phrase so it can't happen by accident. */
function ConfirmDelete({ app, language, busy, error }: { app: AppState; language: Language; busy: boolean; error: ErrorKind | null }) {
  const typed = useSignal('');
  const phrase = t(language, 'deletePhrase');
  const ok = confirmationMatches(typed.value, phrase);
  return (
    <Page title={t(language, 'deleteTitle')} body={t(language, 'deleteBody')} busy={busy}>
      <p class="warning">{t(language, 'deleteWarning')}</p>
      {error && (
        <p class="notice" role="alert">
          {t(language, ERROR_TEXT[error])}
        </p>
      )}
      <label class="field">
        <span>{t(language, 'replaceType', { phrase: '⁨' + phrase + '⁩' })}</span>
        <input
          type="text"
          dir="auto"
          autocomplete="off"
          autocapitalize="off"
          spellcheck={false}
          value={typed.value}
          disabled={busy}
          onInput={(e) => (typed.value = e.currentTarget.value)}
        />
      </label>
      <Button kind="danger" disabled={!ok || busy} onClick={app.deleteAccount}>
        {t(language, 'deleteAccountConfirm')}
      </Button>
      <Button kind="secondary" disabled={busy} onClick={app.cancelDeleteAccount}>
        {t(language, 'cancel')}
      </Button>
    </Page>
  );
}

function Page({ title, body, busy, children }: { title: string; body?: string; busy?: boolean; children?: ComponentChildren }) {
  return (
    <main class="page" aria-busy={busy ? 'true' : undefined}>
      <h1>{title}</h1>
      {body && <p>{body}</p>}
      {busy && <div class="spinner" aria-hidden="true" />}
      {children}
    </main>
  );
}

function Button({
  kind = 'primary',
  disabled,
  onClick,
  children,
}: {
  kind?: 'primary' | 'secondary' | 'danger';
  disabled?: boolean;
  onClick: () => unknown;
  children: ComponentChildren;
}) {
  return (
    <button type="button" class={`button ${kind}`} disabled={disabled} onClick={() => void onClick()}>
      {children}
    </button>
  );
}
