import { useSignal } from '@preact/signals';
import type { ComponentChildren } from 'preact';
import { useEffect, useRef } from 'preact/hooks';
import { locale, relativeTime, t, type Language } from './i18n';
import type { Key } from './i18n/en';
import type { MessageView } from './messaging/store';
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
  created: 'noticeCreated',
  revoked: 'noticeRevoked',
  limit: 'noticeLimit',
  invitation: 'needInvitation',
  joined: 'noticeJoined',
  'invalid-invitation': 'noticeInvalidInvitation',
  'already-member': 'noticeAlreadyMember',
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
    case 'confirm-replace':
      return <ConfirmReplace app={app} language={language} busy={screen.busy} />;
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
  const max = account?.maxLinks ?? 0;
  const canCreate = !offline && !!account?.canCreateLinks && links.length < max;
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
      {links.length === 0 ? (
        <p class="muted">{tr('noLinks')}</p>
      ) : (
        <ul class="links">
          {links.map((link) => (
            <LinkItem key={link.id} app={app} language={language} link={link} busy={busy} />
          ))}
        </ul>
      )}
      {account && max > 0 && <p class="muted">{tr('linksCount', { count: links.length, max })}</p>}
      {canCreate && (
        <Button disabled={busy} onClick={app.createLink}>
          {tr('newLink')}
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

function Conversation({ app, language, screen }: { app: AppState; language: Language; screen: Extract<Screen, { kind: 'conversation' }> }) {
  const tr = (key: Key) => t(language, key);
  const end = useRef<HTMLDivElement>(null);
  const confirmingBlock = useSignal(false);
  useEffect(() => end.current?.scrollIntoView({ block: 'end' }), [screen.messages.length]);
  const time = new Intl.DateTimeFormat(locale(language), { hour: 'numeric', minute: '2-digit' });
  return (
    <main class="page conversation">
      <header class="conversation-head">
        <bdi dir="ltr" class="label">
          {screen.label}
        </bdi>
        <span class="muted">{tr(screen.role === 'owner' ? 'aboutOwnerConversation' : 'aboutSenderConversation')}</span>
        {confirmingBlock.value ? (
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
          <li key={m.id} class={m.out ? 'bubble out' : 'bubble in'}>
            <span class="text" dir="auto">
              {m.text}
            </span>
            <span class="meta">
              <span>{m.status === 'failed' ? tr('notSent') : time.format(new Date(m.at))}</span>
              {m.out && <Ticks status={m.status} language={language} />}
            </span>
          </li>
        ))}
      </ol>
      <div ref={end} />
      {screen.ended ? (
        <p class="notice">{tr('conversationEnded')}</p>
      ) : (
        <>
          {screen.error && (
            <p class="notice" role="alert">
              {tr(SEND_ERROR_TEXT[screen.error])}
            </p>
          )}
          <Composer language={language} busy={screen.busy} onSend={app.sendMessage} />
        </>
      )}
    </main>
  );
}

/** Asks, in Telegram's own popup, whether to delete a conversation from this device, or delete and block it. */
async function confirmDelete(app: AppState, language: Language, id: string): Promise<void> {
  const choice = await app.popup({
    message: t(language, 'deleteConfirm'),
    buttons: [
      { id: 'delete', type: 'destructive', text: t(language, 'delete') },
      { id: 'block', type: 'destructive', text: t(language, 'deleteAndBlock') },
      { id: 'cancel', type: 'cancel' },
    ],
  });
  if (choice === 'delete' || choice === 'block') await app.deleteConversation(id, choice === 'block');
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
            {c.last ? (c.last.out ? tr('youWrote', { text: c.last.text }) : c.last.text) : ''}
          </span>
          {c.unread > 0 && <span class="badge">{t(language, 'unreadCount', { count: c.unread })}</span>}
        </span>
      </button>
    </li>
  );
}

function Composer({ language, busy, onSend }: { language: Language; busy: boolean; onSend: (text: string) => Promise<void> }) {
  const text = useSignal('');
  const send = async () => {
    const value = text.value;
    if (value.trim() === '') return;
    text.value = '';
    await onSend(value);
  };
  return (
    <div class="composer">
      <textarea
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
