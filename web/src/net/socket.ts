// The push socket (design §12). Only a content-free hint ever comes down it
// ({"t":"sync"}: something arrived), and the app then fetches its inbox over
// HTTPS, so a dropped socket loses nothing. The session token goes as the
// second subprotocol, never in the URL. The socket is kept only while the
// app is visible: a hidden app has none, so the server sends a Telegram
// notice instead.

/** API Gateway closes sockets idle for 10 minutes. */
export const HEARTBEAT_MS = 5 * 60_000;
const FIRST_RETRY_MS = 1_000;
const MAX_RETRY_MS = 60_000;
/** A connection that lasted this long resets the backoff. */
const STABLE_MS = 60_000;

export interface PushSocketDeps {
  url: string;
  /** A current session token. */
  token: () => Promise<string>;
  /** Something arrived (or the socket just opened and may have missed some). */
  onHint: () => void;
  /** For tests. */
  WebSocket?: typeof WebSocket;
}

export interface PushSocket {
  /** Connects now if visible, and keeps a socket while visible. */
  start(): void;
  /** The app was shown or hidden. */
  setVisible(visible: boolean): void;
  /** Whether a socket is open now. */
  readonly connected: boolean;
  stop(): void;
}

export function createPushSocket(deps: PushSocketDeps): PushSocket {
  const WS = deps.WebSocket ?? WebSocket;
  let running = false;
  let visible = true;
  let socket: WebSocket | null = null;
  let connecting = false;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let heartbeat: ReturnType<typeof setInterval> | null = null;
  let backoff = FIRST_RETRY_MS;
  let openedAt = 0;

  const wanted = () => running && visible;

  async function connect(): Promise<void> {
    if (!wanted() || socket || connecting) return;
    connecting = true;
    let token: string;
    try {
      token = await deps.token();
    } catch {
      connecting = false;
      retry();
      return;
    }
    connecting = false;
    if (!wanted() || socket) return;
    const ws = new WS(deps.url, ['v1', token]);
    socket = ws;
    ws.onopen = () => {
      openedAt = Date.now();
      heartbeat = setInterval(() => ws.readyState === WS.OPEN && ws.send('{"action":"ping"}'), HEARTBEAT_MS);
      // Anything that arrived while there was no socket.
      deps.onHint();
    };
    ws.onmessage = (event: MessageEvent) => {
      try {
        if ((JSON.parse(String(event.data)) as { t?: unknown }).t === 'sync') deps.onHint();
      } catch {
        // Not ours: ignore.
      }
    };
    ws.onclose = () => {
      if (socket !== ws) return;
      socket = null;
      stopHeartbeat();
      if (openedAt && Date.now() - openedAt >= STABLE_MS) backoff = FIRST_RETRY_MS;
      openedAt = 0;
      retry();
    };
  }

  /** Tries again later, with growing, jittered delays. */
  function retry(): void {
    if (!wanted() || retryTimer) return;
    const delay = backoff * (0.5 + Math.random());
    backoff = Math.min(backoff * 2, MAX_RETRY_MS);
    retryTimer = setTimeout(() => {
      retryTimer = null;
      void connect();
    }, delay);
  }

  function stopHeartbeat(): void {
    if (heartbeat) clearInterval(heartbeat);
    heartbeat = null;
  }

  function close(): void {
    if (retryTimer) clearTimeout(retryTimer);
    retryTimer = null;
    stopHeartbeat();
    const ws = socket;
    socket = null;
    ws?.close();
  }

  return {
    start() {
      running = true;
      void connect();
    },
    setVisible(v) {
      visible = v;
      if (v) {
        backoff = FIRST_RETRY_MS;
        void connect();
      } else {
        close();
      }
    },
    get connected() {
      return socket !== null && socket.readyState === WS.OPEN;
    },
    stop() {
      running = false;
      close();
    },
  };
}
