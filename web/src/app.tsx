import { signal, useSignal } from '@preact/signals';
import type { ComponentChildren } from 'preact';
import { useEffect, useLayoutEffect, useRef } from 'preact/hooks';
import { locale, relativeTime, t, type Language } from './i18n';
import type { Key } from './i18n/en';
import type { MessageView } from './messaging/store';
import { showableAs } from './media/prepare';
import { MAX_FILE_BYTES, shownAs } from './protocol/message';
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

export function App({ app, language }: { app: AppState; language: Language }) {
  return (
    <>
      <Screen app={app} language={language} />
      <DeleteDialog app={app} language={language} />
      {toast.value && (
        <p class="toast" role="status">
          {toast.value}
        </p>
      )}
    </>
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
    case 'blocked':
      return <Blocked app={app} language={language} screen={screen} />;
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
  const scroll = useChatScroll(app, screen.id, screen.messages);
  const sending = app.preparing.value.filter((p) => p.conv === screen.id).length;
  useEffect(() => {
    if (sending > 0) window.scrollTo({ top: document.documentElement.scrollHeight });
  }, [sending]);
  // The message being answered, the one whose menu is open, and the one just jumped to.
  const replying = useSignal<MessageView | null>(null);
  const menu = useSignal<MessageView | null>(null);
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
            <button type="button" class="link-button" disabled={screen.busy} onClick={() => void confirmDelete(app, language, screen.id)}>
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
            onReply={() => (replying.value = m)}
            onMenu={() => (menu.value = m)}
            onQuote={() => m.replyTo && jumpTo(m.replyTo)}
          />
        ))}
      {app.preparing.value
        .filter((p) => p.conv === screen.id)
        .map((p) => (
          <li key={p.key} class="bubble out" aria-busy="true">
            {p.as !== 'file' && p.url ? (
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
          onClose={() => (menu.value = null)}
          onReply={(m) => (replying.value = m)}
          onDelete={(m) => {
            if (replying.value?.id === m.id) replying.value = null;
            void app.deleteMessage(m.id);
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
                  ? { name: replying.value.out ? tr('you') : screen.label, text: mediaLine(language, replying.value.media, replying.value.text) }
                  : null
              }
              onCancelReply={() => (replying.value = null)}
              onSend={async (text) => {
                const to = replying.value?.id;
                replying.value = null;
                await app.sendMessage(text, to);
              }}
              {...(app.media
                ? {
                    onAttach: async (file: File, as: SendAs, caption: string) => {
                      const to = replying.value?.id;
                      replying.value = null;
                      await app.sendMedia(file, as, caption, to);
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
/** The conversation the delete dialog is open for. */
const deleting = signal<string | null>(null);

/** Asks before deleting a conversation, with blocking as an extra, unchecked choice. */
function confirmDelete(_app: AppState, _language: Language, id: string): void {
  deleting.value = id;
}

function DeleteDialog({ app, language }: { app: AppState; language: Language }) {
  const alsoBlock = useSignal(false);
  const first = useRef<HTMLButtonElement>(null);
  const id = deleting.value;
  useEffect(() => {
    if (id) first.current?.focus();
  }, [id]);
  if (!id) return null;
  const close = () => {
    deleting.value = null;
    alsoBlock.value = false;
  };
  const confirm = () => {
    const block = alsoBlock.value;
    close();
    void app.deleteConversation(id, block);
  };
  return (
    <div class="overlay" onClick={close} onKeyDown={(e) => e.key === 'Escape' && close()}>
      <div class="dialog" role="dialog" aria-modal="true" aria-labelledby="delete-text" onClick={(e) => e.stopPropagation()}>
        <p id="delete-text">{t(language, 'deleteConfirm')}</p>
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
                  {c.last ? c.last.text : ''}
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
    void confirmDelete(app, language, c.id);
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
          void confirmDelete(app, language, c.id);
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
            {c.last ? (c.last.out ? tr('youWrote', { text: mediaLine(language, c.last.media, c.last.text) }) : mediaLine(language, c.last.media, c.last.text)) : ''}
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
}: {
  language: Language;
  busy: boolean;
  onSend: (text: string) => Promise<void>;
  /** The message being answered: who wrote it, and its text. */
  reply?: { name: string; text: string } | null;
  onCancelReply?: () => void;
  /** Sends a picked file (absent when attachments are off). */
  onAttach?: (file: File, as: SendAs, caption: string) => Promise<void>;
}) {
  const text = useSignal('');
  const attachMenu = useSignal(false);
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
  const send = async () => {
    const value = text.value;
    if (value.trim() === '') return;
    text.value = '';
    await onSend(value);
  };
  return (
    <div class="composer-wrap">
      {reply && (
        <div class="reply-bar">
          <Quote name={t(language, 'replyingTo', { name: reply.name })} text={reply.text} />
          <button type="button" class="reply-cancel" aria-label={t(language, 'cancelReply')} onClick={onCancelReply}>
            ×
          </button>
        </div>
      )}
      <div class="composer">
      {onAttach && (
        <>
          <button type="button" class="attach" aria-label={t(language, 'attach')} disabled={busy} onClick={() => (attachMenu.value = true)}>
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="M16.5 6.5l-8 8a2.5 2.5 0 003.5 3.5l8.5-8.5a4.5 4.5 0 00-6.4-6.4L5.5 11.7a6.5 6.5 0 009.2 9.2l6.8-6.8" />
            </svg>
          </button>
          <input type="file" accept="image/*,video/*" hidden ref={photoInput} onChange={pick(true)} />
          <input type="file" hidden ref={fileInput} onChange={pick(false)} />
        </>
      )}
      <textarea
        ref={input}
        dir="auto"
        rows={2}
        maxLength={4000}
        placeholder={t(language, 'messagePlaceholder')}
        value={text.value}
        disabled={busy}
        onInput={(e) => (text.value = e.currentTarget.value)}
      />
      <Button disabled={busy || text.value.trim() === ''} onClick={send}>
        {t(language, 'send')}
      </Button>
      </div>
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

/** A picked file, and whether it starts out shown as a photo, video or GIF (rather than as a file). */
interface Picked {
  file: File;
  shown: boolean;
}

/** The choice for each kind a file can be shown as: compress a photo, or send a video or GIF as one. */
const SHOW_AS: Record<'photo' | 'video' | 'gif', Key> = { photo: 'compressImage', video: 'sendAsVideo', gif: 'sendAsGif' };

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
  const tr = (key: Key) => t(language, key);
  const kind = showableAs(picked.file);
  const shown = useSignal(picked.shown && kind !== null);
  const caption = useSignal('');
  const preview = useSignal<string | null>(null);
  // A video this browser can't play shows as a file here (and is sent as one).
  const unplayable = useSignal(false);
  useEffect(() => {
    if (!kind) return;
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
        ) : preview.value && kind !== 'video' ? (
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

/** "📷 Photo", "📹 Video", "GIF" or "📎 name" before a caption: for chat previews, quotes and the reply bar. */
function mediaLine(language: Language, media: { t: string; n?: string } | undefined, text: string): string {
  if (!media) return text;
  const label = {
    photo: `📷 ${t(language, 'photo')}`,
    video: `📹 ${t(language, 'video')}`,
    gif: t(language, 'gif'),
    file: `📎 ${media.n ?? t(language, 'file')}`,
  }[shownAs(media)];
  return text ? `${label}, ${text}` : label;
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
      {m.file === 'remote' && progress === undefined && !m.out && (
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
  const size = m.file === 'remote' ? formatSize(language, s) : null;
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

/** Seconds as "0:42" or "1:02:03". */
function formatLength(language: Language, seconds: number): string {
  const n = new Intl.NumberFormat(locale(language), { useGrouping: false });
  const two = new Intl.NumberFormat(locale(language), { useGrouping: false, minimumIntegerDigits: 2 });
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  return h > 0 ? `${n.format(h)}:${two.format(m)}:${two.format(s)}` : `${n.format(m)}:${two.format(s)}`;
}

/** A full-screen photo, GIF or video, with Save. */
function Viewer({ language, onClose, onSave, children }: { language: Language; onClose: () => void; onSave: () => void; children: ComponentChildren }) {
  const close = useRef<HTMLButtonElement>(null);
  useEffect(() => close.current?.focus(), []);
  return (
    <div class="viewer" role="dialog" aria-modal="true" onClick={onClose} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
      {children}
      <div class="viewer-bar" onClick={(e) => e.stopPropagation()}>
        <button type="button" class="viewer-button" onClick={onSave}>
          {t(language, 'save')}
        </button>
        <button type="button" class="viewer-button" ref={close} aria-label={t(language, 'closeApp')} onClick={onClose}>
          ×
        </button>
      </div>
    </div>
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
    if (m.file === 'remote' && !m.out) void app.fetchMedia(m.id);
    else if (m.file === 'local') void save(app, m, language);
  };
  return (
    <button type="button" class="file" onClick={onClick} disabled={m.file === 'gone'}>
      <span class="file-icon">{busy ? <Ring fraction={progress} /> : m.file === 'remote' && !m.out ? '↓' : extension(media.n) || '📄'}</span>
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
  onReply: () => void;
  onMenu: () => void;
  onQuote: () => void;
}) {
  const tr = (key: Key) => t(language, key);
  const gesture = useRef<{ x: number; y: number; pointer: number; mode: 'pending' | 'swipe' | 'scroll'; timer: number; pressed: boolean } | null>(null);
  const pulled = useSignal(0);
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
  };
  const quoteName = quoted ? (quoted.out ? tr('you') : peer) : '';
  return (
    <li
      id={`m-${m.id}`}
      class={`bubble ${m.out ? 'out' : 'in'}${highlighted ? ' flash' : ''}${pulled.value ? ' pulling' : ''}`}
      style={pulled.value ? { transform: `translateX(${-pulled.value * direction()}px)` } : undefined}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerEnd}
      onPointerCancel={onPointerEnd}
      onContextMenu={(e) => {
        e.preventDefault();
        longPress();
      }}
    >
      {pulled.value > 0 && (
        <span class={pulled.value >= REPLY_SWIPE ? 'reply-hint ready' : 'reply-hint'} aria-hidden="true">
          ↩
        </span>
      )}
      {quoted !== undefined &&
        (quoted ? (
          <Quote name={quoteName} text={mediaLine(language, quoted.media, quoted.text)} onClick={onQuote} />
        ) : (
          <Quote name={tr('deletedMessage')} text="" />
        ))}
      {m.media && <Attachment app={app} message={m} language={language} progress={app.transfers.value[m.id]} />}
      {m.text && (
        <span class="text" dir="auto">
          {m.text}
        </span>
      )}
      <span class="meta">
        <span>{m.status === 'failed' ? tr('notSent') : time.format(new Date(m.at))}</span>
        {m.out && <Ticks status={m.status} language={language} />}
      </span>
    </li>
  );
}

/** The long-press menu of a message: Reply, Copy, Delete for me. */
function MessageMenu({
  message,
  language,
  canReply,
  onClose,
  onReply,
  onDelete,
}: {
  message: MessageView;
  language: Language;
  canReply: boolean;
  onClose: () => void;
  onReply: (m: MessageView) => void;
  onDelete: (m: MessageView) => void;
}) {
  const tr = (key: Key) => t(language, key);
  const confirming = useSignal(false);
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
            <p>{tr('deleteMessageConfirm')}</p>
            <div class="row">
              <button type="button" class="button danger" ref={first} onClick={act(() => onDelete(message))}>
                {tr('deleteForMe')}
              </button>
              <Button kind="secondary" onClick={onClose}>
                {tr('cancel')}
              </Button>
            </div>
          </>
        ) : (
          <>
            {canReply && (
              <button type="button" class="menu-item" ref={first} onClick={act(() => onReply(message))}>
                {tr('reply')}
              </button>
            )}
            <button
              type="button"
              class="menu-item"
              {...(canReply ? {} : { ref: first })}
              onClick={act(() => void navigator.clipboard?.writeText(message.text).catch(() => {}))}
            >
              {tr('copy')}
            </button>
            <button type="button" class="menu-item danger" onClick={() => (confirming.value = true)}>
              {tr('deleteForMe')}
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
