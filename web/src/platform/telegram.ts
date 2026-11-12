// A minimal bridge to the Telegram client, speaking the documented web-events
// protocol (https://core.telegram.org/api/web-events). It replaces Telegram's
// telegram-web-app.js, which changes in place and so can't be pinned or
// verified, logs every event, and posts messages to any origin.
//
// Outgoing events go through TelegramWebviewProxy (mobile and desktop), the
// legacy window.external.notify, or postMessage to the Telegram Web parent.
// Incoming events arrive through window.Telegram.WebView.receiveEvent, or as
// messages from the parent, which are only accepted from web.telegram.org.

export const TELEGRAM_WEB_ORIGIN = 'https://web.telegram.org';

/** Events the client sends us that the app uses. */
export interface IncomingEvents {
  theme_changed: { theme_params: Record<string, string> };
  viewport_changed: { height: number; is_expanded: boolean; is_state_stable: boolean };
  safe_area_changed: { top: number; bottom: number; left: number; right: number };
  content_safe_area_changed: { top: number; bottom: number; left: number; right: number };
  back_button_pressed: Record<string, never>;
  write_access_requested: { status: 'allowed' | 'cancelled' };
  fullscreen_changed: { is_fullscreen: boolean };
  visibility_changed: { is_visible: boolean };
  popup_closed: { button_id?: string };
}

/** Events we send. */
export interface OutgoingEvents {
  iframe_ready: { reload_supported: boolean };
  web_app_ready: undefined;
  web_app_expand: undefined;
  web_app_close: undefined;
  web_app_request_theme: undefined;
  web_app_request_viewport: undefined;
  web_app_request_safe_area: undefined;
  web_app_request_content_safe_area: undefined;
  web_app_setup_back_button: { is_visible: boolean };
  web_app_request_write_access: undefined;
  web_app_open_popup: PopupParams;
  web_app_open_tg_link: { path_full: string };
  web_app_trigger_haptic_feedback:
    | { type: 'impact'; impact_style: 'light' | 'medium' | 'heavy' | 'rigid' | 'soft' }
    | { type: 'notification'; notification_type: 'error' | 'success' | 'warning' }
    | { type: 'selection_change' };
}

/** A native Telegram popup: up to three buttons; ok, close and cancel take no text. */
export interface PopupParams {
  title?: string;
  message: string;
  buttons: PopupButton[];
}

type Handler<K extends keyof IncomingEvents> = (data: IncomingEvents[K]) => void;

export interface Bridge {
  readonly transport: 'webview-proxy' | 'external-notify' | 'iframe';
  post<K extends keyof OutgoingEvents>(type: K, ...data: OutgoingEvents[K] extends undefined ? [] : [OutgoingEvents[K]]): void;
  on<K extends keyof IncomingEvents>(type: K, handler: Handler<K>): () => void;
}

interface TelegramWindow {
  parent: unknown;
  TelegramWebviewProxy?: { postEvent(type: string, data?: string): void };
  external?: { notify?(message: string): void };
  Telegram?: { WebView?: { receiveEvent?(type: string, data: unknown): void } };
  TelegramGameProxy?: { receiveEvent(type: string, data: unknown): void };
  TelegramGameProxy_receiveEvent?: (type: string, data: unknown) => void;
  addEventListener(type: 'message', listener: (e: MessageEvent) => void): void;
}

/**
 * Connects to the Telegram client, or returns null when the page isn't
 * running inside Telegram. inTelegram should come from the launch data.
 */
export function connectTelegram(inTelegram: boolean, win: TelegramWindow = window as unknown as TelegramWindow): Bridge | null {
  if (!inTelegram) return null;
  const parent = win.parent;
  const inIframe = parent !== win;

  let transport: Bridge['transport'];
  if (win.TelegramWebviewProxy && typeof win.TelegramWebviewProxy.postEvent === 'function') transport = 'webview-proxy';
  else if (win.external && typeof win.external.notify === 'function') transport = 'external-notify';
  else if (inIframe) transport = 'iframe';
  else return null;

  const handlers = new Map<string, Set<(data: unknown) => void>>();
  const receive = (type: string, data: unknown) => {
    for (const handler of handlers.get(type) ?? []) {
      try {
        handler(data ?? {});
      } catch {
        // A failing handler must not stop the others.
      }
    }
  };

  win.Telegram = win.Telegram ?? {};
  win.Telegram.WebView = { ...win.Telegram.WebView, receiveEvent: receive };
  win.TelegramGameProxy = { receiveEvent: receive };
  win.TelegramGameProxy_receiveEvent = receive;

  if (inIframe) {
    win.addEventListener('message', (e: MessageEvent) => {
      if (e.source !== parent || e.origin !== TELEGRAM_WEB_ORIGIN) return;
      let msg: unknown = e.data;
      if (typeof msg === 'string') {
        try {
          msg = JSON.parse(msg);
        } catch {
          return;
        }
      }
      if (msg && typeof msg === 'object' && typeof (msg as { eventType?: unknown }).eventType === 'string') {
        const { eventType, eventData } = msg as { eventType: string; eventData?: unknown };
        receive(eventType, eventData);
      }
    });
  }

  const bridge: Bridge = {
    transport,
    post(type, ...args) {
      const data = args[0];
      if (transport === 'webview-proxy') {
        win.TelegramWebviewProxy!.postEvent(type, data === undefined ? undefined : JSON.stringify(data));
      } else if (transport === 'external-notify') {
        win.external!.notify!(JSON.stringify({ eventType: type, eventData: data }));
      } else {
        (parent as Window).postMessage(JSON.stringify({ eventType: type, eventData: data }), TELEGRAM_WEB_ORIGIN);
      }
    },
    on(type, handler) {
      let set = handlers.get(type);
      if (!set) handlers.set(type, (set = new Set()));
      set.add(handler as (data: unknown) => void);
      return () => set!.delete(handler as (data: unknown) => void);
    },
  };

  if (inIframe) bridge.post('iframe_ready', { reload_supported: true });
  return bridge;
}

export type PopupButton = { id: string; type: 'default' | 'destructive'; text: string } | { id: string; type: 'ok' | 'close' | 'cancel' };

const popupsOpen = new WeakSet<Bridge>();

/**
 * Shows a native popup and resolves with the pressed button's ID, or null if
 * it was dismissed (or another popup is already open).
 */
export function openPopup(bridge: Bridge, params: { title?: string; message: string; buttons: PopupButton[] }): Promise<string | null> {
  if (popupsOpen.has(bridge)) return Promise.resolve(null);
  popupsOpen.add(bridge);
  return new Promise((resolve) => {
    const off = bridge.on('popup_closed', (data) => {
      off();
      popupsOpen.delete(bridge);
      resolve(data.button_id || null);
    });
    bridge.post('web_app_open_popup', params as PopupParams);
  });
}
