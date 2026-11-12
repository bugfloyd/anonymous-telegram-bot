import { describe, expect, it } from 'vitest';
import { connectTelegram, openPopup, TELEGRAM_WEB_ORIGIN } from './telegram';

type Listener = (e: MessageEvent) => void;

function fakeWindow(kind: 'proxy' | 'iframe' | 'none') {
  const posted: { type: string; data: string | undefined }[] = [];
  const parentPosts: { message: unknown; origin: string }[] = [];
  const listeners: Listener[] = [];
  const parent = { postMessage: (message: unknown, origin: string) => parentPosts.push({ message, origin }) };
  const win: Record<string, unknown> & { addEventListener(t: 'message', l: Listener): void } = {
    addEventListener: (_t, l) => listeners.push(l),
  };
  win.parent = kind === 'iframe' ? parent : win;
  if (kind === 'proxy') win.TelegramWebviewProxy = { postEvent: (type: string, data?: string) => posted.push({ type, data }) };
  const dispatch = (data: unknown, origin = TELEGRAM_WEB_ORIGIN, source: unknown = parent) =>
    listeners.forEach((l) => l({ data, origin, source } as MessageEvent));
  return { win, posted, parentPosts, dispatch };
}

describe('connectTelegram', () => {
  it('is null outside Telegram', () => {
    expect(connectTelegram(false, fakeWindow('proxy').win as never)).toBeNull();
    expect(connectTelegram(true, fakeWindow('none').win as never)).toBeNull();
  });

  it('posts through the webview proxy and receives events', () => {
    const { win, posted } = fakeWindow('proxy');
    const bridge = connectTelegram(true, win as never)!;
    expect(bridge.transport).toBe('webview-proxy');
    bridge.post('web_app_ready');
    bridge.post('web_app_open_tg_link', { path_full: '/bot/app?startapp=x' });
    expect(posted).toEqual([
      { type: 'web_app_ready', data: undefined },
      { type: 'web_app_open_tg_link', data: '{"path_full":"/bot/app?startapp=x"}' },
    ]);

    const seen: unknown[] = [];
    const off = bridge.on('write_access_requested', (d) => seen.push(d.status));
    const receive = (win.Telegram as { WebView: { receiveEvent(t: string, d: unknown): void } }).WebView.receiveEvent;
    receive('write_access_requested', { status: 'allowed' });
    off();
    receive('write_access_requested', { status: 'cancelled' });
    expect(seen).toEqual(['allowed']);
  });

  it('keeps delivering when a handler throws', () => {
    const { win } = fakeWindow('proxy');
    const bridge = connectTelegram(true, win as never)!;
    let calls = 0;
    bridge.on('back_button_pressed', () => {
      throw new Error('boom');
    });
    bridge.on('back_button_pressed', () => calls++);
    (win.TelegramGameProxy as { receiveEvent(t: string, d: unknown): void }).receiveEvent('back_button_pressed', undefined);
    expect(calls).toBe(1);
  });

  it('in Telegram Web, says ready to the parent and only trusts web.telegram.org', () => {
    const { win, parentPosts, dispatch } = fakeWindow('iframe');
    const bridge = connectTelegram(true, win as never)!;
    expect(bridge.transport).toBe('iframe');
    expect(parentPosts[0]).toEqual({
      message: '{"eventType":"iframe_ready","eventData":{"reload_supported":true}}',
      origin: TELEGRAM_WEB_ORIGIN,
    });

    const themes: Record<string, string>[] = [];
    bridge.on('theme_changed', (d) => themes.push(d.theme_params));
    dispatch(JSON.stringify({ eventType: 'theme_changed', eventData: { theme_params: { bg_color: '#000000' } } }));
    dispatch(JSON.stringify({ eventType: 'theme_changed', eventData: { theme_params: { bg_color: '#bad000' } } }), 'https://evil.example');
    dispatch(JSON.stringify({ eventType: 'theme_changed', eventData: { theme_params: { bg_color: '#bad111' } } }), TELEGRAM_WEB_ORIGIN, {});
    dispatch('not json');
    expect(themes).toEqual([{ bg_color: '#000000' }]);
  });
});

describe('openPopup', () => {
  it('resolves with the pressed button, one popup at a time', async () => {
    const { win, posted } = fakeWindow('proxy');
    const bridge = connectTelegram(true, win as never)!;
    const receive = (win.Telegram as { WebView: { receiveEvent(t: string, d: unknown): void } }).WebView.receiveEvent;
    const buttons = [
      { id: 'delete', type: 'destructive' as const, text: 'Delete' },
      { id: 'cancel', type: 'cancel' as const },
    ];
    const first = openPopup(bridge, { message: 'Sure?', buttons });
    // A second one while it's open is refused, not stacked.
    expect(await openPopup(bridge, { message: 'Again?', buttons })).toBeNull();
    expect(posted.map((p) => p.type)).toEqual(['web_app_open_popup']);
    expect(JSON.parse(posted[0]!.data!)).toEqual({ message: 'Sure?', buttons });
    receive('popup_closed', { button_id: 'delete' });
    expect(await first).toBe('delete');
    // Dismissed without a button.
    const second = openPopup(bridge, { message: 'Sure?', buttons });
    receive('popup_closed', {});
    expect(await second).toBeNull();
  });
});
